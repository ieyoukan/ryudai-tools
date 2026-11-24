package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/ieyoukan/daigaku-hack/internal/db"
)

const maxClientMetadata = 64 << 10

// cimdHTTPClient は CIMD 取得専用の HTTP クライアント。
// 接続先がプライベート IP・ループバックなどなら接続自体を拒否する（SSRF 対策）。
var cimdHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("リダイレクトには従いません")
	},
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil || !isPublicIP(ip) {
					return fmt.Errorf("公開されていないアドレス %s には接続しません", host)
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	},
}

func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast())
}

// validateClientIDURL は CIMD の client_id として受け付ける URL かを検証する。
func validateClientIDURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Host == "" || u.Fragment != "" || u.User != nil || u.Path == "" || u.Path == "/" {
		return errors.New("client_id は https のパス付き URL である必要があります")
	}
	return nil
}

type clientMetadata struct {
	ClientID                 string   `json:"client_id"`
	ClientName               string   `json:"client_name"`
	RedirectURIs             []string `json:"redirect_uris"`
	TokenEndpointAuthMethod  string   `json:"token_endpoint_auth_method"`
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported"`
}

// fetchClientMetadata は CIMD（Client ID Metadata Document）を取得して検証する。
func fetchClientMetadata(ctx context.Context, clientID string) (*db.OAuthClient, error) {
	if err := validateClientIDURL(clientID); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := cimdHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("クライアント情報の取得に失敗: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxClientMetadata+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxClientMetadata {
		return nil, errors.New("クライアント情報が大きすぎます")
	}
	return parseClientMetadata(clientID, body)
}

func parseClientMetadata(clientID string, body []byte) (*db.OAuthClient, error) {
	var m clientMetadata
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("クライアント情報が JSON ではありません: %w", err)
	}
	if m.ClientID != clientID {
		return nil, errors.New("クライアント情報の client_id が URL と一致しません")
	}
	if len(m.RedirectURIs) == 0 {
		return nil, errors.New("redirect_uris がありません")
	}
	for _, u := range m.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			return nil, err
		}
	}
	// こちらは PKCE だけの公開クライアント（none）として扱う。none に対応していないクライアントは受け付けない。
	methods := m.TokenEndpointAuthMethods
	if len(methods) == 0 && m.TokenEndpointAuthMethod != "" {
		methods = []string{m.TokenEndpointAuthMethod}
	}
	if len(methods) > 0 && !containsExact(methods, "none") {
		return nil, errors.New("このクライアントは公開クライアント（none）に対応していません")
	}
	return &db.OAuthClient{
		ClientID:     clientID,
		Name:         truncate(m.ClientName, 100),
		RedirectURIs: m.RedirectURIs,
		Kind:         "cimd",
	}, nil
}

// validateRedirectURI は redirect_uri として安全かを検証する（https、または開発用の localhost）。
func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" {
		return fmt.Errorf("redirect_uri が不正です: %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if h := u.Hostname(); h == "localhost" || h == "127.0.0.1" || h == "::1" {
			return nil
		}
	}
	return fmt.Errorf("redirect_uri は https である必要があります: %q", raw)
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

// handleRegister は動的クライアント登録（RFC 7591）。公開クライアント（none）として登録する。
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var m clientMetadata
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxClientMetadata)).Decode(&m); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "JSON を読み取れません")
		return
	}
	if len(m.RedirectURIs) == 0 || len(m.RedirectURIs) > 10 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris を 1〜10 件指定してください")
		return
	}
	for _, u := range m.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	// クライアントが client_secret_* などを希望しても拒否せず、応答で token_endpoint_auth_method=none を返す
	// （RFC 7591 §3.2.1: サーバーは登録値を変更して返してよい）。PKCE を必須にしているので、
	// シークレット無しの公開クライアントとして扱っても安全性は変わらない。拒否すると、
	// DCR で来るクライアント（ChatGPT の旧方式など）が連携できなくなる。
	c := db.OAuthClient{
		ClientID:     randomToken("dcr_"),
		Name:         truncate(m.ClientName, 100),
		RedirectURIs: m.RedirectURIs,
		Kind:         "dcr",
		CreatedAt:    s.now(),
	}
	if err := s.Store.UpsertOAuthClient(c); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "クライアントを登録できませんでした")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ClientID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}
