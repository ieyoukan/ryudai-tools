// Package webapp は web/（React + TypeScript）をビルドした画面を配信する。
// ビルド結果は dist/ に出力され、go:embed でバイナリに入るので、配布するのは実行ファイル 1 つだけでよい。
//
// ページの URL（/courses や /grades など）にはどれも同じ index.html を返し、どのページを出すかは画面側が URL から決める。
// ページのデータは画面が /api/page/... や /api/me/... から JSON で読む。
package webapp

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

var sub, _ = fs.Sub(dist, "dist")

// Built は画面がビルド済み（dist/index.html がある）か。
func Built() bool {
	_, err := fs.Stat(sub, "index.html")
	return err == nil
}

// Index はページの URL に画面の index.html を返す。
func Index() http.Handler { return indexWithStatus(http.StatusOK) }

// NotFound は存在しない URL に、画面の index.html を 404 で返す（画面が「見つかりません」を出す）。
func NotFound() http.Handler { return indexWithStatus(http.StatusNotFound) }

func indexWithStatus(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "画面がビルドされていません（mise run web を実行してください）", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		w.Write(index)
	})
}

// Files はビルド結果のファイル（/assets/ の JS・CSS、/brand/ のロゴ、ファビコン）を返す。無ければ 404。
func Files() http.Handler {
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(sub, name); err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			// ファイル名にハッシュが付くので長くキャッシュしてよい
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		files.ServeHTTP(w, r)
	})
}
