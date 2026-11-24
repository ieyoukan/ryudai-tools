package db

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ieyoukan/daigaku-hack/internal/session"
)

func TestSessionStorePersistsSlidingIdleTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := database.NewSessionStore().(*SessionStore)
	cookieExpiry := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Second)
	id := store.Create("student", []*http.Cookie{{
		Name: "WCAC", Value: "Authenticated", Path: "/", Domain: "example.test", Expires: cookieExpiry, Secure: true, HttpOnly: true,
	}})

	// A session older than IdleTimeout remains valid after a day of inactivity.
	recent := time.Now().Add(-24 * time.Hour).Unix()
	if _, err := database.sql.Exec(`UPDATE sessions SET created_at = ?, last_seen_at = ? WHERE id = ?`, time.Now().Add(-session.IdleTimeout-time.Minute).Unix(), recent, id); err != nil {
		t.Fatalf("backdate session: %v", err)
	}
	if _, ok := store.Get(id); !ok {
		t.Fatal("recently used persisted session was rejected")
	}

	if err := database.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	database, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.Close()
	store = database.NewSessionStore().(*SessionStore)
	entry, ok := store.Get(id)
	if !ok {
		t.Fatal("session was not restored after reopening the database")
	}
	if len(entry.Cookies) != 1 || !entry.Cookies[0].Expires.Equal(cookieExpiry) || !entry.Cookies[0].Secure || entry.Cookies[0].Domain != "example.test" {
		t.Fatalf("cookie attributes were not restored: %#v", entry.Cookies)
	}

	if _, err := database.sql.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, time.Now().Add(-session.IdleTimeout-time.Second).Unix(), id); err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if _, ok := store.Get(id); ok {
		t.Fatal("idle-expired persisted session was accepted")
	}
	var count int
	if err := database.sql.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("count session: %v", err)
	}
	if count != 0 {
		t.Fatalf("expired session still stored: count = %d", count)
	}

	id = store.Create("student", nil)
	store.Delete(id)
	if _, ok := store.Get(id); ok {
		t.Fatal("deleted persisted session was accepted")
	}
}

func TestSessionStorePreservesCookieWithoutExpiry(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := database.NewSessionStore()
	id := store.Create("student", []*http.Cookie{{Name: "WBT_Session", Value: "session", Path: "/"}})
	entry, ok := store.Get(id)
	if !ok || len(entry.Cookies) != 1 || !entry.Cookies[0].Expires.IsZero() {
		t.Fatal("session cookie acquired an expiry")
	}
}
