package session

import (
	"net/http"
	"testing"
	"time"
)

func TestMemStoreGetUsesIdleTimeoutAndRefreshesLastSeen(t *testing.T) {
	store := NewMemStore().(*memStore)
	id := store.Create("student", []*http.Cookie{{Name: "WCAC", Value: "Authenticated"}})

	store.mu.Lock()
	store.sessions[id].CreatedAt = time.Now().Add(-IdleTimeout - time.Minute)
	store.sessions[id].LastSeenAt = time.Now().Add(-24 * time.Hour)
	previousLastSeen := store.sessions[id].LastSeenAt
	store.mu.Unlock()

	entry, ok := store.Get(id)
	if !ok {
		t.Fatal("active session was rejected based on its creation time")
	}
	if !entry.LastSeenAt.After(previousLastSeen) {
		t.Fatalf("LastSeenAt = %v, want later than %v", entry.LastSeenAt, previousLastSeen)
	}

	store.mu.Lock()
	store.sessions[id].LastSeenAt = time.Now().Add(-IdleTimeout - time.Second)
	store.mu.Unlock()
	if _, ok := store.Get(id); ok {
		t.Fatal("idle-expired session was accepted")
	}
}

func TestMemStoreDeleteRevokesSession(t *testing.T) {
	store := NewMemStore()
	id := store.Create("student", nil)
	store.Delete(id)
	if _, ok := store.Get(id); ok {
		t.Fatal("deleted session was accepted")
	}
}

func TestMemStoreGetReturnsIndependentCookies(t *testing.T) {
	store := NewMemStore()
	id := store.Create("student", []*http.Cookie{{Name: "WCAC", Value: "Authenticated"}})
	entry, _ := store.Get(id)
	entry.Cookies[0].Value = "changed"
	again, _ := store.Get(id)
	if again.Cookies[0].Value != "Authenticated" {
		t.Fatal("request changed stored cookies without UpdateCookies")
	}
}

func TestWebClassCookieExpiresAtUsesLatestWCACExpiry(t *testing.T) {
	earlier := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	later := earlier.Add(6 * 24 * time.Hour)
	expiresAt, ok := WebClassCookieExpiresAt([]*http.Cookie{
		{Name: "WBT_Session"},
		{Name: "WCAC", Expires: earlier},
		{Name: "WCAC", Expires: later},
	})
	if !ok || !expiresAt.Equal(later) {
		t.Fatalf("WebClassCookieExpiresAt = %v, %t; want %v, true", expiresAt, ok, later)
	}
}
