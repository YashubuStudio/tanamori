# filetext — AI integration contract

## Purpose

Use `filetext` when an agent has a local file path and needs readable plain text. With no selector, it returns all extractable text. With selectors, it returns only the requested portion. If meaningful text exists as text rather than only pixels or audio, `filetext` is designed to make it easy to retrieve.

Extraction is read-only. Stdout is the text payload; diagnostics go to stderr.

## Preferred interfaces

CLI:

```text
filetext [options] FILE [FILE...]
```

Go:

```go
filetext.Read(path)
filetext.ReadAs(path, filetext.FormatPDF)
filetext.ReadWithOptions(ctx, path, opts)
filetext.Write(ctx, path, writer, opts)
filetext.DetectFormat(path)
```

Use `Write` for large plain-text files or bounded agent context. `Read` materializes the final result in one string.

## Coverage

Native structured extraction includes:

- text/source/config files with automatic character decoding
- HTML/XHTML and XML-family documents
- JSON, JSONL/NDJSON, YAML, TOML, INI, CSV, and TSV
- DOCX-family, XLSX-family, PPTX-family, and OpenDocument text/spreadsheet/presentation files
- PDF text layers, RTF, EPUB, TeX, BibTeX, SRT, and WebVTT
- EML/MHT/MHTML headers, MIME bodies, and readable attachments
- ZIP-family containers, TAR, GZIP, BZIP2, and compressed TAR

Legacy DOC/XLS/PPT/MSG and several legacy ebook/container extensions use best-effort printable ASCII/UTF-16 recovery. Do not claim layout-complete extraction for them.

Content signatures can identify extensionless or mislabeled PDF, OOXML/EPUB/ODF ZIP containers, GZIP, BZIP2, RTF, OLE files, UTF-16 text, and MIME email. Use `--format FORMAT` when authoritative caller knowledge should override detection.

## Selection model

Available dimensions:

- one-based line ranges: `--lines "1-20,40,80-"`
- include regexp: `--match REGEXP`
- exclude regexp: `--exclude REGEXP`
- match context: `--before N --after N`
- literal boundaries: `--from TEXT --to TEXT`
- result window: `--head N` or `--tail N`
- output budget: `--max-chars N`
- case folding: `--ignore-case`

Order: boundaries → line ranges → exclude → match/context → head/tail → maximum Unicode characters.

## Agent defaults

1. For complete content, call `filetext FILE`.
2. If format is uncertain, call `filetext FILE --detect`; normally auto-detection is sufficient.
3. If output may exceed context, start with `--head`, `--lines`, `--match`, or `--max-chars`.
4. For incident evidence, combine `--match`, `--before`, and `--after`.
5. Do not combine `--head` and `--tail`; that is an error.
6. Preserve the original file. Never assume extraction reproduces visual layout.
7. If output is empty for an image-only document, request OCR rather than inventing text.

## Resource and safety behavior

Plain text is decoded and filtered as a stream. Container formats need parser memory. Archives are never expanded to disk and enforce entry-count, per-entry, and total-uncompressed-size limits.

Unsupported built-in compression formats currently include 7z, RAR, and XZ. Password-protected content is not decrypted.

See [USAGE.ai.md](USAGE.ai.md) for copy-ready recipes and exit behavior.