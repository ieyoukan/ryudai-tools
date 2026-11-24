package db

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ieyoukan/daigaku-hack/internal/session"
)

// cookieRecord は Cookie の Name/Value/Path のみを保存する（認証に必要な最小限）。
type cookieRecord struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path"`
	Domain   string `json:"domain"`
	Expires  int64  `json:"expires_unix"`
	Secure   bool   `json:"secure"`
	HttpOnly bool   `json:"http_only"`
	SameSite int    `json:"same_site"`
}

// SessionStore は SQLite を使う session.Store 実装。
type SessionStore struct {
	db *DB
}

// NewSessionStore は DB に紐付いたセッションストアを返す。
// 期限切れセッションを定期的に削除するゴルーチンを起動する。
func (db *DB) NewSessionStore() session.Store {
	s := &SessionStore{db: db}
	go s.cleanup()
	return s
}

// cleanup は期限切れセッションを起動時と30分ごとに DB から削除する。
func (s *SessionStore) cleanup() {
	s.db.DeleteExpiredSessions(time.Now())
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.db.DeleteExpiredSessions(time.Now())
	}
}

// DeleteExpiredSessions は最後の利用から IdleTimeout を過ぎたセッションと、
// 有効なセッションが 1 つも残っていないユーザーの教務システム Cookie を削除する。
// ログアウトせずに放置しても、教務システムの Cookie が保存されたまま残らないようにする。
func (db *DB) DeleteExpiredSessions(now time.Time) error {
	cutoff := now.Add(-session.IdleTimeout).Unix()
	if _, err := db.sql.Exec(`DELETE FROM sessions WHERE last_seen_at < ?`, cutoff); err != nil {
		return err
	}
	_, err := db.sql.Exec(`DELETE FROM kyomu_sessions WHERE NOT EXISTS (
		SELECT 1 FROM sessions WHERE lower(sessions.username) = lower(kyomu_sessions.user_id))`)
	return err
}

func (s *SessionStore) Create(username string, cookies []*http.Cookie) string {
	id := uuid.New().String()
	data, err := s.db.encodeCookies(cookies)
	if err != nil {
		return id
	}
	s.db.sql.Exec(
		`INSERT OR REPLACE INTO sessions (id, username, cookies_json, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
		id, username, data, time.Now().Unix(), time.Now().Unix(),
	)
	return id
}

func (s *SessionStore) UpdateCookies(id string, cookies []*http.Cookie) bool {
	data, err := s.db.encodeCookies(cookies)
	if err != nil {
		return false
	}
	result, err := s.db.sql.Exec(`UPDATE sessions SET cookies_json = ? WHERE id = ?`, data, id)
	if err != nil {
		return false
	}
	updated, err := result.RowsAffected()
	return err == nil && updated == 1
}

func (s *SessionStore) Get(id string) (*session.Entry, bool) {
	row := s.db.sql.QueryRow(
		`SELECT username, cookies_json, created_at, last_seen_at FROM sessions WHERE id = ?`, id,
	)
	var username, cookiesJSON string
	var createdAtUnix, lastSeenAtUnix int64
	if err := row.Scan(&username, &cookiesJSON, &createdAtUnix, &lastSeenAtUnix); err != nil {
		return nil, false
	}

	createdAt := time.Unix(createdAtUnix, 0)
	lastSeenAt := time.Unix(lastSeenAtUnix, 0)
	if time.Since(lastSeenAt) > session.IdleTimeout {
		s.Delete(id)
		return nil, false
	}
	now := time.Now()
	if _, err := s.db.sql.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
		return nil, false
	}

	cookies, err := s.db.decodeCookies(cookiesJSON)
	if err != nil {
		return nil, false
	}
	return &session.Entry{
		SessionID:  id,
		Username:   username,
		Cookies:    cookies,
		CreatedAt:  createdAt,
		LastSeenAt: now,
	}, true
}

func cookiesFromRecords(recs []cookieRecord) []*http.Cookie {
	cookies := make([]*http.Cookie, len(recs))
	for i, r := range recs {
		expires := time.Time{}
		if r.Expires != 0 {
			expires = time.Unix(r.Expires, 0)
		}
		cookies[i] = &http.Cookie{
			Name:     r.Name,
			Value:    r.Value,
			Path:     r.Path,
			Domain:   r.Domain,
			Expires:  expires,
			Secure:   r.Secure,
			HttpOnly: r.HttpOnly,
			SameSite: http.SameSite(r.SameSite),
		}
	}
	return cookies
}

func recordsFromCookies(cookies []*http.Cookie) []cookieRecord {
	recs := make([]cookieRecord, 0, len(cookies))
	for _, c := range cookies {
		recs = append(recs, cookieRecord{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  cookieExpiryUnix(c.Expires),
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
			SameSite: int(c.SameSite),
		})
	}
	return recs
}

func cookieExpiryUnix(expires time.Time) int64 {
	if expires.IsZero() {
		return 0
	}
	return expires.Unix()
}

func (s *SessionStore) Delete(id string) {
	s.db.sql.Exec(`DELETE FROM sessions WHERE id = ?`, id)
}
