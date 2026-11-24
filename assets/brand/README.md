# ブランド素材

- `ryudai-tools-logo.png`: ロゴの元画像（1024×1024、背景透過）。画面で使う画像はここから書き出す。
- `ryudai-tools-logo.svg`: 元画像を 4 色に減色してからなぞった SVG（約 11KB）。画面のロゴ（`web/public/brand/logo.svg`）とファビコン（`web/public/favicon.svg`）に使う。
- 書き出し先は `web/public/brand/` と `web/public/`（画面のビルドでアプリに埋め込まれる）。

| ファイル | 用途 |
|---|---|
| `logo-64.png` | ヘッダー（28px 表示） |
| `logo-192.png` | ログイン画面・OAuth の同意画面 |
| `logo-512.png` | 大きく表示するとき |
| `favicon-32.png` / `favicon.ico` | ブラウザのタブ |
| `apple-touch-icon.png` | iPhone のホーム画面（白地。透過だと黒く埋められるため） |

書き出し後は `pngquant --quality=70-95` で減色している。

配色（ロゴから抽出）: 深い青 `#01519d`、明るい青 `#0388e1`、青緑 `#08afbe`、珊瑚色 `#fd5e41`。
