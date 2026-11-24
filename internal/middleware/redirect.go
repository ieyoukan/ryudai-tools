package middleware

import (
	"net"
	"net/http"
	"strings"
)

// RedirectLegacyHosts は旧ドメインへのリクエストを publicBaseURL の同じパスへ 308 で転送する。
// 308 なので POST（ログイン・MCP・OAuth のトークン要求）もメソッドとボディを保ったまま転送される。
// publicBaseURL か legacyHosts が空なら何もしない。
func RedirectLegacyHosts(publicBaseURL string, legacyHosts []string) func(http.Handler) http.Handler {
	legacy := make(map[string]bool, len(legacyHosts))
	for _, h := range legacyHosts {
		legacy[strings.ToLower(h)] = true
	}
	base := strings.TrimRight(publicBaseURL, "/")
	return func(next http.Handler) http.Handler {
		if base == "" || len(legacy) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := strings.ToLower(r.Host)
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if legacy[host] {
				http.Redirect(w, r, base+r.URL.RequestURI(), http.StatusPermanentRedirect)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
