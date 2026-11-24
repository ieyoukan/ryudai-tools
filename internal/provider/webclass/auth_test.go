package webclass

import (
	"net/http"
	"testing"
	"time"
)

func TestMergeCookieAttributesPreservesExpiryFromLoginResponse(t *testing.T) {
	expiresAt := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	cookies := mergeCookieAttributes(
		[]*http.Cookie{{Name: "WCAC", Value: "Authenticated", Path: "/webclass/"}},
		[]*http.Cookie{{Name: "WCAC", Value: "Authenticated", Expires: expiresAt, Secure: true, HttpOnly: true}},
	)
	if !cookies[0].Expires.Equal(expiresAt) {
		t.Fatalf("Expires = %v, want %v", cookies[0].Expires, expiresAt)
	}
	if !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("security attributes from the login response were not retained")
	}
}
