package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/ieyoukan/daigaku-hack/internal/db"
)

// authzRequest は /oauth/authorize のパラメータ。
type authzRequest struct {
	ClientID            string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	ResponseType        string
	Resource            string
	Scope               string
}

func parseAuthzRequest(v url.Values) authzRequest {
	return authzRequest{
		ClientID:            v.Get("client_id"),
		RedirectURI:         v.Get("redirect_uri"),
		State:               v.Get("state"),
		CodeChallenge:       v.Get("code_challenge"),
		CodeChallengeMethod: v.Get("code_challenge_method"),
		ResponseType:        v.Get("response_type"),
		Resource:            v.Get("resource"),
		Scope:               v.Get("scope"),
	}
}

func (a authzRequest) values() url.Values {
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set("response_type", a.ResponseType)
	set("client_id", a.ClientID)
	set("redirect_uri", a.RedirectURI)
	set("state", a.State)
	set("code_challenge", a.CodeChallenge)
	set("code_challenge_method", a.CodeChallengeMethod)
	set("resource", a.Resource)
	set("scope", a.Scope)
	return v
}

// handleAuthorize は認可リクエストを処理する。
//
//	GET  … 検証 → 未ログインならログインへ → 信頼済みクライアント・連携済みなら即コード発行、それ以外は同意画面
//	POST … 同意画面の「許可 / 拒否」
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, http.StatusBadRequest, "リクエストを読み取れませんでした。")
		return
	}
	var vals url.Values
	if r.Method == http.MethodPost {
		vals = r.PostForm
	} else {
		vals = r.URL.Query()
	}
	req := parseAuthzRequest(vals)

	// 1. クライアントと redirect_uri。ここが不正なときは絶対にリダイレクトしない（オープンリダイレクト防止）。
	if req.ClientID == "" {
		s.errorPage(w, http.StatusBadRequest, "client_id がありません。")
		return
	}
	client, err := s.lookupClient(r, req.ClientID)
	if err != nil {
		logf("クライアント %q の取得に失敗: %v", req.ClientID, err)
		s.errorPage(w, http.StatusBadRequest, "接続元のアプリを確認できませんでした。もう一度アプリ側から連携をやり直してください。")
		return
	}
	if req.RedirectURI == "" && len(client.RedirectURIs) == 1 {
		req.RedirectURI = client.RedirectURIs[0]
	}
	if !containsExact(client.RedirectURIs, req.RedirectURI) {
		s.errorPage(w, http.StatusBadRequest, "リダイレクト先が登録と一致しません。")
		return
	}

	// 2. ここからのエラーはクライアントへ返す。
	fail := func(code, desc string) {
		http.Redirect(w, r, withQuery(req.RedirectURI, url.Values{
			"error": {code}, "error_description": {desc}, "state": {req.State}, "iss": {s.BaseURL(r)},
		}), http.StatusFound)
	}
	if req.ResponseType != "code" {
		fail("unsupported_response_type", "response_type=code のみ対応しています")
		return
	}
	if req.CodeChallenge == "" || req.CodeChallengeMethod != "S256" {
		fail("invalid_request", "PKCE（code_challenge_method=S256）が必要です")
		return
	}
	if req.Resource == "" {
		req.Resource = s.Resource(r)
	} else if !sameResource(req.Resource, s.Resource(r)) {
		fail("invalid_target", "resource がこのサーバーの MCP と一致しません")
		return
	}

	// 3. ログイン。未ログインならログイン後にこの URL へ戻す。
	user := ""
	if s.CurrentUser != nil {
		user = s.CurrentUser(r)
	}
	if user == "" {
		back := "/oauth/authorize?" + req.values().Encode()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(back), http.StatusFound)
		return
	}

	// 4. 同意。信頼済みの redirect 先か、すでに連携済みなら省略する。
	if r.Method == http.MethodPost {
		if !hmac.Equal([]byte(vals.Get("csrf")), []byte(s.csrfToken(user, req))) {
			s.errorPage(w, http.StatusForbidden, "画面の有効期限が切れました。アプリ側から連携をやり直してください。")
			return
		}
		if vals.Get("decision") != "allow" {
			fail("access_denied", "ユーザーが連携を拒否しました")
			return
		}
	} else if !s.isTrusted(req.RedirectURI) {
		if _, err := s.Store.ActiveOAuthGrant(user, client.ClientID); err != nil {
			s.consentPage(w, user, client, req)
			return
		}
	}

	// 5. 認可コードを発行してクライアントへ戻す。
	code := randomToken("")
	err = s.Store.SaveOAuthCode(hashToken(code), db.OAuthCode{
		ClientID:      client.ClientID,
		UserID:        user,
		RedirectURI:   req.RedirectURI,
		CodeChallenge: req.CodeChallenge,
		Resource:      req.Resource,
		Scope:         Scope,
		ExpiresAt:     s.now().Add(codeTTL),
	})
	if err != nil {
		logf("認可コードの保存に失敗: %v", err)
		fail("server_error", "認可コードを発行できませんでした")
		return
	}
	http.Redirect(w, r, withQuery(req.RedirectURI, url.Values{
		"code": {code}, "state": {req.State}, "iss": {s.BaseURL(r)},
	}), http.StatusFound)
}

