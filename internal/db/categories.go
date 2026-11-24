package db

import (
	"strings"
	"time"
)

// LectureCategory は今期の登録科目の科目区分。履修登録の期間が終わると教務システムの画面に区分が出なくなるため、
// 期間中に見えた区分（source = kyomu）や、利用者が選んだ区分（source = user）を保存しておく。
type LectureCategory struct {
	LectureCode string `json:"lecture_code"`
	Category    string `json:"category"`
	Source      string `json:"source"` // kyomu / user
}

// LectureCategories はユーザーが保存した科目区分を講義コードで引けるように返す。
func (db *DB) LectureCategories(userID string) (map[string]LectureCategory, error) {
	rows, err := db.sql.Query(`SELECT lecture_code, category, source FROM lecture_categories WHERE user_id = ?`, strings.ToLower(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]LectureCategory{}
	for rows.Next() {
		var c LectureCategory
		if err := rows.Scan(&c.LectureCode, &c.Category, &c.Source); err != nil {
			return nil, err
		}
		out[c.LectureCode] = c
	}
	return out, rows.Err()
}

// SaveLectureCategory は科目区分を保存する。利用者が選んだ区分は、教務システムから読んだ区分で上書きしない。
func (db *DB) SaveLectureCategory(userID string, c LectureCategory) error {
	_, err := db.sql.Exec(`INSERT INTO lecture_categories (user_id, lecture_code, category, source, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, lecture_code) DO UPDATE SET category = excluded.category, source = excluded.source, updated_at = excluded.updated_at
		WHERE excluded.source = 'user' OR lecture_categories.source != 'user'`,
		strings.ToLower(userID), c.LectureCode, c.Category, c.Source, time.Now().Unix())
	return err
}

// DeleteLectureCategories はユーザーが保存した科目区分をすべて削除する（履修計画の全削除と一緒に呼ぶ）。
func (db *DB) DeleteLectureCategories(userID string) error {
	_, err := db.sql.Exec(`DELETE FROM lecture_categories WHERE user_id = ?`, strings.ToLower(userID))
	return err
}
