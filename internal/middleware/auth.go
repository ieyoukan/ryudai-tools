package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ieyoukan/ryudai-tools/internal/session"
)

const sessionCookie = "ryudai_tools_session"

type contextKey string

const SessionKey contextKey = "session"

// extractSessionID は X-Session-ID ヘッダーまたは Cookie からセッション ID を取得する。
func extractSessionID(r *http.Request) string {
	if id := r.Header.Get("X-Session-ID"); id != "" {
		return id
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// RefreshSession extends the browser cookie for an active cookie-authenticated
// session. This allows a browser restart within IdleTimeout without exposing the
// session ID to JavaScript.
func RefreshSession(store session.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie(sessionCookie); err == nil {
				if _, ok := store.Get(c.Value); ok {
					http.SetCookie(w, &http.Cookie{
						Name:     sessionCookie,
						Value:    c.Value,
						Path:     "/",
						MaxAge:   int(session.IdleTimeout.Seconds()),
						HttpOnly: true,
						SameSite: http.SameSiteLaxMode,
					})
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSession は API ルート用のミドルウェア。
// X-Session-ID ヘッダーまたは Cookie でセッションを検証し、失敗時は 401 JSON を返す。
func RequireSession(store session.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionID := extractSessionID(r)
			if sessionID == "" {
				http.Error(w, `{"error":"missing X-Session-ID header"}`, http.StatusUnauthorized)
				return
			}

			entry, ok := store.Get(sessionID)
			if !ok {
				http.Error(w, `{"error":"invalid or expired session"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), SessionKey, entry)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetSession extracts the session entry from context.
func GetSession(ctx context.Context) *session.Entry {
	entry, _ := ctx.Value(SessionKey).(*session.Entry)
	return entry
}

// TokenResolver は API トークンからユーザー名を引く（db.DB が実装）。
type TokenResolver interface {
	UserForAPIToken(token string) (string, error)
}

// BearerUser は Authorization: Bearer <API トークン> からユーザー名を返す（無効なら ""）。
func BearerUser(r *http.Request, tokens TokenResolver) string {
	h := r.Header.Get("Authorization")
	if tokens == nil || !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	user, err := tokens.UserForAPIToken(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	if err != nil {
		return ""
	}
	return user
}

// RequestUser は API トークン、X-Session-ID ヘッダー、Cookie の順にユーザー名を解決する（未認証なら ""）。
func RequestUser(r *http.Request, store session.Store, tokens TokenResolver) string {
	if u := BearerUser(r, tokens); u != "" {
		return u
	}
	if id := extractSessionID(r); id != "" {
		if entry, ok := store.Get(id); ok {
			return entry.Username
		}
	}
	return ""
}

// SessionUser はログインセッション（X-Session-ID ヘッダーまたは Cookie）だけからユーザー名を返す（無ければ ""）。
// API トークンは読み取り専用なので、書き込みの API はこちらで認証する。
func SessionUser(r *http.Request, store session.Store) string {
	if id := extractSessionID(r); id != "" {
		if entry, ok := store.Get(id); ok {
			return entry.Username
		}
	}
	return ""
}
