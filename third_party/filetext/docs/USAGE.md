# filetext 使用方法

## インストール

```powershell
go install github.com/YashubuStudio/filetext/cmd/filetext@latest
```

リポジトリ内で直接使う場合:

```powershell
go run ./cmd/filetext -- document.docx
```

## 基本

```powershell
filetext FILE
```

指定がなければ抽出できる全文を標準出力へ返します。オプションはファイル名の前後どちらにも置けます。複数ファイルも指定できます。

```powershell
filetext first.docx second.pdf
filetext first.docx second.pdf --no-header
filetext report.pdf --output report.txt
```

ファイル名が `-` で始まる場合は、オプション終端の `--` を使います。

```powershell
filetext -- -notes.txt
```

## 形式の判定と指定

通常は内容、ZIP内部構造、ファイル名、拡張子、テキスト判定を組み合わせて自動判定します。

```powershell
filetext unknown.bin --detect
filetext first.bin second.bin --detect
```

誤った拡張子や独自コンテナなど、形式が既知なら明示できます。

```powershell
filetext payload.data --format json
filetext mail.dump --format email
filetext legacy.bin --format binary-strings
```

`--format` に指定できる名前:

```text
auto, text, html, xml, json, jsonl, yaml, toml, ini, csv, tsv,
docx, pptx, xlsx, odf, pdf, rtf, epub, srt, vtt, tex, bibtex,
email, zip, tar, gzip, bzip2, binary-strings
```

拡張子名も指定できます。例: `--format .docx`、`--format ndjson`、`--format bz2`。

## 部分抽出

選択処理は、開始／終了位置 → 行範囲 → 除外正規表現 → 一致正規表現と前後行 → 先頭／末尾 → 最大文字数、の順で適用されます。行番号は1始まりです。

### 行範囲

```powershell
filetext notes.txt --lines 10
filetext notes.txt --lines "1-20,40,80-"
filetext notes.txt --lines "-30"
```

- `10`: 10行目
- `1-20`: 1〜20行目
- `80-`: 80行目以降
- `-30`: 先頭〜30行目
- 複数範囲はカンマで結合

### 正規表現と前後行

```powershell
filetext app.log --match "ERROR|WARN"
filetext app.log --match "timeout" --ignore-case
filetext app.log --match "panic" --before 5 --after 10
filetext app.log --exclude "healthcheck|heartbeat"
```

`--before` と `--after` は `--match` に一致した行の前後を追加します。重複する範囲は一度だけ出力されます。

### 開始／終了文字列

```powershell
filetext book.txt --from "第3章" --to "第4章"
filetext book.txt --from "BEGIN" --to "END" --ignore-case
```

開始文字列と終了文字列を含む行も出力対象です。

### 先頭・末尾・文字数上限

```powershell
filetext large.txt --head 100
filetext large.txt --tail 100
filetext large.txt --max-chars 4000
```

`--head` と `--tail` は同時指定できません。`--max-chars` はUnicode文字数で制限し、UTF-8文字の途中では切りません。

### 組み合わせ

```powershell
filetext server.log --lines "1000-" --match "request_id=.*(error|timeout)" --exclude "retry succeeded" --before 2 --after 5 --max-chars 12000 --ignore-case
```

## 全CLIオプション

| オプション | 内容 |
|---|---|
| `--format FORMAT` | パーサーを明示。既定は `auto` |
| `--detect` | 抽出せず、検出した形式名を表示 |
| `--lines SPEC` | `1-20,40,80-` 形式の行範囲 |
| `--match REGEXP` | 一致する行を選択 |
| `--exclude REGEXP` | 一致する行を除外 |
| `--before N` | 一致行の前をN行追加 |
| `--after N` | 一致行の後をN行追加 |
| `--from TEXT` | TEXTを含む最初の行から開始 |
| `--to TEXT` | TEXTを含む最初の行までで終了 |
| `--head N` | 選択結果の先頭N行 |
| `--tail N` | 選択結果の末尾N行 |
| `--max-chars N` | 出力する最大Unicode文字数 |
| `--ignore-case` | 正規表現と境界文字列で大文字小文字を無視 |
| `--output PATH` | 標準出力ではなくファイルへ保存 |
| `--no-header` | 複数ファイル時のファイル見出しを省略 |
| `--list-formats` | 認識する全拡張子を表示 |
| `--version` | バージョンを表示 |

## Go API

### 全文を文字列で受け取る

```go
text, err := filetext.Read("manual.pdf")
```

### パーサーを明示する

```go
text, err := filetext.ReadAs("payload.data", filetext.FormatJSON)
```

### 指定した部分を文字列で受け取る

```go
text, err := filetext.ReadWithOptions(ctx, "app.log", filetext.Options{
    Format: filetext.FormatAuto,
    Selection: filetext.Selection{
        Match:      `ERROR|WARN`,
        Before:     2,
        After:      3,
        IgnoreCase: true,
        MaxChars:   8000,
    },
})
```

### `io.Writer` へ逐次出力する

```go
err := filetext.Write(ctx, "large.txt", os.Stdout, filetext.Options{
    Selection: filetext.Selection{Lines: "1000-2000"},
})
```

大きなプレーンテキストでは `Read` より `Write` を優先してください。`context.Context` をキャンセルすると行処理を中断できます。

### 形式を調べる

```go
format, err := filetext.DetectFormat("unknown.data")
extensions := filetext.SupportedExtensions()
```

## 圧縮ファイル

ZIP系、TAR、GZIP、BZIP2、およびGZIP/BZIP2圧縮TARを読み取れます。内部ファイルごとに次の見出しを付けます。

```text
===== docs/readme.txt =====
本文
```

圧縮内容をディスクへ書き出すことはありません。1項目128 MiB、全項目合計512 MiB、4096項目を超える入力はエラーにします。入れ子の圧縮ファイルを再帰展開しないため、予期しない展開量も抑えます。

## 終了コード

- `0`: 成功
- `1`: ファイル読込、デコード、解析、選択、出力のエラー
- `2`: 引数不足、未知の形式名などの呼び出しエラー

抽出結果は標準出力、診断は標準エラーへ送られます。

## 制限

- スキャン画像だけのPDFや画像内の文字にはOCRが必要です。
- 暗号化文書やパスワード付き文書は復号しません。
- 旧DOC/XLS/PPT等の `binary-strings` は可読文字列の救出で、文書構造や順序を完全には再現しません。
- 7z、RAR、XZは内蔵対応していません。
- Office内の画像文字、埋め込み音声、動画は抽出しません。
- 出力はプレーンテキストで、書式やページレイアウトは保持しません。