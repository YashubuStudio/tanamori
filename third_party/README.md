# 同梱する依存ソース

`filetext/` は独立プロジェクト `filetext` の固定スナップショットです。現時点では `github.com/YashubuStudio/filetext` の公開リポジトリを取得できないため、TANAMORIの単独クローンでビルドできるよう同梱しています。

更新時は独立プロジェクトの変更を確認してからスナップショットを同期し、TANAMORIと `third_party/filetext` のテストを両方実行してください。公開版が利用可能になったら、版を固定した通常のGoモジュール依存へ置き換える予定です。
