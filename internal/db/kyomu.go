package db

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

// SaveKyomuCookies は教務システムのログイン Cookie をユーザーごとに保存する。
// 公開サーバーはパスワードを保存しないので、この Cookie が有効な間だけ教務システムの機能を使える。
func (db *DB) SaveKyomuCookies(userID string, cookies []*http.Cookie) error {
	data, err := db.encodeCookies(cookies)
	if err != nil {
		return err
	}
	_, err = db.sql.Exec(
		`INSERT OR REPLACE INTO kyomu_sessions (user_id, cookies_json, updated_at) VALUES (?, ?, ?)`,
		userID, data, time.Now().Unix(),
	)
	return err
}

// KyomuCookies は保存済みの教務システム Cookie を返す。無ければ nil を返す。
func (db *DB) KyomuCookies(userID string) ([]*http.Cookie, error) {
	var cookiesJSON string
	err := db.sql.QueryRow(`SELECT cookies_json FROM kyomu_sessions WHERE user_id = ?`, userID).Scan(&cookiesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return db.decodeCookies(cookiesJSON)
}

// DeleteKyomuCookies は教務システム Cookie を削除する（セッション切れ・ログアウト時）。
func (db *DB) DeleteKyomuCookies(userID string) error {
	_, err := db.sql.Exec(`DELETE FROM kyomu_sessions WHERE user_id = ?`, userID)
	return err
}
