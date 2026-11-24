package ui

import (
	"errors"
	"fmt"
	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

func TestRenewWebClassCookieReplacesCookieNearExpiry(t *testing.T) {
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			fmt.Fprint(w, `<input type="hidden" name="token" value="csrf-token">`)
		case http.MethodPost:
			loginCalls++
			http.SetCookie(w, &http.Cookie{
				Name: "WCAC", Value: "Authenticated", Path: "/", Expires: time.Now().Add(7 * 24 * time.Hour), MaxAge: 7 * 24 * 60 * 60,
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	database, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	store := database.NewSessionStore()
	id := store.Create("student", []*http.Cookie{{
		Name: "WCAC", Value: "Authenticated", Path: "/", Expires: time.Now().Add(session.WebClassRenewBefore / 2),
	}})
	entry, ok := store.Get(id)
	if !ok {
		t.Fatal("created session was not found")
	}
	h := NewHandler(&config.Config{WebClassBaseURL: server.URL, WebClassUsername: "student", WebClassPassword: "password"}, store, database, academic.New("", nil))

	h.renewWebClassCookie(entry)
	if loginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls)
	}
	expiresAt, ok := session.WebClassCookieExpiresAt(entry.Cookies)
	if !ok || time.Until(expiresAt) < 6*24*time.Hour {
		t.Fatalf("renewed WCAC expiry = %v, present = %t", expiresAt, ok)
	}

	persisted, ok := store.Get(id)
	if !ok {
		t.Fatal("renewed session was not found")
	}
	persistedExpiry, ok := session.WebClassCookieExpiresAt(persisted.Cookies)
	if !ok || !persistedExpiry.Equal(expiresAt) {
		t.Fatalf("persisted WCAC expiry = %v, present = %t; want %v", persistedExpiry, ok, expiresAt)
	}
}

func TestWebClassRecoversRejectedSessionBeforeCookieExpiry(t *testing.T) {
	for _, expiry := range []time.Time{time.Now().Add(6 * 24 * time.Hour), {}} {
		t.Run(fmt.Sprint(expiry.IsZero()), func(t *testing.T) {
			var mu sync.Mutex
			loginCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if strings.HasPrefix(r.URL.Path, "/login.php") {
					if r.Method == http.MethodGet {
						fmt.Fprint(w, `<input name="token" type="hidden" value="csrf">`)
						return
					}
					mu.Lock()
					loginCalls++
					mu.Unlock()
					http.SetCookie(w, &http.Cookie{Name: "WCAC", Value: "Authenticated", Path: "/", Expires: time.Now().Add(7 * 24 * time.Hour)})
					http.SetCookie(w, &http.Cookie{Name: "WBT_Session", Value: "fresh", Path: "/"})
					return
				}
				cookie, _ := r.Cookie("WBT_Session")
				if cookie == nil || cookie.Value != "fresh" {
					fmt.Fprint(w, `<script>window.location.href="/login.php?timeout=1";</script>`)
					return
				}
				fmt.Fprint(w, `<a href="course.php/C01/login">Recovered course</a>`)
			}))
			defer server.Close()
			database, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			store := database.NewSessionStore()
			id := store.Create("student", []*http.Cookie{{Name: "WCAC", Value: "Authenticated", Path: "/", Expires: expiry}, {Name: "WBT_Session", Value: "old", Path: "/"}})
			entry, _ := store.Get(id)
			h := NewHandler(&config.Config{WebClassBaseURL: server.URL, WebClassUsername: "student", WebClassPassword: "password"}, store, database, nil)
			// Both clients are created with rejected cookies before either recovers.
			first, err := h.WebClassClient(entry)
			if err != nil {
				t.Fatal(err)
			}
			second, err := h.WebClassClient(entry)
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for _, client := range []*webclass.Client{first, second} {
				wg.Add(1)
				go func(client *webclass.Client) {
					defer wg.Done()
					courses, err := client.FetchTimetable()
					if err != nil || len(courses) != 1 {
						t.Errorf("courses=%v err=%v", courses, err)
					}
				}(client)
			}
			wg.Wait()
			if loginCalls != 1 {
				t.Fatalf("login calls=%d want 1", loginCalls)
			}
			saved, _ := store.Get(id)
			if saved.Cookies[1].Value != "fresh" {
				t.Fatal("recovered cookies were not persisted")
			}
			store.Delete(id)
			stale, _ := h.WebClassClient(entry)
			if _, err := stale.FetchTimetable(); err == nil {
				t.Fatal("logged-out session recovered")
			}
		})
	}
}

func TestRenewWebClassCookieOnlyForEnvAccount(t *testing.T) {
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			loginCalls++
		}
		fmt.Fprint(w, `<input type="hidden" name="token" value="csrf-token">`)
	}))
	defer server.Close()

	store := session.NewMemStore()
	id := store.Create("someone-else", []*http.Cookie{{
		Name: "WCAC", Value: "Authenticated", Path: "/", Expires: time.Now().Add(session.WebClassRenewBefore / 2),
	}})
	entry, _ := store.Get(id)
	h := NewHandler(&config.Config{WebClassBaseURL: server.URL, WebClassUsername: "student", WebClassPassword: "password"}, store, nil, academic.New("", nil))

	h.renewWebClassCookie(entry)
	if loginCalls != 0 {
		t.Fatalf("logged in for a user without a password: %d calls", loginCalls)
	}
	if err := h.loginWebClass(entry); !errors.Is(err, errNoPassword) {
		t.Fatalf("loginWebClass err = %v, want errNoPassword", err)
	}
}
