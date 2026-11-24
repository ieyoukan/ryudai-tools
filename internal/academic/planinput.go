package academic

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ieyoukan/daigaku-hack/internal/db"
)

var (
	planTermRe    = regexp.MustCompile(`^\d{4}(前|後)期$`)
	lectureCodeRe = regexp.MustCompile(`^[0-9A-Za-z]{1,20}$`)
)

// ErrInvalidPlanItem は履修計画に追加する科目の入力が正しくないことを示す。
var ErrInvalidPlanItem = errors.New("学期・科目名・区分・単位を確認してください")

// ValidPlanTerm は 2027前期 のような学期の書き方か。
func ValidPlanTerm(term string) bool { return planTermRe.MatchString(term) }

// NormalizePlanItem は履修計画に追加する科目の前後の空白を除き、値が正しいかを確かめる。
func NormalizePlanItem(it db.PlanItem) (db.PlanItem, error) {
	it.YearTerm = strings.TrimSpace(it.YearTerm)
	it.Title = strings.TrimSpace(it.Title)
	it.Section = strings.TrimSpace(it.Section)
	it.Category = strings.TrimSpace(it.Category)
	if !planTermRe.MatchString(it.YearTerm) || it.Title == "" || utf8.RuneCountInString(it.Title) > 100 ||
		it.Category == "" || utf8.RuneCountInString(it.Category) > 60 || utf8.RuneCountInString(it.Section) > 60 ||
		it.Credits <= 0 || it.Credits > 30 {
		return db.PlanItem{}, ErrInvalidPlanItem
	}
	return it, nil
}

// ValidLectureCategory は、利用者が選んだ今期の登録科目の区分（講義コード → 個別成績表の区分名）として保存してよいか。
func ValidLectureCategory(code, category string) bool {
	category = strings.TrimSpace(category)
	return lectureCodeRe.MatchString(code) && category != "" && utf8.RuneCountInString(category) <= 60
}
