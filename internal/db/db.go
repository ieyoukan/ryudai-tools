package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB は SQLite 接続とセッション・設定などの操作を提供する。
type DB struct {
	sql       *sql.DB
	cookieKey []byte // 設定されていれば保存する Cookie を暗号化する（SetCookieKey）
}

// Open は SQLite ファイルを開き、スキーマを初期化して返す。
func Open(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("DB ディレクトリ作成: %w", err)
		}
	}
	// secure_delete: 削除した行（Cookie や旧版の認証情報）をファイル上で 0 で上書きする。
	// busy_timeout: 掃除のゴルーチンなどと書き込みが重なったとき、すぐ失敗せず最大 5 秒待つ。
	// どちらも接続ごとの設定なので DSN で全接続に付ける
	sqldb, err := sql.Open("sqlite", path+"?_pragma=secure_delete(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("SQLite オープン: %w", err)
	}
	// WAL モードで並行書き込み性能を向上
	if _, err := sqldb.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("WAL 設定: %w", err)
	}
	db := &DB{sql: sqldb}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("マイグレーション: %w", err)
	}
	// 旧版で DROP したテーブル（暗号化済みパスワードなど）の残骸が空き領域と WAL に残らないよう、
	// 起動時に作り直して WAL を空にする。DB は小さいので毎回でも数十 ms で済む。
	if _, err := sqldb.Exec("VACUUM"); err != nil {
		return nil, fmt.Errorf("VACUUM: %w", err)
	}
	if _, err := sqldb.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return nil, fmt.Errorf("WAL チェックポイント: %w", err)
	}
	return db, nil
}

func (db *DB) Close() error {
	return db.sql.Close()
}

func (db *DB) migrate() error {
	if err := db.addColumnIfMissing("sessions", "last_seen_at", `ALTER TABLE sessions ADD COLUMN last_seen_at INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if exists, _, err := db.tableHasColumn("sessions", "last_seen_at"); err != nil {
		return err
	} else if exists {
		if _, err := db.sql.Exec(`UPDATE sessions SET last_seen_at = created_at WHERE last_seen_at = 0`); err != nil {
			return err
		}
	}

	_, err := db.sql.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id          TEXT PRIMARY KEY,
			username    TEXT NOT NULL,
			cookies_json TEXT NOT NULL,
			created_at  INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS kyomu_sessions (
			user_id      TEXT PRIMARY KEY,
			cookies_json TEXT NOT NULL,
			updated_at   INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS plan_items (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    TEXT NOT NULL,
			year_term  TEXT NOT NULL,
			title      TEXT NOT NULL,
			section    TEXT NOT NULL,
			category   TEXT NOT NULL,
			credits    REAL NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS plan_items_user ON plan_items (user_id);

		CREATE TABLE IF NOT EXISTS lecture_categories (
			user_id      TEXT NOT NULL,
			lecture_code TEXT NOT NULL,
			category     TEXT NOT NULL,
			source       TEXT NOT NULL,
			updated_at   INTEGER NOT NULL,
			PRIMARY KEY (user_id, lecture_code)
		);

		CREATE TABLE IF NOT EXISTS api_tokens (
			hash         TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL,
			name         TEXT NOT NULL,
			prefix       TEXT NOT NULL,
			created_at   INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS oauth_clients (
			client_id     TEXT PRIMARY KEY,
			client_name   TEXT NOT NULL,
			redirect_uris TEXT NOT NULL,
			kind          TEXT NOT NULL,
			created_at    INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS oauth_codes (
			hash           TEXT PRIMARY KEY,
			client_id      TEXT NOT NULL,
			user_id        TEXT NOT NULL,
			redirect_uri   TEXT NOT NULL,
			code_challenge TEXT NOT NULL,
			resource       TEXT NOT NULL,
			scope          TEXT NOT NULL,
			expires_at     INTEGER NOT NULL,
			used_at        INTEGER NOT NULL DEFAULT 0,
			grant_id       TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS oauth_grants (
			id           TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL,
			client_id    TEXT NOT NULL,
			scope        TEXT NOT NULL,
			created_at   INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL DEFAULT 0,
			revoked_at   INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS oauth_tokens (
			hash       TEXT PRIMARY KEY,
			grant_id   TEXT NOT NULL,
			kind       TEXT NOT NULL,
			resource   TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			used_at    INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS oauth_tokens_grant ON oauth_tokens (grant_id);

		-- 公開情報（シラバス・学年暦・時間割や便覧の公式資料）の取得結果。全員で共有し、個人の情報は入れない。
		CREATE TABLE IF NOT EXISTS public_cache (
			key           TEXT PRIMARY KEY,
			value         BLOB NOT NULL,
			etag          TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			fetched_at    INTEGER NOT NULL
		);

		-- 旧版で使っていたテーブル（保存していた認証情報など）が残っていれば消す。
		DROP TABLE IF EXISTS users;
		DROP TABLE IF EXISTS settings;
		DROP TABLE IF EXISTS notified_messages;
		DROP TABLE IF EXISTS attendance_log;
	`)
	return err
}

func (db *DB) addColumnIfMissing(table, column, alterQuery string) error {
	exists, hasColumn, err := db.tableHasColumn(table, column)
	if err != nil || !exists || hasColumn {
		return err
	}
	_, err = db.sql.Exec(alterQuery)
	return err
}

func (db *DB) tableHasColumn(table, column string) (exists, hasColumn bool, err error) {
	rows, err := db.sql.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, false, err
		}
		exists = true
		if name == column {
			hasColumn = true
		}
	}
	return exists, hasColumn, rows.Err()
}
