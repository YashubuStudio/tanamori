# TANAMORI

Windowsでは `dist\tanamori.exe` をダブルクリックするとローカルWeb UIが起動し、既定ブラウザーで `http://127.0.0.1:8765` が開きます。画面操作と分類テストの手順は [USAGE.md](USAGE.md) を参照してください。

TANAMORI は、ローカルのファイルを読み取り、検索可能な索引を作り、登録済みフォルダーへの分類候補と安全な移動提案を作る Go 製アプリケーションです。ファイル本文の抽出には独立プロジェクト `filetext` の固定スナップショットを `third_party/filetext` から利用します。TANAMORIだけをクローンしてもビルドできます。

現在の基礎実装には、SQLite/FTS5 による永続化・全文検索、フォルダー登録、ファイル取り込み、分類、移動提案、承認後の安全な移動と監査記録が含まれます。自動移動は既定で無効です。

## Web UI

```powershell
# ブラウザーを自動で開く
.\dist\tanamori.exe web

# 引数なしでも web と同じ（ダブルクリック用）
.\dist\tanamori.exe
```

Web UIでは、キュー状況、分類先の登録、ドラッグ＆ドロップでのファイル・フォルダー投入、最近のジョブと永続作業ログ、分類結果と関連度スコア、全文検索、移動提案の承認を操作できます。関連タグの優先度と同名ファイルの命名ルール、削除前バックアップ付きの一括DB削除も設定できます。親フォルダー直下のサブフォルダーを分類先として一括登録し、その内容を分類テストへ投入する機能もあります。UI資産は実行ファイルへ埋め込まれているため、Node.jsや別のWebサーバーは不要です。

UIとAPIはループバック接続だけを受け付けます。外部公開用の認証機能は提供していないため、リバースプロキシ等で公開しないでください。

## メインモデル

分類のメインモデルは `BAAI/bge-reranker-v2-m3` です。

- モデルID: `BAAI/bge-reranker-v2-m3`
- 固定リビジョン: `953dc6f6f85a1b2dbfca4c34a2796e7dde08d41e`
- ライセンス: Apache-2.0
- 公式重み: `model.safetensors`、2,271,071,852 bytes
- SHA-256: `d9e3e081faff1eefb84019509b2f5558fd74c1a05a2c7db22f74174fcedb5286`

重みだけで約 2.27 GB あるため、`tanamori.exe` には重みを直接埋め込みません。実行ファイルには、固定済みマニフェスト、初回セットアップ、SHA-256検証、推論ランナーを内包します。重みと環境別Pythonパッケージは、初回セットアップでユーザー領域へ導入されます。これによりTANAMORI本体を小さく保ち、アプリ更新時にモデルを再配布せずに済みます。

## 初回セットアップ

必要なもの:

- 64-bit Python 3.10 以上
- インターネット接続
- モデルと隔離Python環境を置く空き容量（5 GB以上を推奨）

```powershell
.\tanamori.exe setup
```

Goから実行する場合:

```powershell
go run ./cmd/tanamori setup
```

`setup` はデータベースを初期化し、専用venvを作成して、PyTorch・Transformers等の環境対応版と公式モデルを取得します。最後に重みのサイズとSHA-256を検証し、成功した場合だけ導入完了マーカーを書きます。中断・失敗した導入は ready 扱いになりません。

Pythonを明示する場合:

```powershell
.\tanamori.exe setup --python "D:\Program Files\Python\python.exe"
```

既定の配置先は `%LOCALAPPDATA%\YashubuStudio\TANAMORI\ai` です。変更する場合は全コマンドで同じ `--model-root` を指定してください。

```powershell
.\tanamori.exe setup --model-root "D:\TANAMORI-AI"
```

## モデル管理

```powershell
# ネットワークを使わず、導入状態を即座に確認
.\tanamori.exe model-status

# モデルだけを導入
.\tanamori.exe model-install

# 2.27 GBの重み全体を読み、SHA-256を再検証
.\tanamori.exe model-verify

# Python環境と公式スナップショットを強制更新
.\tanamori.exe setup --force
```

