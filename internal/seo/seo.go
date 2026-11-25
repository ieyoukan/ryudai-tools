// Package seo は検索エンジンとリンクのプレビュー（OGP）向けの情報を作る。
//
// 画面は React で描くが、検索エンジンや SNS のプレビューは JavaScript を実行しないことがあるため、
// ページの URL ごとのタイトル・説明・正規 URL・OGP・検索に出すかどうかを、サーバーが index.html の
// <head> に入れて返す。検索に出すのはログイン不要の公開ページだけで、ログインが必要なページは noindex にする。
package seo

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

// SiteName はサイト名。
const SiteName = "Ryudai Tools"

// Tagline は検索結果やプレビューに出す短い紹介。
const Tagline = "琉大生のための非公式の大学生活支援ツール"

// Page は 1 ページ分の検索向けの情報。
type Page struct {
	Title       string // <title>（サイト名を含む完成形）
	Description string
	// Index は検索エンジンに載せてよいか（ログイン不要の公開ページだけ true）。
	Index bool
	// Canonical は正規 URL のパス（空ならリクエストのパス）。
	Canonical string
}

const defaultDescription = "琉球大学の WebClass と教務情報システムをまとめて見やすくする、学生が作った非公式のツール。時間割・授業資料のまとめてダウンロード・成績と卒業要件の見通し・空きコマに入る講義の候補・シラバス検索に対応。パスワードは保存しません。"

// public はログイン不要で、検索に載せるページ。sitemap.xml にもこの順で載せる。
var public = []struct {
	path string
	page Page
}{
	{"/login", Page{Title: SiteName + " - " + Tagline, Description: defaultDescription}},
	{"/syllabus", Page{Title: "シラバス検索 - " + SiteName, Description: "琉球大学のシラバスを、曜日・時限・学部・キーワードで検索。単位数・登録方法・授業形態と概要を一覧で比べられます（ログイン不要）。"}},
	{"/downloads", Page{Title: "ダウンロード - " + SiteName, Description: "Ryudai Tools を自分のパソコンやサーバーで動かすための配布物（macOS・Linux・Windows）。ソースコードは GNU AGPL v3 で公開しています。"}},
	{"/terms", Page{Title: "利用規約 - " + SiteName, Description: "Ryudai Tools の利用規約。琉球大学の公式サービスではない、学生が個人で作った非公式のツールです。"}},
	{"/privacy", Page{Title: "プライバシーポリシー - " + SiteName, Description: "Ryudai Tools のプライバシーポリシー。パスワードは保存せず、ログイン Cookie は暗号化して保存します。"}},
}

// ForPath はページのパスに対する検索向けの情報を返す。公開ページ以外は noindex。
func ForPath(path string) Page {
	if path == "/" {
		path = "/login"
	}
	for _, p := range public {
		if path == p.path {
			page := p.page
			page.Index = true
			page.Canonical = p.path
			return page
		}
	}
	if strings.HasPrefix(path, "/syllabus/") {
		// シラバス詳細は大学の公式シラバスと同じ内容なので検索には載せない（クローラーの巡回が
		// そのまま教務システムへの取得になるのも避ける）。リンクのプレビューは出す。
		return Page{Title: "シラバス - " + SiteName, Description: "琉球大学のシラバス詳細（" + SiteName + "）。"}
	}
	return Page{Title: SiteName, Description: defaultDescription}
}

// Head は index.html の <head> に入れるタグ（<title> を含む）を返す。base は公開 URL（末尾の / なし）。
func Head(p Page, base, path string) string {
	canonical := p.Canonical
	if canonical == "" {
		canonical = path
	}
	e := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, "<title>%s</title>\n", e(p.Title))
	fmt.Fprintf(&b, `    <meta name="description" content="%s" />`+"\n", e(p.Description))
	if p.Index {
		fmt.Fprintf(&b, `    <link rel="canonical" href="%s" />`+"\n", e(base+canonical))
	} else {
		b.WriteString(`    <meta name="robots" content="noindex, nofollow" />` + "\n")
	}
	fmt.Fprintf(&b, `    <meta property="og:site_name" content="%s" />`+"\n", e(SiteName))
	fmt.Fprintf(&b, `    <meta property="og:type" content="website" />`+"\n")
	fmt.Fprintf(&b, `    <meta property="og:title" content="%s" />`+"\n", e(p.Title))
	fmt.Fprintf(&b, `    <meta property="og:description" content="%s" />`+"\n", e(p.Description))
	fmt.Fprintf(&b, `    <meta property="og:url" content="%s" />`+"\n", e(base+canonical))
	fmt.Fprintf(&b, `    <meta property="og:image" content="%s" />`+"\n", e(base+"/brand/logo-512.png"))
	fmt.Fprintf(&b, `    <meta property="og:locale" content="ja_JP" />`+"\n")
	b.WriteString(`    <meta name="twitter:card" content="summary" />` + "\n")
	if canonical == "/login" && p.Index {
		fmt.Fprintf(&b, `    <script type="application/ld+json">{"@context":"https://schema.org","@type":"WebApplication","name":%q,"description":%q,"url":%q,"applicationCategory":"EducationalApplication","operatingSystem":"Web","inLanguage":"ja","isAccessibleForFree":true}</script>`+"\n",
			SiteName, Tagline, base+"/")
	}
	return b.String()
}

// Private は検索に載せないパスか（robots.txt と X-Robots-Tag 用）。
var Private = []string{"/api/", "/courses", "/download", "/grades", "/plan", "/registration", "/lectures/", "/messages", "/settings", "/oauth/", "/mcp", "/syllabus/"}

// RobotsTxt は robots.txt の内容。
func RobotsTxt(base string) string {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for _, p := range Private {
		fmt.Fprintf(&b, "Disallow: %s\n", p)
	}
	fmt.Fprintf(&b, "\nSitemap: %s/sitemap.xml\n", base)
	return b.String()
}

// SitemapXML は公開ページの sitemap.xml の内容。
func SitemapXML(base string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range public {
		fmt.Fprintf(&b, "  <url><loc>%s</loc></url>\n", html.EscapeString(base+p.path))
	}
	b.WriteString("</urlset>\n")
	return b.String()
}

// BaseURL は公開 URL（末尾の / なし）。configured（PUBLIC_BASE_URL）が空ならリクエストから組み立てる。
func BaseURL(r *http.Request, configured string) string {
	if configured != "" {
		return strings.TrimRight(configured, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
