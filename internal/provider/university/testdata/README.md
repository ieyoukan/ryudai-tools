# 合成テストデータ

架空の学年暦、時間割、必修科目分類表です。大学が公開している資料のコピーは同梱していません。PDFは座標付きの行・列、複数行の備考、複数ページ、必修と選択必修を検証するためのものです。

PDFを作り直す場合は、ReportLabとM PLUS Rounded 1c Regular（SIL Open Font License）を用意して、このディレクトリで `python3 generate.py /path/to/MPLUSRounded1c-Regular.ttf` を実行します。フォントは生成PDFの表示に必要な文字だけを埋め込んでいます。通常のGoテストにはPythonは不要です。
