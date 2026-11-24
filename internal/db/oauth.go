package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrOAuthNotFound は OAuth のクライアント・コード・トークンが見つからないことを示す。
var ErrOAuthNotFound = errors.New("OAuth のデータが見つかりません")

// OAuthClient は OAuth クライアント（ChatGPT など）。
// Kind は "cimd"（client_id が URL のメタデータ文書）か "dcr"（動的登録）。
type OAuthClient struct {
	ClientID     string
	Name         string
	RedirectURIs []string
	Kind         string
	CreatedAt    time.Time
}

// OAuthCode は認可コード 1 件（ハッシュで保存）。
type OAuthCode struct {
	ClientID      string
	UserID        string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	Scope         string
	ExpiresAt     time.Time
	UsedAt        time.Time
	GrantID       string
}

// OAuthGrant は「あるユーザーがあるクライアントとの連携を許可した」1 件。
type OAuthGrant struct {
	ID         string
	UserID     string
	ClientID   string
	ClientName string
	Scope      string
	CreatedAt  time.Time
	LastUsedAt time.Time
	RevokedAt  time.Time
}

// OAuthToken はアクセス／リフレッシュトークン 1 件と、その連携の情報。
type OAuthToken struct {
	GrantID   string
	Kind      string // "access" | "refresh"
	Resource  string
	ExpiresAt time.Time
	UsedAt    time.Time
	UserID    string
	ClientID  string
	Scope     string
	Revoked   bool
}

func timeOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// UpsertOAuthClient はクライアントを登録・更新する。
func (db *DB) UpsertOAuthClient(c OAuthClient) error {
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return err
	}
	_, err = db.sql.Exec(
		`INSERT INTO oauth_clients (client_id, client_name, redirect_uris, kind, created_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(client_id) DO UPDATE SET
		   client_name = excluded.client_name,
		   redirect_uris = excluded.redirect_uris,
		   kind = excluded.kind,
		   created_at = excluded.created_at`,
		c.ClientID, c.Name, string(uris), c.Kind, c.CreatedAt.Unix(),
	)
	return err
}

// GetOAuthClient はクライアントを返す。
func (db *DB) GetOAuthClient(clientID string) (*OAuthClient, error) {
	var c OAuthClient
	var uris string
	var created int64
	err := db.sql.QueryRow(
		`SELECT client_id, client_name, redirect_uris, kind, created_at FROM oauth_clients WHERE client_id = ?`, clientID,
	).Scan(&c.ClientID, &c.Name, &uris, &c.Kind, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOAuthNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("redirect_uris の読み取り: %w", err)
	}
	c.CreatedAt = time.Unix(created, 0)
	return &c, nil
}