`--force` は Hugging Face 側から再取得するため、通常の起動では使用しません。

導入済みなら `ingest` はBGEを主スコアラーとして使用します。抽出済みファイル本文をquery、フォルダー名・用途・説明を短いpassageとして渡し、公式手順どおり最大512 tokenで推論し、logitをsigmoidで0〜1へ正規化します。モデルはコマンド中に一度だけ遅延ロードされ、候補ごとに再ロードしません。

関連タグには固定の関連語句・同義語リストを使いません。BGEが既に計算した分類先ごとの関連度から、近接する候補のタグを付けます。関連度が高い場合はモデルの第1候補を基本とし、ファイル名に登録済み分類先の名前が明示され、スコアも近い場合だけ優先度を適用します。BGEは語句を生成するモデルではないため、キーワード辞書の自動生成は行いません。別のLLMや人手によるキーワード登録も不要です。

未導入環境でも索引作成を止めないため、BGEが未セットアップの場合だけ決定的な軽量キーワードスコアラーへフォールバックし、標準エラーへ明示します。

キーワード方式は登録済み分類先の名前・用途・説明とファイルをその場で比較するだけで、関連タグ用の事前語句表は持ちません。この方式では関連タグを自動付与せず、タグ優先度も適用しません。

任意の実行設定:

- `TANAMORI_PYTHON`: セットアップに使うPython
- `TANAMORI_DEVICE`: `cpu`、`cuda`、`mps` 等。未指定は利用可能なアクセラレーターを自動選択
- `TANAMORI_MODEL_THREADS`: CPU推論のスレッド数（既定はCPU数の半分、最大8）
- TANAMORI_MODEL_MAX_CHUNKS: 長文から評価するtoken区間数（既定3、最大8）

## 基本操作

```powershell
.\tanamori.exe folder-add `
  --path "D:\Library\Contracts" `
  --name "契約資料" `
  --purpose "契約書、見積条件、取引先との合意文書"

