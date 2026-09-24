"""Pinned setup and JSON-lines inference worker for TANAMORI.

This file is embedded into tanamori.exe. Model weights and Python packages are
kept outside the executable so application updates stay small and the official
model artifact can be verified independently.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import sys
import time


def configure_standard_streams() -> None:
    for stream in (sys.stdin, sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is not None:
            reconfigure(encoding="utf-8", errors="strict")

def emit(value: dict) -> None:
    print(json.dumps(value, ensure_ascii=False), flush=True)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(8 * 1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def setup_model(args: argparse.Namespace) -> int:
    from huggingface_hub import snapshot_download

    target = Path(args.model_dir)
    target.mkdir(parents=True, exist_ok=True)
    snapshot_download(
        repo_id=args.model_id,
        revision=args.revision,
        local_dir=str(target),
        allow_patterns=args.file,
        force_download=args.force_download,
    )
    weight = target / args.weight_file
    if not weight.is_file():
        raise RuntimeError(f"model weight was not downloaded: {weight}")
    actual_size = weight.stat().st_size
    if actual_size != args.weight_size:
        raise RuntimeError(
            f"model weight size mismatch: got {actual_size}, want {args.weight_size}"
        )
    actual_hash = sha256_file(weight)
    if actual_hash.lower() != args.weight_sha256.lower():
        raise RuntimeError(
            f"model weight SHA-256 mismatch: got {actual_hash}, "
            f"want {args.weight_sha256}"
        )
    emit(
        {
            "status": "verified",
            "model_id": args.model_id,
            "revision": args.revision,
            "weight_size": actual_size,
            "weight_sha256": actual_hash,
        }
    )
    return 0


def select_device(torch):
    requested = os.environ.get("TANAMORI_DEVICE", "auto").strip().lower()
    if requested not in {"", "auto"}:
        return torch.device(requested)
    if torch.cuda.is_available():
        return torch.device("cuda")
    if hasattr(torch.backends, "mps") and torch.backends.mps.is_available():
        return torch.device("mps")
    return torch.device("cpu")


def window_starts(token_count: int, window_size: int, limit: int) -> list[int]:
    if token_count <= window_size or limit <= 1:
        return [0]
    last = token_count - window_size
    starts = {round(index * last / (limit - 1)) for index in range(limit)}
    return sorted(starts)


def make_pair_features(tokenizer, query: str, passage: str, max_chunks: int) -> list[dict]:
    max_length = 512
    max_passage_tokens = 160
    passage_ids = tokenizer.encode(passage, add_special_tokens=False)[:max_passage_tokens]
    special_tokens = tokenizer.num_special_tokens_to_add(pair=True)
    query_window = max(max_length - special_tokens - len(passage_ids), 64)
    query_ids = tokenizer.encode(query, add_special_tokens=False)
    starts = window_starts(len(query_ids), query_window, max_chunks)
    if tokenizer.bos_token_id is None or tokenizer.eos_token_id is None:
        raise RuntimeError("the XLM-R tokenizer does not define BOS/EOS token IDs")
    features = []
    for start in starts:
        query_chunk = query_ids[start : start + query_window]
        input_ids = (
            [tokenizer.bos_token_id]
            + query_chunk
            + [tokenizer.eos_token_id, tokenizer.eos_token_id]
            + passage_ids
            + [tokenizer.eos_token_id]
        )
        features.append({"input_ids": input_ids, "attention_mask": [1] * len(input_ids)})
    return features

def serve(args: argparse.Namespace) -> int:
    os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")
    import torch
    from transformers import AutoModelForSequenceClassification, AutoTokenizer

    thread_count = int(
        os.environ.get("TANAMORI_MODEL_THREADS", min(max((os.cpu_count() or 2) // 2, 1), 8))
    )
    max_chunks = min(max(int(os.environ.get("TANAMORI_MODEL_MAX_CHUNKS", "3")), 1), 8)
    torch.set_num_threads(max(thread_count, 1))
    device = select_device(torch)
    model_dir = str(Path(args.model_dir))
    tokenizer = AutoTokenizer.from_pretrained(model_dir, local_files_only=True)
    model = AutoModelForSequenceClassification.from_pretrained(
        model_dir, local_files_only=True
    )
    model.eval()
    model.to(device)
    emit(
        {
            "type": "ready",
            "model_id": args.model_id,
            "revision": args.revision,
            "device": str(device),
            "threads": thread_count,
        }
    )

    for line in sys.stdin:
        started = time.perf_counter()
        request_id = ""
        query = ""
        passage = ""
        try:
            request = json.loads(line)
            request_id = str(request.get("id", ""))
            if request.get("command") == "shutdown":
                emit({"id": request_id, "type": "stopped"})
                return 0
            query = str(request.get("query", ""))
            passage = str(request.get("passage", ""))
            features = make_pair_features(tokenizer, query, passage, max_chunks)
            logits = []
            with torch.inference_mode():
                for feature in features:
                    inputs = tokenizer.pad([feature], padding=True, return_tensors="pt")
                    inputs = {name: value.to(device) for name, value in inputs.items()}
                    logits.append(float(model(**inputs, return_dict=True).logits.view(-1)[0].float().cpu()))
            logit = max(logits)
            score = 1.0 / (1.0 + math.exp(-max(min(logit, 80.0), -80.0)))
            emit(
                {
                    "id": request_id,
                    "score": score,
                    "logit": logit,
                    "chunks": len(features),
                    "duration_ms": (time.perf_counter() - started) * 1000.0,
                }
            )
        except Exception as error:  # keep the worker alive after a bad request
            diagnostics = (
                f"query={type(query).__name__}[{len(query)}], "
                f"passage={type(passage).__name__}[{len(passage)}]"
            )
            if os.environ.get("TANAMORI_DEBUG_MODEL") == "1":
                diagnostics += f", query_preview={query[:120]!r}, passage_preview={passage[:120]!r}"
            emit(
                {
                    "id": request_id,
                    "error": f"{type(error).__name__}: {error} ({diagnostics})",
                    "duration_ms": (time.perf_counter() - started) * 1000.0,
                }
            )
    return 0


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser()
    subcommands = result.add_subparsers(dest="command", required=True)

    setup = subcommands.add_parser("setup")
    setup.add_argument("--model-id", required=True)
    setup.add_argument("--revision", required=True)
    setup.add_argument("--model-dir", required=True)
    setup.add_argument("--weight-file", required=True)
    setup.add_argument("--weight-size", required=True, type=int)
    setup.add_argument("--weight-sha256", required=True)
    setup.add_argument("--file", action="append", required=True)
    setup.add_argument("--force-download", action="store_true")

    subcommands.add_parser("protocol-check")

    worker = subcommands.add_parser("serve")
    worker.add_argument("--model-id", required=True)
    worker.add_argument("--revision", required=True)
    worker.add_argument("--model-dir", required=True)
    return result


def main() -> int:
    configure_standard_streams()
    args = parser().parse_args()
    if args.command == "setup":
        return setup_model(args)
    if args.command == "serve":
        return serve(args)
    if args.command == "protocol-check":
        request = json.loads(sys.stdin.readline())
        value = str(request.get("text", ""))
        emit({"text": value, "length": len(value)})
        return 0
    raise RuntimeError(f"unknown command: {args.command}")


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        raise SystemExit(130)
    except Exception as error:
        print(f"tanamori model runtime: {type(error).__name__}: {error}", file=sys.stderr)
        raise SystemExit(1)
