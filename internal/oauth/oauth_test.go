package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/db"
)

const (
	testBase      = "https://dh.example.com"
	chatgptClient = "https://chatgpt.com/oauth/client.json"
	chatgptRedir  = "https://chatgpt.com/connector_platform_oauth_redirect"
)

// ChatGPT が実際に公開しているクライアント情報（2026-09 時点）。
const chatgptMetadata = `{"client_id":"https://chatgpt.com/oauth/client.json","client_uri":"https://chatgpt.com/","redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"private_key_jwt","token_endpoint_auth_methods_supported":["none","private_key_jwt"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"client_name":"ChatGPT","token_endpoint_auth_signing_alg":"RS256","jwks_uri":"https://chatgpt.com/oauth/jwks.json"}`

type fixture struct {
	t      *testing.T
	srv    *Server
	db     *db.DB
	router http.Handler
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: database, now: time.Now()}
	f.srv = &Server{
		Store:         database,
		PublicBaseURL: testBase,
		Key:           []byte("0123456789abcdef0123456789abcdef"),
		CurrentUser: func(r *http.Request) string {
			if c, err := r.Cookie("login"); err == nil {
				return c.Value
			}
			return ""
		},
		FetchClientMetadata: func(_ context.Context, id string) (*db.OAuthClient, error) {
			if id == chatgptClient {
				return parseClientMetadata(id, []byte(chatgptMetadata))
			}
			return nil, errors.New("unknown client")
		},
		Now: func() time.Time { return f.now },
	}
	r := chi.NewRouter()
	f.srv.Routes(r)
	f.router = r
	return f
}

func (f *fixture) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func pkcePair() (verifier, challenge string) {
	verifier = randomToken("")
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeURL(clientID, redirect, challenge string, extra url.Values) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"state":                 {"xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {testBase + "/mcp"},
		"scope":                 {Scope},
	}
	for k, v := range extra {
		q[k] = v
	}
	return "/oauth/authorize?" + q.Encode()
}

// authorize はログイン済みユーザーで認可し、リダイレクト先の URL を返す。
func (f *fixture) authorize(user, target string) *url.URL {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if user != "" {
		req.AddCookie(&http.Cookie{Name: "login", Value: user})
	}
	rec := f.do(req)
	if rec.Code != http.StatusFound {
		f.t.Fatalf("authorize: code=%d body=%s", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}

func (f *fixture) token(form url.Values) (int, map[string]any) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := f.do(req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Header().Get("Cache-Control") != "no-store" {
		f.t.Error("トークン応答に Cache-Control: no-store が無い")
	}
	return rec.Code, out
}

func (f *fixture) verify(token string) (string, error) {
	return f.srv.VerifyAccessToken(httptest.NewRequest(http.MethodPost, "/mcp", nil), token)
}

func (f *fixture) grantCount(user string) int {
	gs, err := f.db.ListOAuthGrants(user)
	if err != nil {
		f.t.Fatal(err)
	}
	return len(gs)
}

func TestMetadataIsConsistent(t *testing.T) {
	f := newFixture(t)
	get := func(path string) map[string]any {
		rec := f.do(httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		var m map[string]any
		json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}
	pr := get("/.well-known/oauth-protected-resource")
	get("/.well-known/oauth-protected-resource/mcp") // RFC 9728 のパス付き形式も返す
	as := get("/.well-known/oauth-authorization-server")
	if pr["resource"] != testBase+"/mcp" {
		t.Errorf("resource = %v", pr["resource"])
	}
	if pr["authorization_servers"].([]any)[0] != as["issuer"] {
		t.Errorf("authorization_servers と issuer が一致しない: %v / %v", pr["authorization_servers"], as["issuer"])
	}
	if !strings.Contains(strings.Join(toStrings(as["code_challenge_methods_supported"]), ","), "S256") {
		t.Error("S256 が無い")
	}
	if as["client_id_metadata_document_supported"] != true || as["authorization_response_iss_parameter_supported"] != true {
		t.Error("CIMD / iss の対応が広告されていない")
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// ChatGPT（信頼済み）: 同意画面なしでコード発行 → トークン → 検証 → リフレッシュのローテーション。
func TestChatGPTFlowWithoutConsent(t *testing.T) {
	f := newFixture(t)
	verifier, challenge := pkcePair()
	loc := f.authorize("e000001", authorizeURL(chatgptClient, chatgptRedir, challenge, nil))
	if got := loc.Scheme + "://" + loc.Host + loc.Path; got != chatgptRedir {
		t.Fatalf("redirect = %s", loc)
	}
	q := loc.Query()
	if q.Get("state") != "xyz" || q.Get("iss") != testBase || q.Get("code") == "" {
		t.Fatalf("認可応答が不正: %s", loc)
	}

	code, tok := f.token(url.Values{
		"grant_type": {"authorization_code"}, "code": {q.Get("code")}, "redirect_uri": {chatgptRedir},
		"client_id": {chatgptClient}, "code_verifier": {verifier}, "resource": {testBase + "/mcp"},
	})
	if code != http.StatusOK {
		t.Fatalf("token: %d %v", code, tok)
	}
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)
	if user, err := f.verify(access); err != nil || user != "e000001" {
		t.Fatalf("アクセストークンの検証: %q %v", user, err)
	}
	if f.grantCount("e000001") != 1 {
		t.Error("連携が 1 件になっていない")
	}

	// リフレッシュすると新しいペアが出て、古いリフレッシュトークンは使えない
	code, tok2 := f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {chatgptClient}})
	if code != http.StatusOK || tok2["refresh_token"] == refresh {
		t.Fatalf("refresh: %d %v", code, tok2)
	}
	if _, err := f.verify(tok2["access_token"].(string)); err != nil {
		t.Fatal("新しいアクセストークンが使えない")
	}

	// 使用済みのリフレッシュトークンの再利用 → 漏えいとみなし連携ごと失効
	code, _ = f.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {chatgptClient}})
	if code != http.StatusBadRequest {
		t.Fatalf("再利用が通ってしまう: %d", code)
	}
	if _, err := f.verify(tok2["access_token"].(string)); err == nil {
		t.Error("再利用検知後もアクセストークンが使える")
	}
	if f.grantCount("e000001") != 0 {
		t.Error("再利用検知後も連携が残っている")
	}
}

