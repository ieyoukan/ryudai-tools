// Package oauth は MCP 用の OAuth 2.1 認可サーバー。
//
// ChatGPT や claude.ai が Ryudai Tools の MCP に「アカウント連携」で接続できるようにする。
// ログインは既存の WebClass 認証（Ryudai Tools のセッション）を使い、このパッケージは
// 認可コード + PKCE(S256) とリフレッシュトークンの発行・検証だけを担う。
//
// 対応している仕様:
//   - RFC 9728 保護リソースメタデータ / RFC 8414 認可サーバーメタデータ
//   - RFC 7636 PKCE（S256 のみ）/ RFC 8707 resource 指標 / RFC 9207 iss パラメータ
//   - Client ID Metadata Document（CIMD。ChatGPT が使う）/ RFC 7591 動的登録（claude.ai 等）
//   - RFC 7009 トークン失効
//
// トークンはランダムな不透明文字列で、DB には SHA-256 ハッシュだけを保存する。
package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/db"
)

// Scope は発行するスコープ（読み取り専用）。
const Scope = "registration:read"

const (
	codeTTL    = 60 * time.Second
	accessTTL  = time.Hour
	refreshTTL = 90 * 24 * time.Hour
	cimdTTL    = 24 * time.Hour

	accessPrefix  = "dha_"
	refreshPrefix = "dhr_"
)

// ErrInvalidToken はアクセストークンが無効・期限切れ・別リソース向けであることを示す。
var ErrInvalidToken = errors.New("アクセストークンが無効です")

// Store は OAuth のデータを保存する（db.DB が実装）。
type Store interface {
	UpsertOAuthClient(c db.OAuthClient) error
	GetOAuthClient(clientID string) (*db.OAuthClient, error)
	SaveOAuthCode(hash string, c db.OAuthCode) error
	ConsumeOAuthCode(hash string) (*db.OAuthCode, bool, error)
	SetOAuthCodeGrant(hash, grantID string) error
	ActiveOAuthGrant(userID, clientID string) (string, error)
	CreateOAuthGrant(id, userID, clientID, scope string) error
	SaveOAuthToken(hash, grantID, kind, resource string, expiresAt time.Time) error
	GetOAuthToken(hash string) (*db.OAuthToken, error)
	MarkOAuthTokenUsed(hash string) (bool, error)
	TouchOAuthGrant(grantID string)
	RevokeOAuthGrant(userID, grantID string) error
	DeleteOAuthToken(hash string) error
}

// Server は認可サーバーの HTTP ハンドラ群。
type Server struct {
	Store Store
	// PublicBaseURL は外から見た URL（例: https://ryudai-tools.example.com）。空ならリクエストから組み立てる。
	PublicBaseURL string
	// CurrentUser はブラウザのログインセッションからユーザー名を返す（未ログインなら ""）。
	CurrentUser func(*http.Request) string
	// Key は同意画面の CSRF トークン用の秘密鍵。
	Key []byte
	// TrustedRedirectHosts に redirect する既知のクライアント（ChatGPT / claude.ai）は同意画面を省略する。
	TrustedRedirectHosts []string
	// FetchClientMetadata は CIMD の取得処理（テストで差し替える）。nil なら既定の安全な取得処理。
	FetchClientMetadata func(ctx context.Context, clientID string) (*db.OAuthClient, error)
	// Now は現在時刻（テスト用）。
	Now func() time.Time
}

