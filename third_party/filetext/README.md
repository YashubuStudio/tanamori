# filetext

ファイルを渡すと、中にある読み取り可能なテキストをUTF-8のプレーンテキストとして返す、軽量なGoライブラリ兼CLIです。

**このプロジェクトを使えば、中身のテキストがテキストとして入っている限り、簡単に読み取れます。** 指定なしなら全文を返し、行範囲・正規表現・前後行・開始／終了位置・先頭／末尾・最大文字数を自由に組み合わせて必要な部分だけを取り出せます。

## 特長

- `filetext FILE` だけで形式と文字コードを自動判定
- 拡張子がないファイルでも、PDF、ZIP系Office、GZIP、BZIP2、RTF、旧Office、UTF-16、メールなどを内容から判定
- UTF-8、BOM付きUTF-8、UTF-16、日本語を含む一般的な文字コードをUTF-8へ変換
- Office、PDF、Web、電子書籍、メール、構造化データ、字幕、圧縮ファイル、ソースコードを同じAPIで処理
- プレーンテキストはデコードと選択をストリーミング処理
- 入力ファイルを書き換えず、外部Officeアプリや外部コマンドも不要

## 対応形式

| 分類 | 主な形式 | 読み取り方法 |
|---|---|---|
| テキスト／コード | TXT、Markdown、ログ、Go、Python、JavaScript、TypeScript、C/C++、Java、Rust、Shell、PowerShell、SQLなど | 文字コードを自動判定して本文を返す |
| Web／構造化 | HTML、XHTML、XML、SVG、RSS、Atom、JSON、JSONL/NDJSON、YAML、TOML、INI、CSV、TSV | タグ除去または値を読みやすいテキストへ変換 |
| Microsoft Office | DOCX/DOCM/DOTX、XLSX/XLSM/XLTX、PPTX/PPTM/PPSX/POTX | 文書本文、ヘッダー等、表計算セル、スライド／ノートを抽出 |
| OpenDocument | ODT/OTT、ODS/OTS、ODP/OTP | `content.xml` から本文・セル・スライドを抽出 |
| 文書／書籍 | PDF、RTF、EPUB、TeX、BibTeX | テキスト層、本文、メタデータを抽出 |
| メール | EML、MHT、MHTML | ヘッダー、MIME本文、テキスト添付を抽出 |
| 字幕 | SRT、WebVTT | 時刻情報を除き本文を抽出 |
| 圧縮／コンテナ | ZIP、JAR、WAR、EAR、WHL、APK、AAB、IPA、NuGet、VSIX、CBZ、KMZ、TAR、GZIP、BZIP2、TGZ、TBZ2 | 展開せず、内部の可読ファイルを順番に抽出 |
| 旧式／バイナリ | DOC、XLS、PPT、MSG、MOBI、AZW/AZW3、CHM | ASCII／UTF-16の可読文字列をベストエフォートで救出 |

対応拡張子の完全な一覧は `filetext --list-formats` で確認できます。未知の拡張子も内容がテキストなら読み取りを試みます。形式が誤判定される場合は `--format` またはGoの `ReadAs` で明示できます。

## クイックスタート

```powershell
go install github.com/YashubuStudio/filetext/cmd/filetext@latest
filetext document.docx
filetext report.pdf --lines "1-40"
filetext server.log --match "ERROR|WARN" --before 2 --after 3
filetext unknown.data --detect
filetext no-extension --format email
```

ライブラリとして使う場合:

```go
package main

import (
    "fmt"
    "log"

    "github.com/YashubuStudio/filetext"
)

func main() {
    text, err := filetext.Read("document.docx")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print(text)
}
```

大きなテキストや出力上限が必要な処理では、全文文字列を作る `Read` より `Write` を推奨します。

```go
err := filetext.Write(ctx, "large.log", os.Stdout, filetext.Options{
    Selection: filetext.Selection{
        Match:    `ERROR|WARN`,
        Before:   2,
        After:    3,
        MaxChars: 12000,
    },
})
```

詳しい操作は [USAGE.md](docs/USAGE.md)、AIエージェント向けの短い仕様は [README.ai.md](docs/README.ai.md) と [USAGE.ai.md](docs/USAGE.ai.md) を参照してください。

## プロジェクト構成

```text
filetext.go          公開API: Read / ReadAs / ReadWithOptions / Write
format.go            形式・拡張子・内容シグネチャの判定
selection.go         行範囲、検索、前後行、出力上限
encoding.go          文字コード判定とUTF-8変換
extract.go           形式ごとの抽出器への振り分け
extract_*.go         各形式の抽出実装
cmd/filetext/        CLI
/docs/               詳細UsageとAI向け文書
```

抽出器を別パッケージへ細分化せず同じ `filetext` パッケージに置くことで、利用側は形式を問わず `filetext.Read(path)` だけを呼び出せます。

## メモリと安全性

プレーンテキストは行単位で処理されるため、`Write` とCLIではファイル全体をメモリに保持しません。PDF、Office、EPUBなどは各形式の解析に必要な範囲でメモリを使います。XLSXは行イテレーターを利用します。

圧縮ファイルはディスクへ展開せず、1項目128 MiB、合計512 MiB、4096項目の上限を設け、過剰な展開を拒否します。

## 制限

- 画像だけのPDFや画像内文字にはテキスト層がないため、別途OCRが必要です。
- パスワード付き／暗号化済みファイルは復号しません。
- 旧DOC/XLS/PPT、MOBI等は専用レイアウト解析ではなく、可読文字列のベストエフォート抽出です。
- 7z、RAR、XZは現在の内蔵デコーダー対象外です。
- 出力は検索・要約・解析向けのプレーンテキストであり、元の視覚レイアウトを再現しません。

## 開発

```powershell
go test ./...
go vet ./...
go build ./cmd/filetext
```

Go 1.27.1以上を使用します。