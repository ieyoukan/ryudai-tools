// Package kyomu は琉球大学 教務情報システム（TIGLON, ASP.NET WebForms）のスクレイパー。
//
// 未ログインで使える公開シラバス検索・詳細と、ログイン後の履修登録状況・
// 登録候補講義の取得（読み取り専用）を提供する。
// 履修登録の追加・削除などの書き込み操作は意図的に実装していない。
package kyomu

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

// DefaultBaseURL は教務システムのポータル URL。
const DefaultBaseURL = "https://tiglon.jim.u-ryukyu.ac.jp/portal"

// ErrSessionExpired はログインセッションが切れて RichTimeOut.aspx に飛ばされたことを示す。
var ErrSessionExpired = errors.New("教務システムのセッションが切れています")

// ErrLoginFailed は ID またはパスワードが違うなどでログインできなかったことを示す。
var ErrLoginFailed = errors.New("教務システムへのログインに失敗しました")

// Client は教務システムとのセッション（cookie）を保持する HTTP クライアント。
// ASP.NET のセッションは同一セッション内のリクエストを直列化するため、
// 並列に取得したい場合は Client を複数作ること。
type Client struct {
	wc              *webclient.Client
	loginSchedule   *page
	loginScheduleAt time.Time
}

// New はベース URL（例: https://tiglon.jim.u-ryukyu.ac.jp/portal）の Client を生成する。
func New(baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	wc, err := webclient.New(baseURL)
	if err != nil {
		return nil, err
	}
	// 教務システムはシラバス詳細などで 302 を挟んでセッションを張るため、リダイレクトは追う。
	wc.HTTPClient.CheckRedirect = nil
	wc.HTTPClient.Timeout = 60 * time.Second
	return &Client{wc: wc}, nil
}

// NewFromCookies は既存の cookie を注入した Client を生成する。
func NewFromCookies(baseURL string, cookies []*http.Cookie) (*Client, error) {
	c, err := New(baseURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.wc.BaseHost() + "/")
	if err != nil {
		return nil, err
	}
	c.wc.HTTPClient.Jar.SetCookies(u, cookies)
	return c, nil
}

// Cookies は現在保持している cookie を返す（ホスト直下 path=/ のもの）。
func (c *Client) Cookies() []*http.Cookie {
	u, err := url.Parse(c.wc.BaseHost() + "/")
	if err != nil {
		return nil
	}
	return c.wc.HTTPClient.Jar.Cookies(u)
}

// abs はベース URL からの相対パスを絶対 URL にする。
func (c *Client) abs(path string) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	return c.wc.BaseURL + "/" + strings.TrimLeft(path, "/")
}

// Raw はパスを GET して最終 URL と生 HTML を返す（デバッグ用）。
func (c *Client) Raw(path string) (string, string, error) {
	resp, err := c.wc.HTTPClient.Get(c.abs(path))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.Request.URL.String(), string(body), err
}

// get は GET してパース済みページを返す。
func (c *Client) get(path string) (*page, error) {
	target := c.abs(path)
	resp, err := c.wc.HTTPClient.Get(target)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", target, err)
	}
	return readPage(resp)
}

// post はフォームを POST してパース済みページを返す。
func (c *Client) post(path string, vals url.Values) (*page, error) {
	target := c.abs(path)
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", target)
	resp, err := c.wc.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", target, err)
	}
	return readPage(resp)
}

// postback は ASP.NET の __doPostBack(target, arg) 相当の POST を行う。
// フォームの現在値（hidden・select・text）を引き継ぎ、overrides で上書きする。
func (c *Client) postback(p *page, target, arg string, overrides map[string]string) (*page, error) {
	vals := p.formValues()
	vals.Set("__EVENTTARGET", target)
	vals.Set("__EVENTARGUMENT", arg)
	for k, v := range overrides {
		vals.Set(k, v)
	}
	return c.post(p.action(), vals)
}

// submit は name のボタンを押したことにしてフォームを送信する。
func (c *Client) submit(p *page, button, value string, overrides map[string]string) (*page, error) {
	vals := p.formValues()
	vals.Set("__EVENTTARGET", "")
	vals.Set("__EVENTARGUMENT", "")
	for k, v := range overrides {
		vals.Set(k, v)
	}
	vals.Set(button, value)
	return c.post(p.action(), vals)
}

func readPage(resp *http.Response) (*page, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("レスポンス読み取り: %w", err)
	}
	finalURL := resp.Request.URL.String()
	if strings.Contains(finalURL, "RichTimeOut.aspx") {
		return nil, ErrSessionExpired
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, finalURL)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("HTML パース: %w", err)
	}
	return newPage(finalURL, doc), nil
}
