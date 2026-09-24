# BAAI/bge-reranker-v2-m3 bundle

このディレクトリは、TANAMORIのメイン分類モデルを再現可能に導入するための小さな配布資産です。

- `manifest.json`: モデルID、公式revision、対象ファイル、重みサイズ、SHA-256、実行依存条件
- `runtime.py`: 初回ダウンロード、検証、JSON Lines推論ワーカー
- `bundle.go`: 上記2ファイルを `tanamori.exe` へ埋め込むGoパッケージ

`model.safetensors` は2,271,071,852 bytesあるため、ここには保存しません。`tanamori setup` が固定revisionをユーザー領域へ取得し、SHA-256を照合します。

モデルのライセンスは Apache-2.0 です。配布元とモデルカードは <https://huggingface.co/BAAI/bge-reranker-v2-m3> を参照してください。