# filetext — AI usage recipes

## Minimal calls

```powershell
filetext "C:\data\document.docx"
filetext "C:\data\report.pdf" --max-chars 12000
filetext "C:\logs\app.log" --match "error|panic" --before 3 --after 8 --ignore-case
```

## Inspect before extracting

Use detection when routing, auditing, or debugging an ambiguous file. Normal reads already auto-detect.

```powershell
filetext "C:\data\unknown.bin" --detect
filetext first.bin second.bin --detect
filetext payload.data --format json
```

Valid parser names are `auto`, `text`, `html`, `xml`, `json`, `jsonl`, `yaml`, `toml`, `ini`, `csv`, `tsv`, `docx`, `pptx`, `xlsx`, `odf`, `pdf`, `rtf`, `epub`, `srt`, `vtt`, `tex`, `bibtex`, `email`, `zip`, `tar`, `gzip`, `bzip2`, and `binary-strings`.

## Intent mapping

| User intent | Arguments |
|---|---|
| entire file | `FILE` |
| lines 30 through 80 | `FILE --lines "30-80"` |
| beginning or quick preview | `FILE --head 100` |
| latest log lines | `FILE --tail 200` |
| section between markers | `FILE --from "START" --to "END"` |
| matching evidence with context | `FILE --match "REGEXP" --before 2 --after 4` |
| bounded model input | add `--max-chars N` |
| suppress noise | add `--exclude "REGEXP"` |
| force known content type | add `--format FORMAT` |
| only identify content type | add `--detect` |

## Composable example

```powershell
filetext incident.log --lines "500-" --match "error|timeout" --exclude "healthcheck" --before 2 --after 5 --ignore-case --max-chars 16000
```

Semantics:

1. Consider lines 500 onward.
2. Remove health-check lines.
3. Select error or timeout lines case-insensitively.
4. Include two preceding and five following eligible lines.
5. Stop at 16,000 Unicode characters without splitting UTF-8.

## Format-specific recipes

```powershell
# Extract speaker notes together with slide text
filetext briefing.pptx

# Read JSON objects stored one per line
filetext events.ndjson --match "failed|timeout" --ignore-case

# Extract MIME headers, body alternatives, and readable attachments
filetext message.eml

# Read text members without expanding an archive to disk
filetext evidence.tar.gz --match "token|error" --ignore-case

# Best-effort printable text recovery from a legacy Office file
filetext old-report.doc --format binary-strings
```

Archive output contains `===== member/path =====` section headers. Preserve them when member provenance matters; remove or split them only if the calling task requests it.

## Go integration

```go
var output bytes.Buffer
err := filetext.Write(ctx, path, &output, filetext.Options{
    Format: filetext.FormatAuto,
    Selection: filetext.Selection{
        Lines:      "1-500",
        Match:      `decision|result|error`,
        Before:     2,
        After:      4,
        IgnoreCase: true,
        MaxChars:   12000,
    },
})
```

For an authoritative content type:

```go
text, err := filetext.ReadAs(path, filetext.FormatEmail)
```

`Selection.IgnoreCase` applies to `Match`, `Exclude`, `From`, and `To`.

## Output handling

- Exit `0`: extraction succeeded.
- Exit `1`: file, decoding, parsing, selection, or output error.
- Exit `2`: invalid invocation or format name.
- Extracted text goes to stdout or `--output`.
- Diagnostics go to stderr.
- Multiple inputs receive file headings unless `--no-header` is set.

An empty result is not permission to infer missing content. It may mean that the document contains only images, audio, encrypted material, unsupported compression, or no matching selection.

## Safety and limitations

- Read-only: inputs are never modified.
- No OCR or speech recognition.
- No password decryption.
- No visual-layout guarantee.
- Plain text streams; parsed document formats may require memory proportional to format structures.
- Archive expansion stays in memory/streams, never writes members to disk, and enforces 4096 entries, 128 MiB per entry, and 512 MiB total.
- 7z, RAR, and XZ need another decoder before passing content to `filetext`.