package db

import (
	"database/sql"
	"errors"
	"time"
)

// 公開情報のキャッシュ（public_cache）。シラバスや大学の公式資料など、ログイン不要で誰でも見られる情報だけを
// 入れる。授業資料・成績・履修情報など利用者ごとの情報は入れない（プライバシーポリシー）。

// LoadPublic は key の保存内容を返す。etag と lastModified は条件付きの再取得に使う。
func (db *DB) LoadPublic(key string) (value []byte, etag, lastModified string, fetchedAt time.Time, ok bool) {
	var at int64
	err := db.sql.QueryRow(`SELECT value, etag, last_modified, fetched_at FROM public_cache WHERE key = ?`, key).
		Scan(&value, &etag, &lastModified, &at)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return nil, "", "", time.Time{}, false
	}
	return value, etag, lastModified, time.Unix(at, 0), true
}

// SavePublic は key の内容を保存する（取得日時は今）。
func (db *DB) SavePublic(key string, value []byte, etag, lastModified string) error {
	_, err := db.sql.Exec(`INSERT INTO public_cache (key, value, etag, last_modified, fetched_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, etag = excluded.etag, last_modified = excluded.last_modified, fetched_at = excluded.fetched_at`,
		key, value, etag, lastModified, time.Now().Unix())
	return err
}

// PurgePublic は olderThan より前に取得した公開情報を消す（使われなくなった年度の資料がたまり続けないように）。
func (db *DB) PurgePublic(olderThan time.Duration) error {
	_, err := db.sql.Exec(`DELETE FROM public_cache WHERE fetched_at < ?`, time.Now().Add(-olderThan).Unix())
	return err
}
