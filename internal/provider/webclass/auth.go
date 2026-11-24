package webclass

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

// FetchLoginToken はログインページを取得して隠し CSRF トークンを抽出する。
func (c *Client) FetchLoginToken() (string, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/login.php?language=JAPANESE")
	if err != nil {
		return "", fmt.Errorf("ログインページ取得: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ログインページで予期しないステータス %d", resp.StatusCode)
	}
	return extractToken(resp.Body)
}

// extractToken は HTML から <input type="hidden" name="token" value="..."> を探す。
func extractToken(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("HTML パース: %w", err)
	}
	token := webclient.HiddenInputsNode(doc)["token"]
	if token == "" {
		return "", fmt.Errorf("ログインページ HTML にトークンが見つからない")
	}
	return token, nil
}

// Login はトークン取得 → POST のフルログインフローを実行し、認証 cookie を返す。
func (c *Client) Login(username, password string) ([]*http.Cookie, error) {
	token, err := c.FetchLoginToken()
	if err != nil {
		return nil, fmt.Errorf("トークン取得: %w", err)
	}
	return c.loginWithToken(username, password, token)
}

// loginWithToken は事前取得済みトークンで POST ログインを行う。
// WCAC=Authenticated クッキーの有無で成否を判定する。
func (c *Client) loginWithToken(username, password, token string) ([]*http.Cookie, error) {
	form := url.Values{}
	form.Set("username", username)
	form.Set("val", password)
	form.Set("token", token)
	form.Set("language", "JAPANESE")
	form.Set("useragent", webclient.UserAgent)
	form.Set("login", "ログイン")

	req, err := http.NewRequest("POST", c.BaseURL+"/login.php?language=JAPANESE", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("ログインリクエスト作成: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ログイン POST: %w", err)
	}
	defer resp.Body.Close()

	// Jar から返る Cookie には Expires が含まれない。レスポンスの属性を
	// 引き継ぐことで、呼び出し元が再ログインのタイミングを判断できる。
	cookies := mergeCookieAttributes(c.Cookies(), resp.Cookies())
	if !hasAuthCookie(cookies) {
		return nil, fmt.Errorf("ログイン失敗: 認証情報が無効")
	}
	return cookies, nil
}

func mergeCookieAttributes(jarCookies, responseCookies []*http.Cookie) []*http.Cookie {
	for _, jarCookie := range jarCookies {
		for _, responseCookie := range responseCookies {
			if jarCookie.Name != responseCookie.Name || responseCookie.Expires.IsZero() {
				continue
			}
			jarCookie.Expires = responseCookie.Expires
			jarCookie.Secure = responseCookie.Secure
			jarCookie.HttpOnly = responseCookie.HttpOnly
			jarCookie.SameSite = responseCookie.SameSite
		}
	}
	return jarCookies
}

func hasAuthCookie(cookies []*http.Cookie) bool {
	for _, c := range cookies {
		if c.Name == "WCAC" && c.Value == "Authenticated" {
			return true
		}
	}
	return false
}
