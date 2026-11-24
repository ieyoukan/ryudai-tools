package academic

import (
	"testing"

	"github.com/ieyoukan/daigaku-hack/internal/db"
	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
	"github.com/ieyoukan/daigaku-hack/internal/provider/university"
)

func TestPickSuggestions(t *testing.T) {
	gs, grades, reg := outlookFixture()
	gs.Requirements = append(gs.Requirements, kyomu.Requirement{Section: "専門教育", Name: "知能情報関連", Kind: "item", Required: fp(10), Earned: fp(10), Shortfall: fp(0)})
	grades.Grades = []kyomu.Grade{
		{Year: 2025, LectureCode: "990350001", Title: "試験機械学習", Credits: 2, Passed: true, Category: "専門教育科目（知能情報アドバンスト）"},
		{Year: 2025, LectureCode: "990370001", Title: "試験ロボット工学", Credits: 2, Passed: false, Category: "専門教育科目（知能情報アドバンスト）"},
		{Year: 2024, LectureCode: "990310001", Title: "試験データ管理", Credits: 2, Passed: true, Category: "専門教育科目（知能情報関連）"},
	}
	gs.RequiredCourses = &RequiredCheck{Required: []RequiredSubjectStatus{
		{RequiredSubject: university.RequiredSubject{NumberingCode: "知能301", Title: "試験画像処理"}, Status: "not_passed"},
	}}
	buildOutlook(gs, grades, reg, []db.PlanItem{{YearTerm: "2027前期", Title: "試験言語処理", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 2}})

	groups, groupOf := suggestionGroups(gs)
	if len(groups) != 1 || groups[0].Name != "知能情報アドバンスト" || groups[0].Shortfall != 22-10-4-2 {
		t.Fatalf("groups = %+v", groups)
	}
	offered := []kyomu.SyllabusSummary{
		{Year: 2026, LectureCode: "990350002", Title: "試験機械学習 02組"},                         // 修得済み
		{Year: 2026, LectureCode: "990370001", Title: "試験ロボット工学"},                           // 不合格 → 再履修
		{Year: 2026, LectureCode: "990355001", Title: "試験画像処理", NumberingCode: "知能 301"},    // 必修
		{Year: 2026, LectureCode: "990355002", Title: "試験画像処理 02組", NumberingCode: "知能301"}, // 同じ科目の別の組
		{Year: 2026, LectureCode: "990352001", Title: "試験パターン認識"},                           // 試験機械学習に近い
		{Year: 2026, LectureCode: "990312001", Title: "試験ネットワーク"},                           // 知能情報関連（不足なし）
		{Year: 2026, LectureCode: "990360001", Title: "試験言語処理"},                             // 計画済み
		{Year: 2026, LectureCode: "993311001", Title: "試験心理学"},                              // 成績の無い開講部署
		{Year: 2026, LectureCode: "情報-1", Title: "コードが数字でない"},
		{Year: 2026, LectureCode: "990320001", Title: "試験技術演習II"}, // 今期登録済み
	}
	got := pickSuggestions(gs, grades, reg, offered, groupOf)[0]
	titles := map[string]PlanSuggestion{}
	for _, s := range got {
		titles[s.Title] = s
	}
	if len(got) != 3 {
		t.Fatalf("suggestions = %+v", got)
	}
	if s := titles["試験ロボット工学"]; !s.Retake || s.Category != "知能情報アドバンスト" || s.Section != "専門教育" {
		t.Fatalf("retake = %+v", s)
	}
	if s := titles["試験画像処理"]; !s.Required || s.LectureCode != "990355001" || s.year != 2026 {
		t.Fatalf("required = %+v", s)
	}
	if s := titles["試験パターン認識"]; s.Required || s.Retake || s.distance != 2 || s.Basis != "講義コードが近い「試験機械学習」と同じ区分（推定）" {
		t.Fatalf("near = %+v", s)
	}
}

func TestSuggestionGroupsSubtotal(t *testing.T) {
	// （専門計）が足りないとき、構成する区分のうち自身に不足の無いもの（知能情報関連）は小計のグループに入る
	gs := &GraduationStatus{Requirements: []kyomu.Requirement{
		{Section: "専門教育", Name: "知能情報アドバンスト", Kind: "item", Required: fp(10), Earned: fp(6), Shortfall: fp(4)},
		{Section: "専門教育", Name: "知能情報関連", Kind: "item", Earned: fp(4)},
		{Section: "専門教育", Name: "（数情計）", Kind: "subtotal", Required: fp(20), Earned: fp(10), Shortfall: fp(10)},
	}}
	buildOutlook(gs, nil, nil, nil)
	groups, groupOf := suggestionGroups(gs)
	if len(groups) != 2 || groups[0].Name != "知能情報アドバンスト" || groups[1].Name != "（数情計）" {
		t.Fatalf("groups = %+v", groups)
	}
	if groupOf["専門教育\x00知能情報アドバンスト"] != 0 || groupOf["専門教育\x00知能情報関連"] != 1 {
		t.Fatalf("groupOf = %+v", groupOf)
	}
}
