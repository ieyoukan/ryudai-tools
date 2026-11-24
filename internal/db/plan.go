package db

import (
	"strings"
	"time"
)

// PlanItem は卒業までの履修計画の 1 科目。Section・Category は個別成績表の区分（大区分・区分名）。
type PlanItem struct {
	ID        int64   `json:"id"`
	YearTerm  string  `json:"year_term"` // 例: 2027前期
	Title     string  `json:"title"`
	Section   string  `json:"section"`
	Category  string  `json:"category"`
	Credits   float64 `json:"credits"`
	CreatedAt int64   `json:"created_at"`
}

// PlanItems はユーザーの履修計画を学期・登録順に返す。
func (db *DB) PlanItems(userID string) ([]PlanItem, error) {
	rows, err := db.sql.Query(`SELECT id, year_term, title, section, category, credits, created_at
		FROM plan_items WHERE lower(user_id) = lower(?) ORDER BY year_term, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []PlanItem
	for rows.Next() {
		var it PlanItem
		if err := rows.Scan(&it.ID, &it.YearTerm, &it.Title, &it.Section, &it.Category, &it.Credits, &it.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// AddPlanItem は履修計画に科目を追加する。
func (db *DB) AddPlanItem(userID string, it PlanItem) error {
	_, err := db.sql.Exec(`INSERT INTO plan_items (user_id, year_term, title, section, category, credits, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		strings.ToLower(userID), it.YearTerm, it.Title, it.Section, it.Category, it.Credits, time.Now().Unix())
	return err
}

// DeletePlanItem はユーザー本人の計画から 1 科目を削除する。
func (db *DB) DeletePlanItem(userID string, id int64) error {
	_, err := db.sql.Exec(`DELETE FROM plan_items WHERE id = ? AND lower(user_id) = lower(?)`, id, userID)
	return err
}

// DeletePlan はユーザーの履修計画をすべて削除する。
func (db *DB) DeletePlan(userID string) error {
	_, err := db.sql.Exec(`DELETE FROM plan_items WHERE lower(user_id) = lower(?)`, userID)
	return err
}