.\tanamori.exe ingest --path "D:\Inbox\新規契約.txt"
.\tanamori.exe search --query "更新条件の契約書"
.\tanamori.exe proposals
.\tanamori.exe apply --proposal "prp_..." --reason "候補を確認済み"
```

CLIはJSONを標準出力へ返します。セットアップの進捗と警告は標準エラーへ出すため、JSONをそのまま他のUIやプログラムから扱えます。既定DBは `%LOCALAPPDATA%\YashubuStudio\TANAMORI\library.db` です。

## 大量ファイルのキュー処理とローカルサーバー

`ingest` は従来どおり単一ファイルの同期処理です。大量のファイルには、先に `setup` とフォルダー登録を済ませ、同じDBを指定して `serve` と `enqueue` を使います。

```powershell
.\dist\tanamori.exe serve --db "D:\TANAMORI\library.db" --model-root "D:\TANAMORI\ai"
```

別のターミナルから、ファイルまたはディレクトリを投入します。ディレクトリの全ファイル名を受付時に読み込まず、走査ジョブをSQLiteへ記録してすぐ返します。

```powershell
$job = .\dist\tanamori.exe enqueue --db "D:\TANAMORI\library.db" --path "D:\Inbox" | ConvertFrom-Json
.\dist\tanamori.exe job-status --db "D:\TANAMORI\library.db" --job $job.job_id
.\dist\tanamori.exe queue-status --db "D:\TANAMORI\library.db"
```

サーバーは既定で `127.0.0.1:8765` のみ待ち受けます。無認証APIを外部へ公開しないため、`--listen` でもループバック以外は拒否します。別PCから使う場合は認証・TLS付きリバースプロキシを別途用意してください。

```powershell
# Windows PowerShell 5.1では日本語パスをUTF-8バイト列として送る
$json = @{ path = "D:\Inbox" } | ConvertTo-Json -Compress
$body = [Text.Encoding]::UTF8.GetBytes($json)
$job = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:8765/v1/jobs" -ContentType "application/json; charset=utf-8" -Body $body
Invoke-RestMethod "http://127.0.0.1:8765/v1/jobs/$($job.job_id)"
Invoke-RestMethod "http://127.0.0.1:8765/v1/queue"
$q = [Uri]::EscapeDataString("契約")
Invoke-RestMethod "http://127.0.0.1:8765/v1/search?q=$q&limit=10"
```

`GET /healthz` も利用できます。HTTP受付はファイル内容を読みません。走査は1ディレクトリ64件ずつ列挙し、ファイルジョブが `--queue-limit`（既定256件）に達すると待機します。`queue-status` の件数には走査ジョブも含まれます。直接ファイルを投入して満杯の場合はHTTP 429を返します。ジョブはSQLiteへ永続化され、失敗時は最大3回試行し、通常停止後も残ります。強制終了時のワーカーリースは最大約15秒で失効します。

Web UI右上の「正規終了」は確認後に新規受付を止め、処理中ジョブを最大2分待ってからHTTPサーバー、キューワーカー、モデルプロセスを閉じます。待機中・未完了のジョブは次回起動時に回復します。ブラウザーのタブを閉じるだけではサーバーは終了しません。

ファイル処理ワーカーはCPUと起動時の空き物理メモリから1～2件に制限され、`--workers 1` でさらに減らせます。空きメモリが1 GiB未満になると新しいファイル処理を一時停止します。サーバー起動時に空きメモリが5 GiB未満ならBGEを起動せずキーワード方式を使います。BGEモデル本体は1プロセスで共有し、推論要求は直列です。

常駐サーバーでは入力を1ファイル512 MiBまでに制限します。非ストリーム形式は抽出前64 MiB、ZIP系は展開後合計64 MiBまでです。超過やコピー中の変更はジョブの失敗として表示し、サーバー全体は停止しません。これらは安全上限であり、全ファイル形式のピークメモリを厳密に保証するものではありません。

## 構成

```text
cmd/tanamori/                     CLIと初回セットアップ入口
internal/app/                     アプリケーションサービスとPort
internal/domain/                  エンティティ、状態、分類ポリシー
internal/extract/                 filetext Adapterと本文チャンク化
internal/filesystem/              検査、ハッシュ、安全な移動
internal/modelsetup/              venv・モデル導入・整合性検証
internal/scoring/                 BGE常駐ワーカーAdapterと軽量fallback
internal/storage/                 SQLite、FTS5、Repository
internal/jobqueue/                永続キュー、64件単位走査、資源上限
internal/httpapi/                 ローカルHTTP API
models/bge-reranker-v2-m3/        埋め込み用マニフェストとPythonランナー
third_party/filetext/             独立filetextプロジェクトのビルド用スナップショット
```

モデルの巨大ファイルやvenvはリポジトリへ保存しません。`models/bge-reranker-v2-m3` に置くのは、`.exe`へ埋め込める小さな配布資産だけです。

## Windows向けビルド

```powershell
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o .\dist\tanamori.exe ./cmd/tanamori
```

生成した `.exe` は単体でセットアップ処理を開始できます。ただし実際の推論には、初回セットアップで作成される64-bit Python環境とモデルが必要です。

## 開発時の確認

```powershell
go test -count=1 ./...
go vet ./...
go build ./cmd/tanamori
Push-Location .\third_party\filetext
try {
    $env:GOWORK = off
    go test -count=1 ./...
} finally {
    Remove-Item Env:GOWORK -ErrorAction SilentlyContinue
    Pop-Location
}
```

SQLiteはCGO不要の `modernc.org/sqlite` を使用しています。

改善の進捗と未解決事項は [IMPROVEMENTS.md](IMPROVEMENTS.md)、Web UIの実操作チェックリストは [UI_TEST.md](UI_TEST.md) を参照してください。

## 公式資料

- [BAAI/bge-reranker-v2-m3](https://huggingface.co/BAAI/bge-reranker-v2-m3)
- [FlagEmbedding reranker usage](https://github.com/FlagOpen/FlagEmbedding/tree/master/examples/inference/reranker)
- [TANAMORI 企画書](TANAMORI_企画書.docx)
- [TANAMORI システム設計書](TANAMORI_システム設計書.docx)
