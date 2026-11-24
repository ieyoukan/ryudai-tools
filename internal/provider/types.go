// Package provider は複数の教育システム（WebClass・教務システム等）を
// 統一的に扱うための抽象（インターフェース・共通ドメイン型・レジストリ）を定義する。
//
// 各システム固有の実装は provider/<name> 以下に置き、ここで定義した能力インターフェースの
// うち対応するものだけを実装する。UI/API は能力インターフェースの有無で機能を出し分ける。
package provider

import "net/http"

// Credentials はログイン資格情報。
type Credentials struct {
	Username string
	Password string
}

// Session は認証済みセッション。cookie を保持し、再利用できる。
type Session struct {
	Provider string         // プロバイダ ID
	Username string         // 表示・記録用のユーザー名
	Cookies  []*http.Cookie // 認証 cookie
}

// Course は時間割上の1コマ分のコース。WebClass・教務システム共通。
type Course struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Day    string `json:"day"`    // "月"〜"土"
	Period int    `json:"period"` // 1〜
}

// Syllabus はシラバス（講義概要）情報。教務システムで未ログインでも取得できる。
type Syllabus struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Instructor string `json:"instructor"`
	Faculty    string `json:"faculty"`  // 開講学部・学科
	Term       string `json:"term"`     // 開講時期
	Credits    string `json:"credits"`  // 単位数
	Overview   string `json:"overview"` // 概要
	URL        string `json:"url"`      // 元ページ
}

// Grade は成績1件。教務システムでログイン後に取得できる。
type Grade struct {
	CourseName string  `json:"course_name"`
	Credits    float64 `json:"credits"`
	Score      string  `json:"score"` // 素点や評語（"A" 等）
	GradePoint float64 `json:"grade_point"`
	Year       string  `json:"year"` // 履修年度
	Term       string  `json:"term"` // 学期
}
