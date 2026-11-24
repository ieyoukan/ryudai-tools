// Package apidoc は経路の表（handler.Routes, ui.PageRoutes, ui.FileRoutes）から docs/api.md を作る。
// 文書は apidoc_test.go が生成結果と比べ、ずれていればテストが失敗する（mise run gen-api-doc で更新）。
package apidoc

import (
	"github.com/ieyoukan/ryudai-tools/internal/handler"
	"github.com/ieyoukan/ryudai-tools/internal/routes"
	"github.com/ieyoukan/ryudai-tools/internal/ui"
)

const intro = `
このファイルは internal/apidoc が経路の表から生成します。直接編集せず、経路の表（internal/handler/routes.go、
internal/ui/routes.go）を直して ` + "`mise run gen-api-doc`" + ` を実行してください。

- ログイン Cookie: 画面からログインしたときの ` + "`ryudai_tools_session`" + `。
- Bearer トークン: 設定画面で発行した API トークン（読み取り専用）を ` + "`Authorization: Bearer <トークン>`" + ` で送る。
- 応答の型は ` + "`web/src/api/gen`" + ` の TypeScript の型（Go の構造体から tygo で生成）。
- このほか ` + "`GET /api/health`" + `（版の確認）、` + "`/mcp`" + `（AI アプリ向けの MCP。Bearer トークンのみ）、` + "`/oauth/*`" + ` と ` + "`/.well-known/*`" + `（AI アプリとの連携）があります。
`

// Markdown は docs/api.md の内容を返す。
func Markdown() string {
	h := &ui.Handler{}
	return routes.Markdown("HTTP API", intro, []routes.Group{
		{
			Title:       "JSON API",
			Description: "教務システム（TIGLON）・公開資料・履修計画。AI アプリや自作のツールからも使える。",
			Routes:      (&handler.AcademicHandler{}).Routes(),
		},
		{
			Title:       "ページ API",
			Prefix:      "/api/page",
			Description: "画面（React）が各ページの表示に使う。GET 以外は `X-Ryudai-Page: 1` ヘッダーが必要。エラーは応答の `Error` に入る。",
			Routes:      h.PageRoutes(),
		},
		{
			Title:       "資料ファイル",
			Description: "WebClass の資料ファイルの中継とまとめてダウンロード。",
			Routes:      h.FileRoutes(),
		},
	})
}
