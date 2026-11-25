// Package university reads the university's public calendar, timetables and handbooks.
package university

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const CalendarURL = "https://www.u-ryukyu.ac.jp/aboutus/calendar/"
const TimetableURL = "https://rais.skr.u-ryukyu.ac.jp/dc/?page_id=1281"
const HandbookURL = "https://rais.skr.u-ryukyu.ac.jp/dc/?page_id=665"

var Japan = time.FixedZone("Asia/Tokyo", 9*60*60)

type Source struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	Stale     bool      `json:"stale"`
	Warning   string    `json:"warning,omitempty"`
}

type Document struct {
	Title   string `json:"title"`
	Faculty string `json:"faculty,omitempty"`
	URL     string `json:"url"`
	Kind    string `json:"kind"`
}

type cachedBody struct {
	data         []byte
	source       Source
	retryAt      time.Time
	etag         string
	lastModified string
}

// PublicStore は公開情報の保存先（db.DB が実装）。取得した公式資料を再起動後も使い、
// 次の取得は ETag / Last-Modified の条件付きで行って、変わっていなければ本文を取り直さない。
type PublicStore interface {
	LoadPublic(key string) (value []byte, etag, lastModified string, fetchedAt time.Time, ok bool)
	SavePublic(key string, value []byte, etag, lastModified string) error
}

// refreshAfter は公式資料を取り直すまでの間隔。取り直しは条件付きなので、変わっていなければ軽い。
const refreshAfter = 24 * time.Hour

type Client struct {
	// Store があれば、取得した公式資料を保存して共有する（nil ならメモリだけ）。
	Store   PublicStore
	HTTP    *http.Client
	mu      sync.Mutex
	cache   map[string]cachedBody
	parseMu sync.Mutex
	parsed  map[parsedKey]parsedDocument
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("公式資料のリダイレクトが多すぎます")
		}
		_, err := officialURL(req.URL.String(), req.URL.String())
		return err
	}}, cache: make(map[string]cachedBody)}
}

// Only discovered official URLs can be fetched; callers cannot supply arbitrary URLs.
func officialURL(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	u := b.ResolveReference(r)
	if u.Host != "www.u-ryukyu.ac.jp" && u.Host != "rais.skr.u-ryukyu.ac.jp" {
		return "", fmt.Errorf("公式サイト以外のリンクです")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("HTTP のリンクではありません")
	}
	u.Scheme = "https"
	u.Fragment = ""
	return u.String(), nil
}

// fetch coalesces requests and keeps the last successful response on transient failures.
func (c *Client) fetch(ctx context.Context, rawURL string) ([]byte, Source, error) {
	rawURL, err := officialURL(rawURL, rawURL)
	if err != nil {
		return nil, Source{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	old, ok := c.cache[rawURL]
	if !ok && c.Store != nil {
		// 再起動後など、メモリに無ければ保存した内容を使う。
		if data, etag, lm, at, found := c.Store.LoadPublic(storeKey(rawURL)); found {
			old = cachedBody{data: data, source: Source{URL: rawURL, FetchedAt: at.UTC()}, retryAt: at.Add(refreshAfter), etag: etag, lastModified: lm}
			ok = true
			c.cache[rawURL] = old
		}
	}
	if ok && time.Now().Before(old.retryAt) {
		return old.data, old.source, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, Source{}, err
	}
	req.Header.Set("User-Agent", "ryudai-tools/academic-info")
	if ok {
		// 前回の内容があれば条件付きで取得し、変わっていなければ 304 で本文を受け取らない。
		if old.etag != "" {
			req.Header.Set("If-None-Match", old.etag)
		}
		if old.lastModified != "" {
			req.Header.Set("If-Modified-Since", old.lastModified)
		}
	}
	resp, err := c.HTTP.Do(req)
	var data []byte
	if err == nil {
		defer resp.Body.Close()
		if _, e := officialURL(resp.Request.URL.String(), resp.Request.URL.String()); e != nil {
			err = e
		} else if ok && resp.StatusCode == http.StatusNotModified {
			old.source = Source{URL: rawURL, FetchedAt: time.Now().UTC()}
			old.retryAt = time.Now().Add(refreshAfter)
			c.cache[rawURL] = old
			c.save(rawURL, old)
			return old.data, old.source, nil
		} else if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("公式資料の取得: HTTP %d", resp.StatusCode)
		} else {
			data, err = io.ReadAll(io.LimitReader(resp.Body, (20<<20)+1))
			if len(data) > 20<<20 {
				err = fmt.Errorf("公式資料が 20 MB を超えています")
			}
		}
	}
	if err != nil {
		if ok && ctx.Err() == nil {
			old.source.Stale = true
			old.source.Warning = "更新に失敗したため前回取得した資料です: " + err.Error()
			old.retryAt = time.Now().Add(5 * time.Minute)
			c.cache[rawURL] = old
			return old.data, old.source, nil
		}
		return nil, Source{}, err
	}
	src := Source{URL: rawURL, FetchedAt: time.Now().UTC()}
	// Bound memory, preserving the document being refreshed.
	total := len(data)
	for k, v := range c.cache {
		if k != rawURL {
			total += len(v.data)
		}
	}
	for len(c.cache) >= 64 || total > 64<<20 {
		var key string
		var oldest time.Time
		for k, v := range c.cache {
			if k != rawURL && (key == "" || v.source.FetchedAt.Before(oldest)) {
				key, oldest = k, v.source.FetchedAt
			}
		}
		if key == "" {
			break
		}
		total -= len(c.cache[key].data)
		delete(c.cache, key)
	}
	body := cachedBody{data: data, source: src, retryAt: time.Now().Add(refreshAfter), etag: resp.Header.Get("ETag"), lastModified: resp.Header.Get("Last-Modified")}
	c.cache[rawURL] = body
	c.save(rawURL, body)
	return data, src, nil
}

func storeKey(rawURL string) string { return "official:" + rawURL }

// save は取得した公式資料を保存先に書く（失敗しても取得結果はそのまま使う）。
func (c *Client) save(rawURL string, b cachedBody) {
	if c.Store == nil {
		return
	}
	if err := c.Store.SavePublic(storeKey(rawURL), b.data, b.etag, b.lastModified); err != nil {
		log.Printf("公式資料の保存に失敗: %v", err)
	}
}

func walk(n *html.Node, f func(*html.Node)) {
	f(n)
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		walk(ch, f)
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
func textOf(n *html.Node) string {
	var b strings.Builder
	walk(n, func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		if x.Data == "br" {
			b.WriteByte('\n')
		}
	})
	return strings.TrimSpace(b.String())
}
func compact(s string) string { return strings.Join(strings.Fields(normalize(s)), "") }
func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return r - '０' + '0'
		}
		if r == '（' {
			return '('
		}
		if r == '）' {
			return ')'
		}
		return r
	}, s)
}

