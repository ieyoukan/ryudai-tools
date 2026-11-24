// Package routes は HTTP の経路を「表」として定義するための小さな部品。
//
// 各パッケージは経路の一覧（メソッド・パス・認証・説明・応答の型）を返し、サーバーはその一覧から
// chi に登録する。同じ一覧から docs/api.md を生成するので（internal/apidoc）、コードと文書がずれない。
package routes

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Auth は経路を使うのに必要な認証。
type Auth string

const (
	Public         Auth = "不要"
	Session        Auth = "ログイン Cookie"
	SessionOrToken Auth = "ログイン Cookie または Bearer トークン"
	OptionalUser   Auth = "不要（ログイン中なら本人の情報も使う）"
)

// Route は 1 つの経路。
type Route struct {
	Method  string
	Path    string
	Auth    Auth
	Summary string
	// Params はクエリまたは本文の主なパラメータ（文書用）。
	Params string
	// Response は応答の型（web/src/api/gen の TypeScript の型名など）。空なら書かない。
	Response string
	Handler  http.HandlerFunc
}

// Group は文書の 1 節にまとめる経路の集まり。Prefix は登録先のパスの前に付く部分（文書にも出す）。
type Group struct {
	Title       string
	Prefix      string
	Description string
	Routes      []Route
}

// Register は rs を r に登録する。同じメソッドとパスが重なっていたら panic する（設定の誤り）。
func Register(r chi.Router, rs []Route) {
	seen := map[string]bool{}
	for _, rt := range rs {
		key := rt.Method + " " + rt.Path
		if seen[key] {
			panic("routes: duplicate route " + key)
		}
		seen[key] = true
		r.MethodFunc(rt.Method, rt.Path, rt.Handler)
	}
}

// Markdown は経路の一覧を Markdown の表にする。
func Markdown(title, intro string, groups []Group) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", title, strings.TrimSpace(intro))
	for _, g := range groups {
		fmt.Fprintf(&b, "\n## %s\n\n", g.Title)
		if g.Description != "" {
			fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(g.Description))
		}
		rs := append([]Route(nil), g.Routes...)
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].Path != rs[j].Path {
				return rs[i].Path < rs[j].Path
			}
			return methodOrder(rs[i].Method) < methodOrder(rs[j].Method)
		})
		b.WriteString("| メソッド | パス | パラメータ | 認証 | 内容 | 応答 |\n|---|---|---|---|---|---|\n")
		for _, rt := range rs {
			resp := ""
			if rt.Response != "" {
				resp = "`" + rt.Response + "`"
			}
			params := ""
			if rt.Params != "" {
				params = "`" + rt.Params + "`"
			}
			fmt.Fprintf(&b, "| %s | `%s%s` | %s | %s | %s | %s |\n", rt.Method, g.Prefix, rt.Path, params, rt.Auth, rt.Summary, resp)
		}
	}
	return b.String()
}

func methodOrder(m string) int {
	return strings.Index("GET POST PUT DELETE", m)
}

// New は経路を作る（表を短く書くため）。
func New(method, path string, auth Auth, summary string, h http.HandlerFunc) Route {
	return Route{Method: method, Path: path, Auth: auth, Summary: summary, Handler: h}
}

// Returns は応答の型を文書に書く。
func (rt Route) Returns(typeName string) Route {
	rt.Response = typeName
	return rt
}

// With は主なパラメータを文書に書く。
func (rt Route) With(params string) Route {
	rt.Params = params
	return rt
}