func TestNotLoggedInRedirectsToLogin(t *testing.T) {
	f := newFixture(t)
	_, challenge := pkcePair()
	loc := f.authorize("", authorizeURL(chatgptClient, chatgptRedir, challenge, nil))
	if loc.Path != "/login" {
		t.Fatalf("redirect = %s", loc)
	}
	next := loc.Query().Get("next")
	if !strings.HasPrefix(next, "/oauth/authorize?") || !strings.Contains(next, "code_challenge="+challenge) {
		t.Errorf("next = %q", next)
	}
}

func TestInvalidClientOrRedirectNeverRedirects(t *testing.T) {
	f := newFixture(t)
	_, challenge := pkcePair()
	for name, target := range map[string]string{
		"未知のクライアント":     authorizeURL("https://evil.example/client.json", "https://evil.example/cb", challenge, nil),
		"未登録の redirect": authorizeURL(chatgptClient, "https://evil.example/cb", challenge, nil),
		"client_id なし":  authorizeURL("", chatgptRedir, challenge, nil),
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.AddCookie(&http.Cookie{Name: "login", Value: "e000001"})
		rec := f.do(req)
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
			t.Errorf("%s: code=%d location=%q", name, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestRequestErrorsRedirectToClient(t *testing.T) {
	f := newFixture(t)
	_, challenge := pkcePair()
	cases := map[string]url.Values{
		"PKCE なし":       {"code_challenge": {""}},
		"plain は不可":     {"code_challenge_method": {"plain"}},
		"別のリソース":        {"resource": {"https://other.example/mcp"}},
		"response_type": {"response_type": {"token"}},
	}
	for name, extra := range cases {
		loc := f.authorize("e000001", authorizeURL(chatgptClient, chatgptRedir, challenge, extra))
		if loc.Query().Get("error") == "" || loc.Query().Get("code") != "" || loc.Query().Get("state") != "xyz" {
			t.Errorf("%s: %s", name, loc)
		}
	}
}

func TestTokenRejections(t *testing.T) {
	f := newFixture(t)
	verifier, challenge := pkcePair()
	newCode := func() string {
		return f.authorize("e000001", authorizeURL(chatgptClient, chatgptRedir, challenge, nil)).Query().Get("code")
	}
	base := func(code string) url.Values {
		return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {chatgptRedir},
			"client_id": {chatgptClient}, "code_verifier": {verifier}}
	}

	bad := base(newCode())
	bad.Set("code_verifier", strings.Repeat("a", 43))
	if c, out := f.token(bad); c != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Errorf("PKCE 不一致: %d %v", c, out)
	}

	wrongClient := base(newCode())
	wrongClient.Set("client_id", "https://evil.example/client.json")
	if c, _ := f.token(wrongClient); c != http.StatusBadRequest {
		t.Errorf("別クライアント: %d", c)
	}

	wrongResource := base(newCode())
	wrongResource.Set("resource", "https://other.example/mcp")
	if c, out := f.token(wrongResource); c != http.StatusBadRequest || out["error"] != "invalid_target" {
		t.Errorf("別リソース: %d %v", c, out)
	}

	expired := newCode()
	f.now = f.now.Add(2 * time.Minute)
	if c, _ := f.token(base(expired)); c != http.StatusBadRequest {
		t.Errorf("期限切れコード: %d", c)
	}
	f.now = f.now.Add(-2 * time.Minute)

	// コードの 2 回目の使用 → 1 回目で出たトークンも失効
	code := newCode()
	c, tok := f.token(base(code))
	if c != http.StatusOK {
		t.Fatalf("1 回目: %d %v", c, tok)
	}
	if c, _ := f.token(base(code)); c != http.StatusBadRequest {
		t.Errorf("コード再利用が通る: %d", c)
	}
	if _, err := f.verify(tok["access_token"].(string)); err == nil {
		t.Error("コード再利用後も 1 回目のトークンが使える")
	}

	if c, out := f.token(url.Values{"grant_type": {"password"}}); c != http.StatusBadRequest || out["error"] != "unsupported_grant_type" {
		t.Errorf("未対応 grant: %d %v", c, out)
	}
}

func TestAccessTokenExpiryAndAudience(t *testing.T) {
	f := newFixture(t)
	verifier, challenge := pkcePair()
	code := f.authorize("e000001", authorizeURL(chatgptClient, chatgptRedir, challenge, nil)).Query().Get("code")
	_, tok := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {chatgptClient},
		"redirect_uri": {chatgptRedir}, "code_verifier": {verifier}})
	access := tok["access_token"].(string)

	other := &Server{Store: f.db, PublicBaseURL: "https://other.example", Now: f.srv.Now}
	if _, err := other.VerifyAccessToken(httptest.NewRequest(http.MethodPost, "/mcp", nil), access); err == nil {
		t.Error("別のサーバー向けのトークンとして通ってしまう（audience 未検証）")
	}
	f.now = f.now.Add(accessTTL + time.Second)
	if _, err := f.verify(access); err == nil {
		t.Error("期限切れのアクセストークンが使える")
	}
	if _, err := f.verify("dha_nonexistent"); err == nil {
		t.Error("存在しないトークンが通る")
	}
}

