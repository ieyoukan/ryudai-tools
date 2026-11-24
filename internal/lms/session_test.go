package lms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

func envPassword(user string) func(string) (string, bool) {
	return func(u string) (string, bool) { return "password", strings.EqualFold(u, user) }
}

func TestRenewIfExpiringReplacesCookieNearExpiry(t *testing.T) {
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
		}
	}))
	defer server.Close()

	database, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := database.NewSessionStore()
	id := store.Create("student", []*http.Cookie{{
		Name: "WCAC", Value: "Authenticated", Path: "/", Expires: time.Now().Add(session.WebClassRenewBefore / 2),
	}})
	entry, _ := store.Get(id)
	s := New(server.URL, store, envPassword("student"))

	s.RenewIfExpiring(context.Background(), entry)
	if loginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls)
	}
	expiresAt, ok := session.WebClassCookieExpiresAt(entry.Cookies)
	if !ok || time.Until(expiresAt) < 6*24*time.Hour {
		t.Fatalf("renewed WCAC expiry = %v, present = %t", expiresAt, ok)
	}
	persisted, _ := store.Get(id)
	if got, ok := session.WebClassCookieExpiresAt(persisted.Cookies); !ok || !got.Equal(expiresAt) {
		t.Fatalf("persisted WCAC expiry = %v, want %v", got, expiresAt)
	}
}

func TestRenewIfExpiringOnlyForEnvAccount(t *testing.T) {
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
	s := New(server.URL, store, envPassword("student"))

	s.RenewIfExpiring(context.Background(), entry)
	if loginCalls != 0 {
		t.Fatalf("logged in for a user without a password: %d calls", loginCalls)
	}
	if err := s.relogin(context.Background(), entry); !errors.Is(err, ErrNoPassword) {
		t.Fatalf("relogin err = %v, want ErrNoPassword", err)
	}
}

// WebClass 側でセッションが切れていたら、.env の本人は 1 回だけ再ログインして読み取りをやり直す。
// 同時に来た 2 つの読み取りでも再ログインは 1 回で、新しい Cookie を保存する。ログアウト後は回復しない。
func TestRecoversRejectedSessionOnce(t *testing.T) {
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
			s := New(server.URL, store, envPassword("student"))

			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					entry, _ := store.Get(id) // どちらも切れた Cookie から始める
					courses, err := s.Courses(context.Background(), entry)
					if err != nil || len(courses) != 1 {
						t.Errorf("courses=%v err=%v", courses, err)
					}
				}()
			}
			wg.Wait()
			if loginCalls != 1 {
				t.Fatalf("login calls=%d want 1", loginCalls)
			}
			saved, _ := store.Get(id)
			if saved.Cookies[1].Value != "fresh" {
				t.Fatal("recovered cookies were not persisted")
			}
			stale := *saved
			stale.Cookies = []*http.Cookie{{Name: "WBT_Session", Value: "old", Path: "/"}}
			store.Delete(id)
			if _, err := s.Courses(context.Background(), &stale); err == nil {
				t.Fatal("logged-out session recovered")
			}
		})
	}
}

// 順番待ちの間に利用者がページを離れたら（context の取り消し）、待たずに戻る。
func TestOpenGivesUpWhenRequestIsCancelled(t *testing.T) {
	store := session.NewMemStore()
	id := store.Create("student", nil)
	entry, _ := store.Get(id)
	s := New("http://127.0.0.1:9", store, nil)

	_, release, err := s.Open(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, release2, err := s.Open(ctx, entry); !errors.Is(err, context.DeadlineExceeded) {
		release2()
		t.Fatalf("second Open err = %v, want deadline exceeded while the first holds the session", err)
	}
}

func TestDescribe(t *testing.T) {
	for err, want := range map[error]string{
		context.DeadlineExceeded:           "WebClass の応答がありません",
		fmt.Errorf("x: %w", ErrNoPassword): "ログインが切れました",
		errors.New("boom"):                 "の取得に失敗しました: boom",
	} {
		if got := Describe("資料の一覧", err); !strings.Contains(got, want) {
			t.Errorf("Describe(%v) = %q, want to contain %q", err, got, want)
		}
	}
}
