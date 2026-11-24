package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectLegacyHosts(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RedirectLegacyHosts("https://ryudai-tools.example.com/", []string{"daigaku-hack.example.com"})(ok)

	r := httptest.NewRequest(http.MethodPost, "https://Daigaku-Hack.example.com/mcp?x=1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != "https://ryudai-tools.example.com/mcp?x=1" {
		t.Fatalf("legacy host: code %d, location %q", w.Code, w.Header().Get("Location"))
	}

	for _, host := range []string{"ryudai-tools.example.com", "localhost:8080"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+host+"/login", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s should not be redirected: code %d", host, w.Code)
		}
	}
}
