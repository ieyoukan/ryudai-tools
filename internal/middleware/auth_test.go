package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ieyoukan/ryudai-tools/internal/session"
)

func TestRefreshSessionRenewsActiveCookie(t *testing.T) {
	store := session.NewMemStore()
	id := store.Create("student", nil)
	req := httptest.NewRequest(http.MethodGet, "/courses", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: id})
	res := httptest.NewRecorder()

	RefreshSession(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(res, req)

	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookie || cookie.Value != id {
		t.Fatalf("cookie = %q=%q, want %q=%q", cookie.Name, cookie.Value, sessionCookie, id)
	}
	if cookie.MaxAge != 7*24*60*60 {
		t.Fatalf("MaxAge = %d, want 604800 (seven days)", cookie.MaxAge)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("renewed cookie lost its security attributes")
	}
}

func TestRefreshSessionDoesNotSetCookieForInvalidSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/courses", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "unknown"})
	res := httptest.NewRecorder()

	RefreshSession(session.NewMemStore())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(res, req)
	if cookies := res.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("unexpected Set-Cookie headers: %v", cookies)
	}
}