// 信頼済みでないクライアント（動的登録）は同意画面を出し、許可・拒否・CSRF を正しく扱う。
func TestDCRClientRequiresConsent(t *testing.T) {
	f := newFixture(t)
	reg := httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(`{"client_name":"Some MCP Client","redirect_uris":["https://client.example/cb"]}`))
	rec := f.do(reg)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var regOut map[string]any
	json.Unmarshal(rec.Body.Bytes(), &regOut)
	clientID := regOut["client_id"].(string)
	if regOut["token_endpoint_auth_method"] != "none" {
		t.Errorf("auth method = %v", regOut["token_endpoint_auth_method"])
	}

	_, challenge := pkcePair()
	target := authorizeURL(clientID, "https://client.example/cb", challenge, nil)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: "login", Value: "e000001"})
	rec = f.do(req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Some MCP Client") {
		t.Fatalf("同意画面が出ない: %d", rec.Code)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("同意画面に X-Frame-Options: DENY が無い")
	}
	csrf := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(rec.Body.String())
	if csrf == nil {
		t.Fatal("CSRF トークンが無い")
	}
	form, _ := url.ParseQuery(strings.TrimPrefix(target, "/oauth/authorize?"))

	post := func(decision, token string) *httptest.ResponseRecorder {
		v := url.Values{}
		for k, vs := range form {
			v[k] = vs
		}
		v.Set("decision", decision)
		v.Set("csrf", token)
		r := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(&http.Cookie{Name: "login", Value: "e000001"})
		return f.do(r)
	}
	if rec := post("allow", "deadbeef"); rec.Code != http.StatusForbidden {
		t.Errorf("CSRF 不一致で %d", rec.Code)
	}
	if rec := post("deny", csrf[1]); !strings.Contains(rec.Header().Get("Location"), "error=access_denied") {
		t.Errorf("拒否: %s", rec.Header().Get("Location"))
	}
	rec = post("allow", csrf[1])
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || loc.Query().Get("code") == "" {
		t.Errorf("許可: %d %s", rec.Code, loc)
	}
}