var yearRE = regexp.MustCompile(`20\d{2}`)

// documents extracts links from the main content, retaining faculty headings.
func documents(data []byte, base string) ([]Document, error) {
	root, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	var content *html.Node
	walk(root, func(n *html.Node) {
		if attr(n, "id") == "content" {
			content = n
		}
	})
	if content == nil {
		content = root
	}
	var out []Document
	faculty := ""
	seen := map[string]bool{}
	walk(content, func(n *html.Node) {
		if n.Data == "p" || n.Data == "h2" || n.Data == "h3" || strings.Contains(attr(n, "class"), "su-spoiler-title") {
			t := compact(textOf(n))
			for _, f := range []string{"共通教育科目", "人文社会学部", "国際地域創造学部", "法文学部", "教育学部", "理学部", "医学部", "工学部", "農学部"} {
				if t == f {
					faculty = f
				}
			}
		}
		if n.Data != "a" {
			return
		}
		u, e := officialURL(base, attr(n, "href"))
		if e != nil || seen[u] {
			return
		}
		t := strings.TrimSpace(textOf(n))
		if t == "" {
			return
		}
		kind := "page"
		if strings.HasSuffix(strings.ToLower(strings.Split(u, "?")[0]), ".pdf") {
			kind = "pdf"
		}
		out = append(out, Document{Title: t, URL: u, Faculty: faculty, Kind: kind})
		seen[u] = true
	})
	return out, nil
}

func (c *Client) yearPage(ctx context.Context, index string, year int, term string) (string, Source, error) {
	data, src, err := c.fetch(ctx, index)
	if err != nil {
		return "", src, err
	}
	root, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return "", src, err
	}
	var docs []Document
	walk(root, func(n *html.Node) {
		if n.Data != "a" {
			return
		}
		u, e := officialURL(index, attr(n, "href"))
		if e == nil {
			docs = append(docs, Document{Title: textOf(n), URL: u})
		}
	})
	for _, d := range docs {
		t := compact(d.Title)
		if yearRE.FindString(t) != fmt.Sprint(year) || strings.Contains(t, "大学院") {
			continue
		}
		if index == TimetableURL && strings.Contains(t, "学部生用") && strings.Contains(t, term) && strings.Contains(t, "配当表") {
			return d.URL, src, nil
		}
		if index == HandbookURL && strings.Contains(t, "学生便覧") && !strings.Contains(t, "正誤") {
			return d.URL, src, nil
		}
	}
	return "", src, fmt.Errorf("%d 年度 %s の公式資料は掲載されていません", year, term)
}
