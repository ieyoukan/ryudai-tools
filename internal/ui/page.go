package ui

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/routes"
)

// 画面（web/ の React）は、各ページのデータをこのパッケージのハンドラーから JSON で受け取る。
// ハンドラーはページの URL と同じパスで /api/page 以下にまとめ（例: /courses → /api/page/courses）、
// 描画の代わりにデータの構造体（CoursesData など）をそのまま JSON にして返す。
// 型は mise run gen-types で web/src/api/gen/ui.ts に書き出す。

// render はページのデータを JSON で返す。
func (h *Handler) render(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		log.Printf("ページのデータの書き出しエラー (%s): %v", page, err)
	}
}

// pageWriter はリダイレクト（ログイン画面へ・保存後の戻り先など）を {"redirect": "/login"} の JSON に変える。
// fetch はリダイレクトを勝手にたどってしまうので、画面側が自分で移動できるようにする。
type pageWriter struct {
	http.ResponseWriter
	redirected bool
}

func (p *pageWriter) WriteHeader(code int) {
	if code >= 300 && code < 400 {
		loc := p.Header().Get("Location")
		p.Header().Del("Location")
		p.Header().Set("Content-Type", "application/json; charset=utf-8")
		p.Header().Set("Cache-Control", "no-store")
		p.ResponseWriter.WriteHeader(http.StatusOK)
		json.NewEncoder(p.ResponseWriter).Encode(map[string]string{"redirect": loc})
		p.redirected = true
		return
	}
	p.ResponseWriter.WriteHeader(code)
}

func (p *pageWriter) Write(b []byte) (int, error) {
	if p.redirected {
		return len(b), nil // http.Redirect が書く HTML の本文は捨てる
	}
	return p.ResponseWriter.Write(b)
}

// PageHeader は画面からの書き込み（POST）に付けるヘッダー。付いていない POST は受け付けない。
// 独自のヘッダーは他サイトのフォームからは送れず、fetch でも CORS の確認が要るので、なりすましの送信を防げる。
const PageHeader = "X-Ryudai-Page"

// PageAPI は画面用の JSON のハンドラー（/api/page 以下にマウントする）。
func (h *Handler) PageAPI() http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodGet && req.Header.Get(PageHeader) == "" {
				http.Error(w, `{"error":"X-Ryudai-Page ヘッダーが必要です"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(&pageWriter{ResponseWriter: w}, req)
		})
	})
	routes.Register(r, h.PageRoutes())
	return r
}

// ShowMe は GET /api/page/me（ログイン中のユーザー名。未ログインなら空）を処理する。画面のヘッダーが使う。
func (h *Handler) ShowMe(w http.ResponseWriter, r *http.Request) {
	h.render(w, "me", h.policyData(r))
}
