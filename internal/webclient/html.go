package webclient

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	// 複数形式の JS リダイレクト（WebClass 等）。
	jsRedirectRes = []*regexp.Regexp{
		regexp.MustCompile(`window\.top\.location\.href\s*=\s*"([^"]+?)(?:\.replace\(/&amp;/g,"&"\))?["\s]`),
		regexp.MustCompile(`window\.location\.href\s*=\s*"([^"]+?)"`),
		regexp.MustCompile(`location\.href\s*=\s*'([^']+?)'`),
		regexp.MustCompile(`<a\s+href="([^"]+)"></a>`),
	}
	framesetRe   = regexp.MustCompile(`(?i)<frameset`)
	frameSrcRe   = regexp.MustCompile(`(?i)<frame\b[^>]+\bsrc=["']([^"']+)["']`)
	formActionRe = regexp.MustCompile(`(?i)<form[^>]+\baction=["']([^"']+)["']`)
	hiddenInpRe  = regexp.MustCompile(`(?i)<input\b[^>]+\btype=["']?hidden["']?[^>]*>`)
	inpNameRe    = regexp.MustCompile(`(?i)\bname=["']([^"']+)["']`)
	inpValueRe   = regexp.MustCompile(`(?i)\bvalue=["']([^"']*?)["']`)
)

// JSRedirect はボディから JS リダイレクト先を絶対 URL で返す。無ければ ""。
func (c *Client) JSRedirect(body []byte, base string) string {
	decoded := strings.ReplaceAll(string(body), "&amp;", "&")
	for _, pat := range jsRedirectRes {
		if m := pat.FindStringSubmatch(decoded); len(m) > 1 {
			return c.ResolveURL(strings.ReplaceAll(m[1], `\/`, "/"), base)
		}
	}
	return ""
}

// FrameSrcs は FRAMESET ボディから全 <frame src> を返す（&amp; デコード済み）。
// FRAMESET でなければ nil。どのフレームを選ぶかは呼び出し側の方針。
func (c *Client) FrameSrcs(body []byte) []string {
	if !framesetRe.Match(body) {
		return nil
	}
	matches := frameSrcRe.FindAllSubmatch(body, -1)
	srcs := make([]string, 0, len(matches))
	for _, m := range matches {
		srcs = append(srcs, strings.ReplaceAll(string(m[1]), "&amp;", "&"))
	}
	return srcs
}

// FormActionURL はボディ内の最初の <form action> を絶対 URL で返す。無ければ ""。
func (c *Client) FormActionURL(body []byte, base string) string {
	m := formActionRe.FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return c.ResolveURL(string(m[1]), base)
}

// HiddenInputsFromHTML は HTML 文字列中の全 hidden input を name→value で返す。
// CSRF トークンや ASP.NET の __VIEWSTATE 等の取得に使える。
func HiddenInputsFromHTML(body []byte) map[string]string {
	out := make(map[string]string)
	for _, hm := range hiddenInpRe.FindAll(body, -1) {
		nm := inpNameRe.FindSubmatch(hm)
		if len(nm) < 2 {
			continue
		}
		v := ""
		if vm := inpValueRe.FindSubmatch(hm); len(vm) > 1 {
			v = strings.ReplaceAll(string(vm[1]), "&amp;", "&")
		}
		out[string(nm[1])] = v
	}
	return out
}

// AttrMap は要素ノードの属性を map で返す。
func AttrMap(n *html.Node) map[string]string {
	m := make(map[string]string, len(n.Attr))
	for _, a := range n.Attr {
		m[a.Key] = a.Val
	}
	return m
}

// TextContent はノード配下の全テキストを連結して返す。
func TextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(TextContent(c))
	}
	return sb.String()
}

// HiddenInputsNode はパース済み DOM から全 hidden input を name→value で返す。
func HiddenInputsNode(doc *html.Node) map[string]string {
	result := make(map[string]string)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			attrs := AttrMap(n)
			if attrs["type"] == "hidden" && attrs["name"] != "" {
				result[attrs["name"]] = attrs["value"]
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return result
}

// FindTableByClass は class 属性に target を含む最初の <table> を返す。
func FindTableByClass(doc *html.Node, target string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "table" {
			if strings.Contains(AttrMap(n)["class"], target) {
				found = n
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

// FindTableByID は id 属性が target に一致する最初の <table> を返す。
func FindTableByID(doc *html.Node, target string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "table" {
			if AttrMap(n)["id"] == target {
				found = n
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

// TableRows は <table> 直下の <tr> を返す（ネストしたテーブルは除く）。
func TableRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node, int)
	walk = func(n *html.Node, depth int) {
		if depth > 0 && n.Type == html.ElementNode && n.Data == "table" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "tr" {
			rows = append(rows, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, depth+1)
		}
	}
	walk(table, 0)
	return rows
}

// RowCells は <tr> から <td> / <th> を返す。
func RowCells(row *html.Node) []*html.Node {
	var cells []*html.Node
	for c := row.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cells = append(cells, c)
		}
	}
	return cells
}
