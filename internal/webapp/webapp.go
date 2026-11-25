// Package webapp は web/（React + TypeScript）をビルドした画面を配信する。
// ビルド結果は dist/ に出力され、go:embed でバイナリに入るので、配布するのは実行ファイル 1 つだけでよい。
//
// ページの URL（/courses や /grades など）にはどれも同じ index.html を返し、どのページを出すかは画面側が URL から決める。
// ページのデータは画面が /api/page/... や /api/me/... から JSON で読む。
package webapp

import (
	"bytes"
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

// Head はページごとに index.html の <head> へ入れるタグ（<title> を含む）と、検索に載せないかを返す。
type Head func(r *http.Request) (tags string, noindex bool)

// Index はページの URL に画面の index.html を返す。head があれば <title> をそのページ用のタグ
// （説明・正規 URL・OGP など）に置き換える。検索や SNS のプレビューは JavaScript を実行しないことがあるため。
func Index(head Head) http.Handler { return indexWithStatus(http.StatusOK, head) }

// NotFound は存在しない URL に、画面の index.html を 404 で返す（画面が「見つかりません」を出す）。
func NotFound(head Head) http.Handler { return indexWithStatus(http.StatusNotFound, head) }

var defaultTitle = []byte("<title>Ryudai Tools</title>")

func indexWithStatus(status int, head Head) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "画面がビルドされていません（mise run web を実行してください）", http.StatusServiceUnavailable)
			return
		}
		if head != nil {
			tags, noindex := head(r)
			index = bytes.Replace(index, defaultTitle, []byte(tags), 1)
			if noindex || status != http.StatusOK {
				w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			}
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
