# 分類評価用の保留データ

元の18件とは別に作成した架空文書12件です。ファイル名には分類ラベルを入れていません。一部は会議と会計、顧客と修正依頼などの話題を重ねています。実運用文書の精度を保証するものではありません。

`expected_labels.csv` の `expected_tags` は `|` 区切りで複数指定できます。評価時、各サブフォルダーを分類先として一時DBに登録し、BGEモデルの分類先・関連タグを別々に採点します。元のTANAMORI利用者DBには書き込みません。

```powershell
.\dist\tanamori.exe evaluate --expected .\testdata\evaluation_holdout\expected_labels.csv
```

結果の `accuracy` は全件に対する正解率、`coverage` は保留されなかった割合です。`tag_precision` と `tag_recall` はタグ単位のマイクロ平均です。BGEの `score` は関連度であり、正解確率ではありません。

初回の実測結果と注意点は [RESULT_2026-09-29.md](RESULT_2026-09-29.md) を参照してください。
