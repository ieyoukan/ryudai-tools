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

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/db"
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

// 大学のメールアドレスは @ より前を ID としてログインし、それ以外のメールアドレスは WebClass に送らずに止める。
func TestLoginWithEmailAddress(t *testing.T) {
	var posted []string
	webclassServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<input type="hidden" name="token" value="csrf-token">`)
			return
		}
		r.ParseForm()
		posted = append(posted, r.PostForm.Get("username"))
		http.SetCookie(w, &http.Cookie{Name: "WCAC", Value: "Authenticated", Path: "/"})
	}))
	defer webclassServer.Close()
	kyomuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer kyomuServer.Close()
	database, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := database.NewSessionStore()
	h := NewHandler(&config.Config{WebClassBaseURL: webclassServer.URL}, store, database, academic.New(kyomuServer.URL, nil))

	login := func(username string) *httptest.ResponseRecorder {
		values := url.Values{"username": {username}, "password": {"secret-password"}}
		r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.HandleLogin(w, r)
		return w
	}

	w := login("e000001@cs.u-ryukyu.ac.jp")
	if w.Code != http.StatusSeeOther || len(posted) != 1 || posted[0] != "e000001" {
		t.Fatalf("university address: status %d, posted %v, body %s", w.Code, posted, w.Body.String())
	}
	var sid string
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			sid = c.Value
		}
	}
	if entry, ok := store.Get(sid); !ok || entry.Username != "e000001" {
		t.Fatalf("session user = %+v, ok %t", entry, ok)
	}

	w = login("student@gmail.com")
	if len(posted) != 1 || !strings.Contains(w.Body.String(), "メールアドレスではなく") {
		t.Fatalf("other address reached WebClass: posted %v, body %s", posted, w.Body.String())
	}
}
