package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrTokenNotFound はトークンが存在しない（失効済みを含む）ことを示す。
var ErrTokenNotFound = errors.New("トークンが見つかりません")

// APIToken は MCP / API 用の長期トークンのメタデータ（平文は保存しない）。
type APIToken struct {
	ID         string // ハッシュの先頭（削除時の識別子）
	Name       string
	Prefix     string // 表示用に平文の先頭数文字
	CreatedAt  time.Time
	LastUsedAt time.Time
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken は新しいトークンを発行して平文を返す。平文はこの戻り値でしか得られない。
func (db *DB) CreateAPIToken(userID, name string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("トークン生成: %w", err)
	}
	token := "dh_" + base64.RawURLEncoding.EncodeToString(buf)
	_, err := db.sql.Exec(
		`INSERT INTO api_tokens (hash, user_id, name, prefix, created_at) VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), userID, name, token[:9], time.Now().Unix(),
	)
	if err != nil {
		return "", fmt.Errorf("トークン保存: %w", err)
	}
	return token, nil
}

// UserForAPIToken はトークンの持ち主のユーザー ID を返し、最終利用時刻を更新する。
func (db *DB) UserForAPIToken(token string) (string, error) {
	h := hashToken(token)
	var userID string
	err := db.sql.QueryRow(`SELECT user_id FROM api_tokens WHERE hash = ?`, h).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", err
	}
	db.sql.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE hash = ?`, time.Now().Unix(), h)
	return userID, nil
}

// ListAPITokens はユーザーのトークン一覧を新しい順に返す。
func (db *DB) ListAPITokens(userID string) ([]APIToken, error) {
	rows, err := db.sql.Query(
		`SELECT hash, name, prefix, created_at, last_used_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var h string
		var created, used int64
		var t APIToken
		if err := rows.Scan(&h, &t.Name, &t.Prefix, &created, &used); err != nil {
			return nil, err
		}
		t.ID = h[:16]
		t.CreatedAt = time.Unix(created, 0)
		if used > 0 {
			t.LastUsedAt = time.Unix(used, 0)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken はユーザー自身のトークンを失効させる。id は ListAPITokens の ID。
func (db *DB) DeleteAPIToken(userID, id string) error {
	if len(id) != 16 {
		return ErrTokenNotFound
	}
	res, err := db.sql.Exec(`DELETE FROM api_tokens WHERE user_id = ? AND substr(hash, 1, 16) = ?`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTokenNotFound
	}
	return nil
}
