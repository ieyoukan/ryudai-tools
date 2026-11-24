// Package webclient はプロバイダ非依存の Web スクレイピング基盤を提供する。
// cookie セッション管理・ブラウザ相当ヘッダ・GET/POST・JS リダイレクト追従・
// HTML 補助（フォーム/hidden 抽出・URL 解決）など、どの教育システムにも使える下回りをまとめる。
package webclient

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

// UserAgent は全リクエストに付与するブラウザ相当の User-Agent。
// WebClass・教務システムはブラウザでの利用を前提にしているため、ブラウザと同じ User-Agent を送る。
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// browserHeaderTransport は全リクエストにブラウザ相当のヘッダを注入する RoundTripper。
type browserHeaderTransport struct {
	base http.RoundTripper
}

func (t *browserHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "ja,en-US;q=0.9,en;q=0.8")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	}
	return t.base.RoundTrip(req)
}

// Client はセッション（cookie）を保持する HTTP クライアント。
// 自動リダイレクトは追わず（ErrUseLastResponse）、JS リダイレクトは呼び出し側で扱う。
type Client struct {
	HTTPClient *http.Client
	BaseURL    string
}

// New はベース URL を持つ Client を生成する。
func New(baseURL string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("クッキージャー作成: %w", err)
	}
	return &Client{
		HTTPClient: &http.Client{
			Jar: jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &browserHeaderTransport{base: http.DefaultTransport},
		},
		BaseURL: strings.TrimRight(baseURL, "/"),
	}, nil
}

// NewFromCookies は既存の cookie を注入した Client を生成する。
func NewFromCookies(baseURL string, cookies []*http.Cookie) (*Client, error) {
	c, err := New(baseURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.BaseURL + "/")
	if err != nil {
		return nil, fmt.Errorf("ベース URL のパース: %w", err)
	}
	c.HTTPClient.Jar.SetCookies(u, cookies)
	return c, nil
}

// Cookies は BaseURL に対して現在保持している cookie を返す。
func (c *Client) Cookies() []*http.Cookie {
	u, err := url.Parse(c.BaseURL + "/")
	if err != nil {
		return nil
	}
	return c.HTTPClient.Jar.Cookies(u)
}

// BaseHost は BaseURL の scheme://host 部分を返す。
func (c *Client) BaseHost() string {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return c.BaseURL
	}
	return u.Scheme + "://" + u.Host
}

// FetchPage はベース URL に path を結合して GET する。
func (c *Client) FetchPage(path string) (*http.Response, error) {
	target := c.BaseURL + "/" + strings.TrimLeft(path, "/")
	resp, err := c.HTTPClient.Get(target)
	if err != nil {
		return nil, fmt.Errorf("%s の取得: %w", path, err)
	}
	return resp, nil
}

// FetchURL は URL に対して認証済み GET を行う。"/" 始まりはベースホストを補完する。
func (c *Client) FetchURL(rawURL string) (*http.Response, error) {
	if strings.HasPrefix(rawURL, "/") {
		rawURL = c.BaseHost() + rawURL
	}
	resp, err := c.HTTPClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL 取得 (%s): %w", rawURL, err)
	}
	return resp, nil
}

// DownloadFile は FetchPage の別名。呼び出し元が Body を閉じること。
func (c *Client) DownloadFile(path string) (*http.Response, error) {
	return c.FetchPage(path)
}

// jsRedirectPattern はシンプルな window.location.href リダイレクトを検出する。
var jsRedirectPattern = regexp.MustCompile(`window\.location\.href\s*=\s*"([^"]+)"`)

// FollowJSRedirect はページを取得し、単純な JS リダイレクトがあれば1段だけ追う。
func (c *Client) FollowJSRedirect(path string) (*http.Response, error) {
	resp, err := c.FetchPage(path)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("レスポンス読み取り: %w", err)
	}
	if m := jsRedirectPattern.FindSubmatch(body); len(m) > 1 {
		target := strings.TrimRight(c.BaseHost(), "/") + string(m[1])
		return c.HTTPClient.Get(target)
	}
	return &http.Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}, nil
}

// ResolveURL は相対/絶対パスを base に対して絶対 URL に解決する。
func (c *Client) ResolveURL(raw, base string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "&amp;", "&"))
	if strings.HasPrefix(raw, "http") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return c.BaseHost() + raw
	}
	if idx := strings.LastIndex(base, "/"); idx > 0 {
		return base[:idx+1] + raw
	}
	return c.BaseURL + "/" + raw
}
