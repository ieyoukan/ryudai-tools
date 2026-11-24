package session

import (
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// IdleTimeout は最後の利用からセッションを保持する時間。
// 利用が続いている間は Get により期限が延長される。
const IdleTimeout = 7 * 24 * time.Hour

// WebClassRenewBefore は WebClass のログイン Cookie を再発行するまでの残り時間。
const WebClassRenewBefore = 24 * time.Hour

// Entry は1ユーザーの WebClass セッションデータ。
type Entry struct {
	SessionID  string
	Username   string
	Cookies    []*http.Cookie
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Store はセッション管理のインターフェース。
// 実装は MemStore（インメモリ）と db.SessionStore（SQLite）がある。
type Store interface {
	Create(username string, cookies []*http.Cookie) string
	Get(id string) (*Entry, bool)
	UpdateCookies(id string, cookies []*http.Cookie) bool
	Delete(id string)
}

// WebClassCookieExpiresAt は認証 Cookie (WCAC) の期限を返す。
// WebClass が期限を明示しない Cookie を返した場合は false を返す。
func WebClassCookieExpiresAt(cookies []*http.Cookie) (time.Time, bool) {
	var latest time.Time
	for _, cookie := range cookies {
		if cookie.Name == "WCAC" && cookie.Expires.After(latest) {
			latest = cookie.Expires
		}
	}
	return latest, !latest.IsZero()
}

// NewMemStore はインメモリセッションストアを返す。DB 未設定時のフォールバック。
func NewMemStore() Store {
	s := &memStore{sessions: make(map[string]*Entry)}
	go s.cleanup()
	return s
}

type memStore struct {
	mu       sync.RWMutex
	sessions map[string]*Entry
}

func (s *memStore) Create(username string, cookies []*http.Cookie) string {
	id := uuid.New().String()
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = &Entry{
		SessionID:  id,
		Username:   username,
		Cookies:    cookies,
		CreatedAt:  now,
		LastSeenAt: now,
	}
	return id
}

func (s *memStore) Get(id string) (*Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Since(e.LastSeenAt) > IdleTimeout {
		delete(s.sessions, id)
		return nil, false
	}
	e.LastSeenAt = time.Now()
	copy := *e
	copy.Cookies = cloneCookies(e.Cookies)
	return &copy, ok
}

func cloneCookies(cookies []*http.Cookie) []*http.Cookie {
	result := make([]*http.Cookie, len(cookies))
	for i, cookie := range cookies {
		copy := *cookie
		result[i] = &copy
	}
	return result
}

func (s *memStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *memStore) UpdateCookies(id string, cookies []*http.Cookie) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[id]
	if !ok {
		return false
	}
	entry.Cookies = cookies
	return true
}

func (s *memStore) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for id, e := range s.sessions {
			if time.Since(e.LastSeenAt) > IdleTimeout {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