// DefaultTrustedRedirectHosts は同意画面を省略してよい redirect 先。
// ここへのリダイレクトはユーザー自身のブラウザの ChatGPT / Claude にしか届かないため、
// 第三者がコードを横取りできない。
var DefaultTrustedRedirectHosts = []string{"chatgpt.com", "claude.ai", "claude.com"}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Routes は OAuth 関連のエンドポイントを登録する。
func (s *Server) Routes(r chi.Router) {
	r.Get("/.well-known/oauth-protected-resource", s.handleProtectedResource)
	r.Get("/.well-known/oauth-protected-resource/*", s.handleProtectedResource)
	r.Get("/.well-known/oauth-authorization-server", s.handleAuthServerMetadata)
	r.Get("/.well-known/oauth-authorization-server/*", s.handleAuthServerMetadata)
	r.Get("/oauth/authorize", s.handleAuthorize)
	r.Post("/oauth/authorize", s.handleAuthorize)
	r.Post("/oauth/token", s.handleToken)
	r.Post("/oauth/register", s.handleRegister)
	r.Post("/oauth/revoke", s.handleRevoke)
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server",
		"/oauth/token", "/oauth/register", "/oauth/revoke"} {
		r.Options(p, handlePreflight)
	}
}

// BaseURL は issuer として使う URL を返す。
func (s *Server) BaseURL(r *http.Request) string {
	if s.PublicBaseURL != "" {
		return strings.TrimRight(s.PublicBaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// Resource は MCP サーバーのリソース識別子（トークンの audience）を返す。
func (s *Server) Resource(r *http.Request) string { return s.BaseURL(r) + "/mcp" }

// ResourceMetadataURL は保護リソースメタデータの URL を返す。
func (s *Server) ResourceMetadataURL(r *http.Request) string {
	return s.BaseURL(r) + "/.well-known/oauth-protected-resource"
}

// Challenge は 401 応答や未認証ツールで返す WWW-Authenticate の値を返す。
func (s *Server) Challenge(r *http.Request) string {
	return `Bearer resource_metadata="` + s.ResourceMetadataURL(r) + `", scope="` + Scope + `"`
}

// VerifyAccessToken はアクセストークンを検証してユーザー名を返す。
// 期限・失効に加え、このサーバーの MCP 向けに発行されたか（audience）も確認する。
func (s *Server) VerifyAccessToken(r *http.Request, token string) (string, error) {
	if !strings.HasPrefix(token, accessPrefix) {
		return "", ErrInvalidToken
	}
	t, err := s.Store.GetOAuthToken(hashToken(token))
	if err != nil {
		return "", ErrInvalidToken
	}
	if t.Kind != "access" || t.Revoked || !s.now().Before(t.ExpiresAt) || !sameResource(t.Resource, s.Resource(r)) {
		return "", ErrInvalidToken
	}
	s.Store.TouchOAuthGrant(t.GrantID)
	return t.UserID, nil
}

// IsAccessToken はトークンがこのサーバーの発行形式か（API トークンとの判別用）を返す。
func IsAccessToken(token string) bool { return strings.HasPrefix(token, accessPrefix) }

// --- メタデータ ---

func (s *Server) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.Resource(r),
		"authorization_servers":    []string{s.BaseURL(r)},
		"scopes_supported":         []string{Scope},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Ryudai Tools",
	})
}

func (s *Server) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.BaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         base,
		"authorization_endpoint":                         base + "/oauth/authorize",
		"token_endpoint":                                 base + "/oauth/token",
		"registration_endpoint":                          base + "/oauth/register",
		"revocation_endpoint":                            base + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
		"scopes_supported":                               []string{Scope},
	})
}

// --- ユーティリティ ---

func randomToken(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand が失敗する環境では安全にトークンを作れない
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// verifyPKCE は code_verifier が S256 の code_challenge と一致するかを返す。
func verifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return hmac.Equal([]byte(got), []byte(challenge))
}

// sameResource は末尾の / の有無だけ違う resource を同一とみなす。
func sameResource(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// oauthError は RFC 6749 形式のエラー応答を返す。
func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func handlePreflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

// withQuery は u にクエリパラメータを足した URL を返す（既存のクエリは保つ）。
func withQuery(u string, params url.Values) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	for k, vs := range params {
		for _, v := range vs {
			if v != "" {
				q.Set(k, v)
			}
		}
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func logf(format string, args ...any) { log.Printf("oauth: "+format, args...) }
