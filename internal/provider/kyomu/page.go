package kyomu

import (
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

// page はパース済みの 1 ページ。ASP.NET の要素 ID で値を引けるよう索引を持つ。
type page struct {
	url string
	doc *html.Node
	ids map[string]*html.Node
}

func newPage(pageURL string, doc *html.Node) *page {
	p := &page{url: pageURL, doc: doc, ids: make(map[string]*html.Node)}
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode {
			if id := attr(n, "id"); id != "" {
				if _, dup := p.ids[id]; !dup {
					p.ids[id] = n
				}
			}
		}
		return true
	})
	return p
}

func parseHTML(pageURL, body string) (*page, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	return newPage(pageURL, doc), nil
}

// text は ID の要素のテキストを返す（<br> は改行、空白は正規化）。無ければ ""。
func (p *page) text(id string) string {
	n, ok := p.ids[id]
	if !ok {
		return ""
	}
	return nodeText(n)
}

// action は最初の <form> の送信先を絶対 URL で返す（無ければページ自身）。
func (p *page) action() string {
	var act string
	walk(p.doc, func(n *html.Node) bool {
		if act != "" {
			return false
		}
		if n.Type == html.ElementNode && n.Data == "form" {
			act = attr(n, "action")
			if act == "" {
				act = p.url
			}
			return false
		}
		return true
	})
	base, err := url.Parse(p.url)
	if err != nil || act == "" {
		return p.url
	}
	ref, err := url.Parse(act)
	if err != nil {
		return p.url
	}
	return base.ResolveReference(ref).String()
}

// has は ID の要素が存在するかを返す。
func (p *page) has(id string) bool {
	_, ok := p.ids[id]
	return ok
}

// formValues は <form> 内の送信対象値（hidden・text・select の選択値）を返す。
// submit / image ボタンは押したものだけ送るのが ASP.NET の約束なので含めない。
func (p *page) formValues() url.Values {
	vals := url.Values{}
	walk(p.doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		name := attr(n, "name")
		switch n.Data {
		case "input":
			if name == "" {
				return true
			}
			switch strings.ToLower(attr(n, "type")) {
			case "", "text", "hidden", "password":
				vals.Set(name, attr(n, "value"))
			case "checkbox", "radio":
				if hasAttr(n, "checked") {
					vals.Set(name, attrOr(n, "value", "on"))
				}
			}
		case "select":
			if name == "" {
				return false
			}
			first, selected := "", ""
			found := false
			walk(n, func(o *html.Node) bool {
				if o.Type == html.ElementNode && o.Data == "option" {
					v := attr(o, "value")
					if !found && first == "" {
						first = v
					}
					if hasAttr(o, "selected") && !found {
						selected, found = v, true
					}
				}
				return true
			})
			if found {
				vals.Set(name, selected)
			} else {
				vals.Set(name, first)
			}
			return false
		case "textarea":
			if name != "" {
				vals.Set(name, webclient.TextContent(n))
			}
		}
		return true
	})
	return vals
}

// Option は <select> の選択肢。
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// selectOptions は name 属性が一致する <select> の選択肢を返す。
func (p *page) selectOptions(name string) []Option {
	var out []Option
	walk(p.doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "select" && attr(n, "name") == name {
			walk(n, func(o *html.Node) bool {
				if o.Type == html.ElementNode && o.Data == "option" {
					out = append(out, Option{Value: attr(o, "value"), Label: squash(webclient.TextContent(o))})
				}
				return true
			})
			return false
		}
		return true
	})
	return out
}

// walk は DOM を先行順に辿る。fn が false を返すとその子は辿らない。
func walk(n *html.Node, fn func(*html.Node) bool) {
	if !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func attrOr(n *html.Node, key, def string) string {
	if v := attr(n, key); v != "" {
		return v
	}
	return def
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

var (
	spaceRun   = regexp.MustCompile(`[ \t\r\f\v\x{00a0}]+`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// nodeText は要素のテキストを <br> を改行として取り出し、行ごとに空白を整える。
func nodeText(n *html.Node) string {
	var sb strings.Builder
	walk(n, func(c *html.Node) bool {
		switch {
		case c.Type == html.TextNode:
			sb.WriteString(strings.ReplaceAll(c.Data, "\n", " "))
		case c.Type == html.ElementNode && (c.Data == "br" || c.Data == "p" || c.Data == "div" || c.Data == "tr"):
			sb.WriteString("\n")
		}
		return true
	})
	lines := strings.Split(sb.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRun.ReplaceAllString(l, " "))
	}
	s := strings.Join(lines, "\n")
	s = blankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// squash は改行も含めて空白を 1 つに潰す（1 行で表示する値向け）。
func squash(s string) string {
	s = strings.ReplaceAll(s, "　", " ")
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}

// gridRows は GridView の <table> を見出し名→列番号と、見出しを除く行のセル列に分解する。
// ネストしたテーブル（ページャ等）の行は含めない。
func gridRows(table *html.Node) (map[string]int, [][]*html.Node) {
	var rows [][]*html.Node
	walk(table, func(n *html.Node) bool {
		if n != table && n.Type == html.ElementNode && n.Data == "table" {
			return false
		}
		if n.Type == html.ElementNode && n.Data == "tr" {
			var cells []*html.Node
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
					cells = append(cells, c)
				}
			}
			rows = append(rows, cells)
			return false
		}
		return true
	})
	col := map[string]int{}
	if len(rows) == 0 {
		return col, nil
	}
	for i, h := range rows[0] {
		if name := squash(nodeText(h)); name != "" {
			col[name] = i
		}
	}
	return col, rows[1:]
}

// gridCell は行 r の見出し name 列のテキストを 1 行にして返す。
func gridCell(col map[string]int, r []*html.Node, name string) string {
	i, ok := col[name]
	if !ok || i >= len(r) {
		return ""
	}
	return squash(nodeText(r[i]))
}

// linkQuery は n 配下の最初の <a href> のクエリを返す。
func linkQuery(n *html.Node) url.Values {
	var q url.Values
	walk(n, func(c *html.Node) bool {
		if q != nil {
			return false
		}
		if c.Type == html.ElementNode && c.Data == "a" {
			if u, err := url.Parse(attr(c, "href")); err == nil {
				q = u.Query()
			}
			return false
		}
		return true
	})
	return q
}

// gridRowsWithHeader は key を含む行を見出しとして表を分解する（見出しの前にタイトル行がある表用）。
func gridRowsWithHeader(table *html.Node, key string) (map[string]int, [][]*html.Node) {
	col, rows := gridRows(table)
	if _, ok := col[key]; ok {
		return col, rows
	}
	for i, r := range rows {
		hdr := map[string]int{}
		for j, c := range r {
			if name := squash(nodeText(c)); name != "" {
				hdr[name] = j
			}
		}
		if _, ok := hdr[key]; ok {
			return hdr, rows[i+1:]
		}
	}
	return col, rows
}