// lookupClient は client_id からクライアントを返す。URL なら CIMD として取得・キャッシュする。
func (s *Server) lookupClient(r *http.Request, clientID string) (*db.OAuthClient, error) {
	if !strings.HasPrefix(clientID, "https://") {
		c, err := s.Store.GetOAuthClient(clientID)
		if err != nil {
			return nil, err
		}
		if c.Kind != "dcr" {
			return nil, errors.New("未登録のクライアントです")
		}
		return c, nil
	}
	cached, err := s.Store.GetOAuthClient(clientID)
	if err == nil && cached.Kind == "cimd" && s.now().Sub(cached.CreatedAt) < cimdTTL {
		return cached, nil
	}
	fetch := s.FetchClientMetadata
	if fetch == nil {
		fetch = fetchClientMetadata
	}
	fresh, ferr := fetch(r.Context(), clientID)
	if ferr != nil {
		if err == nil && cached.Kind == "cimd" {
			return cached, nil // 取得に失敗しても、前回の内容があれば使う
		}
		return nil, ferr
	}
	fresh.CreatedAt = s.now()
	if err := s.Store.UpsertOAuthClient(*fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

func (s *Server) isTrusted(redirectURI string) bool {
	u, err := url.Parse(redirectURI)
	if err != nil || u.Scheme != "https" {
		return false
	}
	hosts := s.TrustedRedirectHosts
	if hosts == nil {
		hosts = DefaultTrustedRedirectHosts
	}
	return containsExact(hosts, strings.ToLower(u.Hostname()))
}

// csrfToken は同意画面の改ざん防止トークン（ユーザーとリクエスト内容に結びつく）。
func (s *Server) csrfToken(user string, req authzRequest) string {
	mac := hmac.New(sha256.New, append([]byte("ryudai-tools oauth consent\x00"), s.Key...))
	mac.Write([]byte(user))
	mac.Write([]byte{0})
	mac.Write([]byte(req.values().Encode()))
	return hex.EncodeToString(mac.Sum(nil))
}

func containsExact(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// --- 画面 ---

// 同意画面は CSRF の確認と埋め込み禁止があるので、画面（React）ではなくここで描画する。見た目は画面の配色に合わせている。
var pageTmpl = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="ja"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}} - Ryudai Tools</title><link rel="icon" href="/favicon.svg" type="image/svg+xml">
<style>
:root{color-scheme:light dark;--bg:#f6f6f5;--card:#fff;--fg:#0b0b0b;--muted:#6b6a66;--border:#e4e4e1;--primary:#2a78d6;--critical:#d03b3b}
@media (prefers-color-scheme:dark){:root{--bg:#0d0d0d;--card:#171717;--fg:#f0efec;--muted:#a3a29b;--border:#2c2c2a;--primary:#3987e5;--critical:#e66767}}
*{box-sizing:border-box;margin:0}
body{font-family:system-ui,-apple-system,"Hiragino Sans","Noto Sans JP",sans-serif;background:var(--bg);color:var(--fg);min-height:100svh;display:grid;place-items:center;padding:1rem}
.card{width:100%;max-width:24rem;background:var(--card);border:1px solid var(--border);border-radius:.75rem;padding:1.5rem}
.brand{display:flex;align-items:center;gap:.6rem;font-weight:600;margin-bottom:1.25rem}
h1{font-size:1.05rem;line-height:1.5;margin-bottom:.75rem}
p,li{font-size:.875rem;line-height:1.6}
.muted{color:var(--muted)}
ul{margin:.75rem 0 .75rem 1.2rem}
.error{color:var(--critical)}
button{width:100%;height:2.75rem;border-radius:.5rem;font:inherit;font-size:.9rem;font-weight:500;cursor:pointer;margin-top:.5rem}
.allow{background:var(--primary);color:#fff;border:0}
.deny{background:transparent;color:var(--fg);border:1px solid var(--border)}
</style></head>
<body><main class="card">
<div class="brand"><img src="/brand/logo.svg" alt="" width="28" height="28">Ryudai Tools</div>
{{if .Error}}<p class="error">{{.Error}}</p>{{else}}
<h1>{{.ClientName}} が Ryudai Tools との連携を求めています</h1>
<p class="muted">ログイン中: {{.User}}</p>
<ul>
  <li>シラバスの検索</li>
  <li>あなたの履修登録状況・空きコマに入れられる講義・履修プランの確認</li>
</ul>
<p class="muted">読み取りのみです。履修登録の追加・削除はできません。連携は設定画面からいつでも解除できます。</p>
<form method="POST" action="/oauth/authorize" style="margin-top:1rem">
  {{range $k, $v := .Params}}{{range $v}}<input type="hidden" name="{{$k}}" value="{{.}}">{{end}}{{end}}
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <button type="submit" name="decision" value="allow" class="allow">許可する</button>
  <button type="submit" name="decision" value="deny" class="deny">キャンセル</button>
</form>
<p class="muted" style="margin-top:1rem;font-size:.8rem">接続先: {{.RedirectHost}}</p>
{{end}}
</main></body></html>`))

type pageData struct {
	Title        string
	Error        string
	ClientName   string
	User         string
	RedirectHost string
	Params       url.Values
	CSRF         string
}

func (s *Server) renderPage(w http.ResponseWriter, status int, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.WriteHeader(status)
	if err := pageTmpl.Execute(w, d); err != nil {
		logf("画面の描画に失敗: %v", err)
	}
}

func (s *Server) errorPage(w http.ResponseWriter, status int, msg string) {
	s.renderPage(w, status, pageData{Title: "連携エラー", Error: msg})
}

func (s *Server) consentPage(w http.ResponseWriter, user string, c *db.OAuthClient, req authzRequest) {
	host := req.RedirectURI
	if u, err := url.Parse(req.RedirectURI); err == nil {
		host = u.Host
	}
	name := c.Name
	if name == "" {
		name = host
	}
	s.renderPage(w, http.StatusOK, pageData{
		Title:        "連携の確認",
		ClientName:   name,
		User:         user,
		RedirectHost: host,
		Params:       req.values(),
		CSRF:         s.csrfToken(user, req),
	})
}
