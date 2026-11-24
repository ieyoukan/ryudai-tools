package webclass

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var ErrSessionExpired = errors.New("WebClass のログインが切れています。再ログインしてください")

// SetSessionRecovery retries read requests once after an upstream login rejection.
// Requests that can submit answers or change data are never retried.
func (c *Client) SetSessionRecovery(recover func() ([]*http.Cookie, error)) {
	base, _ := url.Parse(c.BaseURL + "/")
	c.HTTPClient.Transport = &recoveryTransport{base: c.HTTPClient.Transport, client: c, origin: base, recover: recover}
}

type recoveryTransport struct {
	base    http.RoundTripper
	client  *Client
	origin  *url.URL
	recover func() ([]*http.Cookie, error)
}

func (t *recoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || req.Method != http.MethodGet || req.URL.Host != t.origin.Host || req.URL.Scheme != t.origin.Scheme || !strings.HasPrefix(req.URL.Path, t.origin.Path) {
		return resp, err
	}
	expired, err := loginRejected(resp)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	if !expired {
		return resp, nil
	}
	resp.Body.Close()
	cookies, err := t.recover()
	if err != nil {
		return nil, err
	}
	t.client.HTTPClient.Jar.SetCookies(t.origin, cookies)
	retry := req.Clone(req.Context())
	retry.Header.Del("Cookie")
	for _, cookie := range t.client.HTTPClient.Jar.Cookies(req.URL) {
		retry.AddCookie(cookie)
	}
	resp, err = t.base.RoundTrip(retry)
	if err != nil {
		return nil, err
	}
	expired, err = loginRejected(resp)
	if err != nil || expired {
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrSessionExpired
	}
	return resp, nil
}

var loginNavigation = regexp.MustCompile(`(?i)(?:location(?:\.href)?\s*=\s*["']|url\s*=\s*["']?)[^"'<>\s]*login\.php`)
var usernameInput = regexp.MustCompile(`(?i)name\s*=\s*["']?username(?:["'\s>])`)
var passwordInput = regexp.MustCompile(`(?i)type\s*=\s*["']?password(?:["'\s>])`)

func loginRejected(resp *http.Response) (bool, error) {
	if resp.StatusCode == http.StatusUnauthorized {
		return true, nil
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		u, err := url.Parse(resp.Header.Get("Location"))
		return err == nil && strings.HasSuffix(u.Path, "/login.php"), nil
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return false, nil
	}
	// Inspect a bounded prefix and preserve the complete response for its parser.
	prefix, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return false, err
	}
	resp.Body = &prefixedBody{Reader: io.MultiReader(bytes.NewReader(prefix), resp.Body), closer: resp.Body}
	return loginNavigation.Match(prefix) || (usernameInput.Match(prefix) && passwordInput.Match(prefix)), nil
}

type prefixedBody struct {
	io.Reader
	closer io.Closer
}

func (b *prefixedBody) Close() error { return b.closer.Close() }
