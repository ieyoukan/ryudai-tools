# github.com/ledongthuc/pdf の修正版

元: github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728（BSD-3-Clause、LICENSE 参照）。
go.mod の replace でこのディレクトリを使う。テスト・examples は含めていない。

## 修正

- `lex.go` の `readArray`: 閉じていない配列のまま EOF に達すると、`readToken` が返す `io.EOF` を
  配列に追加し続けて無限ループになり、メモリを使い切っていた（`readDict` には EOF の判定がある）。
  `io.EOF` でも配列の読み取りを終えるようにした。
  琉球大学 R6 便覧の教育学部の資料（kyoiku_1_1.pdf、seigohyo_kyoiku.pdf）で、読むだけでプロセスが落ちていた。