func TestRegisterRejectsUnsafeRedirects(t *testing.T) {
	f := newFixture(t)
	for _, body := range []string{
		`{"redirect_uris":["http://client.example/cb"]}`,
		`{"redirect_uris":[]}`,
		`{"redirect_uris":["https://client.example/cb#frag"]}`,
	} {
		rec := f.do(httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
	rec := f.do(httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{"redirect_uris":["http://localhost:6274/oauth/callback"]}`)))
	if rec.Code != http.StatusCreated {
		t.Errorf("localhost（MCP Inspector 用）が登録できない: %d", rec.Code)
	}
}

// client_secret_* を希望する DCR クライアントも拒否せず、none として登録して返す。
func TestRegisterDowngradesClientSecretToNone(t *testing.T) {
	f := newFixture(t)
	rec := f.do(httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(`{"redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"client_secret_post"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["token_endpoint_auth_method"] != "none" {
		t.Errorf("応答の token_endpoint_auth_method = %v", out["token_endpoint_auth_method"])
	}
	if _, has := out["client_secret"]; has {
		t.Error("シークレットを発行してはいけない")
	}
}

func TestRevoke(t *testing.T) {
	f := newFixture(t)
	verifier, challenge := pkcePair()
	code := f.authorize("e000001", authorizeURL(chatgptClient, chatgptRedir, challenge, nil)).Query().Get("code")
	_, tok := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {chatgptClient},
		"redirect_uri": {chatgptRedir}, "code_verifier": {verifier}})
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(url.Values{"token": {tok["refresh_token"].(string)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := f.do(req); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if _, err := f.verify(tok["access_token"].(string)); err == nil {
		t.Error("リフレッシュトークン失効後もアクセストークンが使える")
	}
}

// 2 回目以降（連携済み）は信頼済みでないクライアントでも同意画面を出さない。
func TestExistingGrantSkipsConsent(t *testing.T) {
	f := newFixture(t)
	if err := f.db.UpsertOAuthClient(db.OAuthClient{ClientID: "dcr_x", Name: "X", RedirectURIs: []string{"https://client.example/cb"}, Kind: "dcr", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.CreateOAuthGrant("g_1", "e000001", "dcr_x", Scope); err != nil {
		t.Fatal(err)
	}
	_, challenge := pkcePair()
	loc := f.authorize("e000001", authorizeURL("dcr_x", "https://client.example/cb", challenge, nil))
	if loc.Query().Get("code") == "" {
		t.Errorf("連携済みなのにコードが出ない: %s", loc)
	}
}

func TestParseClientMetadata(t *testing.T) {
	c, err := parseClientMetadata(chatgptClient, []byte(chatgptMetadata))
	if err != nil || c.Name != "ChatGPT" || c.RedirectURIs[0] != chatgptRedir {
		t.Fatalf("ChatGPT のクライアント情報を読めない: %+v %v", c, err)
	}
	bad := map[string]string{
		"client_id 不一致": `{"client_id":"https://other/client.json","redirect_uris":["https://a/cb"]}`,
		"http redirect": `{"client_id":"` + chatgptClient + `","redirect_uris":["http://a/cb"]}`,
		"none 非対応":      `{"client_id":"` + chatgptClient + `","redirect_uris":["https://a/cb"],"token_endpoint_auth_methods_supported":["private_key_jwt"]}`,
		"redirect なし":   `{"client_id":"` + chatgptClient + `"}`,
	}
	for name, body := range bad {
		if _, err := parseClientMetadata(chatgptClient, []byte(body)); err == nil {
			t.Errorf("%s が通ってしまう", name)
		}
	}
}

func TestClientIDURLAndSSRFGuards(t *testing.T) {
	for _, u := range []string{"http://chatgpt.com/oauth/client.json", "https://chatgpt.com", "https://chatgpt.com/", "https://user@chatgpt.com/x"} {
		if validateClientIDURL(u) == nil {
			t.Errorf("%s が通ってしまう", u)
		}
	}
	if validateClientIDURL(chatgptClient) != nil {
		t.Error("ChatGPT の client_id が拒否される")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "::1", "0.0.0.0", "fc00::1"} {
		if isPublicIP(net.ParseIP(ip)) {
			t.Errorf("%s を公開 IP とみなしている", ip)
		}
	}
	if !isPublicIP(net.ParseIP("104.18.32.47")) {
		t.Error("公開 IP を拒否している")
	}
	// 実際にループバックへの接続が拒否されることを確認する
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") }))
	defer ts.Close()
	if _, err := cimdHTTPClient.Get(ts.URL); err == nil {
		t.Error("ループバックへの接続が許されている")
	}
}
