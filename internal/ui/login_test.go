package ui

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ieyoukan/daigaku-hack/internal/academic"
	"github.com/ieyoukan/daigaku-hack/internal/config"
	"github.com/ieyoukan/daigaku-hack/internal/db"
)

func TestLoginDoesNotKeepPassword(t *testing.T) {
	webclassServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<input type="hidden" name="token" value="csrf-token">`)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "WCAC", Value: "Authenticated", Path: "/"})
	}))
	defer webclassServer.Close()
	// 教務システムは落ちていても WebClass のログインは成功させる。
	kyomuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer kyomuServer.Close()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cfg := &config.Config{WebClassBaseURL: webclassServer.URL}
	h := NewHandler(cfg, database.NewSessionStore(), database, academic.New(kyomuServer.URL, nil))

	values := url.Values{"username": {"student"}, "password": {"secret-password"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.HandleLogin(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, body %s", w.Code, w.Body.String())
	}
	database.Close()
	for _, suffix := range []string{"", "-wal"} {
		raw, err := os.ReadFile(dbPath + suffix)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("secret-password")) {
			t.Fatalf("password was written to %s", dbPath+suffix)
		}
	}
}

func TestPolicyPagesArePublic(t *testing.T) {
	h := NewHandler(&config.Config{}, nil, nil, nil)
	for _, path := range []string{"/terms", "/privacy", "/login"} {
		w := httptest.NewRecorder()
		h.PageAPI().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "redirect") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestPageAPIRejectsPostWithoutHeader(t *testing.T) {
	h := NewHandler(&config.Config{}, nil, nil, nil)
	r := httptest.NewRequest(http.MethodPost, "/logout", nil)
	w := httptest.NewRecorder()
	h.PageAPI().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST without %s = %d", PageHeader, w.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/logout", nil)
	r.Header.Set(PageHeader, "1")
	w = httptest.NewRecorder()
	h.PageAPI().ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"redirect":"/login"`) {
		t.Fatalf("logout = %d %s", w.Code, w.Body.String())
	}
}