// SaveOAuthCode は認可コードを保存する。
func (db *DB) SaveOAuthCode(hash string, c OAuthCode) error {
	_, err := db.sql.Exec(
		`INSERT INTO oauth_codes (hash, client_id, user_id, redirect_uri, code_challenge, resource, scope, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hash, c.ClientID, c.UserID, c.RedirectURI, c.CodeChallenge, c.Resource, c.Scope, c.ExpiresAt.Unix(),
	)
	return err
}

// ConsumeOAuthCode は認可コードを使用済みにして返す。
// すでに使われていた場合は reused=true で返す（呼び出し側がその連携を失効させる）。
func (db *DB) ConsumeOAuthCode(hash string) (code *OAuthCode, reused bool, err error) {
	tx, err := db.sql.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var c OAuthCode
	var expires, used int64
	err = tx.QueryRow(
		`SELECT client_id, user_id, redirect_uri, code_challenge, resource, scope, expires_at, used_at, grant_id
		 FROM oauth_codes WHERE hash = ?`, hash,
	).Scan(&c.ClientID, &c.UserID, &c.RedirectURI, &c.CodeChallenge, &c.Resource, &c.Scope, &expires, &used, &c.GrantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrOAuthNotFound
	}
	if err != nil {
		return nil, false, err
	}
	c.ExpiresAt = time.Unix(expires, 0)
	c.UsedAt = timeOrZero(used)
	if used != 0 {
		return &c, true, tx.Commit()
	}
	if _, err := tx.Exec(`UPDATE oauth_codes SET used_at = ? WHERE hash = ?`, time.Now().Unix(), hash); err != nil {
		return nil, false, err
	}
	return &c, false, tx.Commit()
}

// SetOAuthCodeGrant は認可コードから発行した連携を記録する（コード再利用時の失効に使う）。
func (db *DB) SetOAuthCodeGrant(hash, grantID string) error {
	_, err := db.sql.Exec(`UPDATE oauth_codes SET grant_id = ? WHERE hash = ?`, grantID, hash)
	return err
}

// ActiveOAuthGrant は同じユーザー・クライアントの有効な連携があれば返す。
func (db *DB) ActiveOAuthGrant(userID, clientID string) (string, error) {
	var id string
	err := db.sql.QueryRow(
		`SELECT id FROM oauth_grants WHERE user_id = ? AND client_id = ? AND revoked_at = 0 ORDER BY created_at DESC LIMIT 1`,
		userID, clientID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrOAuthNotFound
	}
	return id, err
}

// CreateOAuthGrant は連携を作成する。
func (db *DB) CreateOAuthGrant(id, userID, clientID, scope string) error {
	_, err := db.sql.Exec(
		`INSERT INTO oauth_grants (id, user_id, client_id, scope, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, userID, clientID, scope, time.Now().Unix(),
	)
	return err
}

// SaveOAuthToken はトークンを保存する。
func (db *DB) SaveOAuthToken(hash, grantID, kind, resource string, expiresAt time.Time) error {
	_, err := db.sql.Exec(
		`INSERT INTO oauth_tokens (hash, grant_id, kind, resource, expires_at) VALUES (?, ?, ?, ?, ?)`,
		hash, grantID, kind, resource, expiresAt.Unix(),
	)
	return err
}

// GetOAuthToken はトークンと、その連携の情報を返す。
func (db *DB) GetOAuthToken(hash string) (*OAuthToken, error) {
	var t OAuthToken
	var expires, used, revoked int64
	err := db.sql.QueryRow(
		`SELECT t.grant_id, t.kind, t.resource, t.expires_at, t.used_at, g.user_id, g.client_id, g.scope, g.revoked_at
		 FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id WHERE t.hash = ?`, hash,
	).Scan(&t.GrantID, &t.Kind, &t.Resource, &expires, &used, &t.UserID, &t.ClientID, &t.Scope, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOAuthNotFound
	}
	if err != nil {
		return nil, err
	}
	t.ExpiresAt = time.Unix(expires, 0)
	t.UsedAt = timeOrZero(used)
	t.Revoked = revoked != 0
	return &t, nil
}

// MarkOAuthTokenUsed はリフレッシュトークンを使用済みにする。
// すでに使用済みなら false（再利用 = 漏えいの疑い）。
func (db *DB) MarkOAuthTokenUsed(hash string) (bool, error) {
	res, err := db.sql.Exec(`UPDATE oauth_tokens SET used_at = ? WHERE hash = ? AND used_at = 0`, time.Now().Unix(), hash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// TouchOAuthGrant は連携の最終利用時刻を更新する。
func (db *DB) TouchOAuthGrant(grantID string) {
	db.sql.Exec(`UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, time.Now().Unix(), grantID)
}

// RevokeOAuthGrant は連携を失効させ、そのトークンもすべて削除する。
// userID を渡すと本人の連携に限る（"" なら制限なし。内部の漏えい検知用）。
func (db *DB) RevokeOAuthGrant(userID, grantID string) error {
	q := `UPDATE oauth_grants SET revoked_at = ? WHERE id = ? AND revoked_at = 0`
	args := []any{time.Now().Unix(), grantID}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	res, err := db.sql.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 && userID != "" {
		return ErrOAuthNotFound
	}
	_, err = db.sql.Exec(`DELETE FROM oauth_tokens WHERE grant_id = ?`, grantID)
	return err
}

// DeleteOAuthToken はトークン 1 件を削除する。
func (db *DB) DeleteOAuthToken(hash string) error {
	_, err := db.sql.Exec(`DELETE FROM oauth_tokens WHERE hash = ?`, hash)
	return err
}

// ListOAuthGrants はユーザーの有効な連携を新しい順に返す。
func (db *DB) ListOAuthGrants(userID string) ([]OAuthGrant, error) {
	rows, err := db.sql.Query(
		`SELECT g.id, g.user_id, g.client_id, COALESCE(c.client_name, g.client_id), g.scope, g.created_at, g.last_used_at
		 FROM oauth_grants g LEFT JOIN oauth_clients c ON c.client_id = g.client_id
		 WHERE g.user_id = ? AND g.revoked_at = 0 ORDER BY g.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OAuthGrant
	for rows.Next() {
		var g OAuthGrant
		var created, used int64
		if err := rows.Scan(&g.ID, &g.UserID, &g.ClientID, &g.ClientName, &g.Scope, &created, &used); err != nil {
			return nil, err
		}
		g.CreatedAt = time.Unix(created, 0)
		g.LastUsedAt = timeOrZero(used)
		out = append(out, g)
	}
	return out, rows.Err()
}

// PurgeExpiredOAuth は期限切れのコード・トークンを削除する。
func (db *DB) PurgeExpiredOAuth() error {
	now := time.Now().Unix()
	if _, err := db.sql.Exec(`DELETE FROM oauth_codes WHERE expires_at < ?`, now-3600); err != nil {
		return err
	}
	_, err := db.sql.Exec(`DELETE FROM oauth_tokens WHERE expires_at < ?`, now)
	return err
}
