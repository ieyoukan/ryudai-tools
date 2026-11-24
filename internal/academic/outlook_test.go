package academic

import (
	"context"
	"strings"
	"testing"

	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
	"github.com/ieyoukan/ryudai-tools/internal/provider/university"
)

func fp(v float64) *float64 { return &v }

// 3年後期（今期 2026後期、残り 3 学期）で、卒業まであと 40 単位・今期 16 単位登録・上限 24 単位の学生。
func outlookFixture() (*GraduationStatus, *kyomu.GradeList, *kyomu.Registration) {
	gs := &GraduationStatus{
		TotalRequired: 130, TotalEarned: 90, TotalShortfall: 40, RemainingTerms: 3,
		Requirements: []kyomu.Requirement{
			{Section: "専門教育", Name: "知能情報アドバンスト", Kind: "item", Required: fp(22), Earned: fp(10), Shortfall: fp(12)},
			{Section: "共通教育", Name: "人文系", Kind: "item", Required: fp(2), Earned: fp(2), Shortfall: fp(0)},
			{Section: "", Name: "合計", Kind: "total", Required: fp(130), Earned: fp(90)},
		},
		ThisTerm: &TermPlan{YearTerm: "2026年度後期", RegisteredCredits: 16, CreditLimit: 24,
			Projection: []ProjectedItem{{Section: "専門教育", Name: "知能情報アドバンスト", RegisteredNow: 4}}},
	}
	gs.Shortfalls = []kyomu.Requirement{gs.Requirements[0]}
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{
		{Year: 2026, Term: "前期", Title: "試験機械学習", Credits: 2, Passed: true},
		{Year: 2026, Term: "前期", Title: "試験ロボット工学", Credits: 2, Passed: false},
	}}
	reg := &kyomu.Registration{YearTerm: "2026年度後期", Lectures: []kyomu.RegisteredLecture{{Title: "試験技術演習II", Credits: 2, Rank: 1}}}
	return gs, grades, reg
}

func TestOutlookPaceOnTrackAndAtRisk(t *testing.T) {
	gs, grades, reg := outlookFixture()
	buildOutlook(gs, grades, reg, nil)
	if gs.Outlook.Status != "on_track" || gs.Outlook.Basis != "pace" || gs.Outlook.ExpectedGraduation != "2028年3月" {
		t.Fatalf("outlook = %+v", gs.Outlook)
	}
	if got := strings.Join(gs.Plan.FutureTerms, ","); got != "2027前期,2027後期" {
		t.Fatalf("future terms = %s", got)
	}

	// 残り 60 単位: 今期 16 を引いて 44 > 上限 24 × 2 学期 = 48 ではない → まだ間に合う。70 なら 54 > 48 で間に合わない
	gs, grades, reg = outlookFixture()
	gs.TotalShortfall = 70
	buildOutlook(gs, grades, reg, nil)
	if gs.Outlook.Status != "at_risk" || !strings.Contains(gs.Outlook.Reasons[0], "48 単位までしか取れません") {
		t.Fatalf("outlook = %+v", gs.Outlook)
	}
}

func TestOutlookWithPlan(t *testing.T) {
	gs, grades, reg := outlookFixture()
	plan := []db.PlanItem{
		{YearTerm: "2027前期", Title: "試験特論A", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 8},
		{YearTerm: "2027後期", Title: "卒業研究Ⅱ", Section: "専門教育", Category: "研究", Credits: 16},
	}
	buildOutlook(gs, grades, reg, plan)
	// 合計: 40 - 16 - 24 = 0、知能情報アドバンスト: 22 - 10 - 4 - 8 = 0
	if gs.Outlook.Status != "on_track" || gs.Outlook.Basis != "plan" || gs.Plan.TotalShortfallAfterPlan != 0 {
		t.Fatalf("outlook = %+v, plan = %+v", gs.Outlook, gs.Plan)
	}

	// 区分の計画が足りない・上限超過
	gs, grades, reg = outlookFixture()
	plan[0].Credits = 4
	plan[1].Credits = 30
	buildOutlook(gs, grades, reg, plan)
	reasons := strings.Join(gs.Outlook.Reasons, "\n")
	if gs.Outlook.Status != "at_risk" || !strings.Contains(reasons, "「知能情報アドバンスト」（専門教育）は計画どおりに取れても 4 単位足りません") ||
		!strings.Contains(reasons, "2027後期 の計画 30 単位が履修上限 24 単位を超えています") {
		t.Fatalf("reasons = %s", reasons)
	}
}

func TestOutlookDetectsDeviationFromPlan(t *testing.T) {
	gs, grades, reg := outlookFixture()
	plan := []db.PlanItem{
		{YearTerm: "2026前期", Title: "試験機械学習", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 2},   // 修得済み
		{YearTerm: "2026前期", Title: "試験ロボット工学", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 2}, // 不合格
		{YearTerm: "2026後期", Title: "試験画像処理", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 2},   // 今期未登録
		{YearTerm: "2027前期", Title: "特論", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 24},
		{YearTerm: "2027後期", Title: "卒業研究Ⅱ", Section: "専門教育", Category: "研究", Credits: 24},
	}
	buildOutlook(gs, grades, reg, plan)
	if len(gs.Plan.Deviations) != 2 {
		t.Fatalf("deviations = %+v", gs.Plan.Deviations)
	}
	reasons := strings.Join(gs.Outlook.Reasons, "\n")
	if gs.Outlook.Status != "at_risk" || !strings.Contains(reasons, "「試験ロボット工学」（2026前期）: 不合格でした") ||
		!strings.Contains(reasons, "「試験画像処理」（2026後期）: 今期の履修登録（第1希望）にありません") {
		t.Fatalf("outlook = %+v", gs.Outlook)
	}
}

func TestOutlookMatchesRegisteredTitleVariantsWithoutMarkingThemEarned(t *testing.T) {
	gs, grades, reg := outlookFixture()
	reg.Lectures = []kyomu.RegisteredLecture{
		{Rank: 1, Title: "試験デザイン演習 3組", Credits: 2},
		{Rank: 1, Title: "セミナーⅠ 2組", Credits: 1},
		{Rank: 1, Title: "試験特別講義I", Credits: 2},
	}
	plan := []db.PlanItem{
		{YearTerm: "2026後期", Title: "試験デザイン演習", Credits: 2},
		{YearTerm: "2026後期", Title: "セミナーＩ", Credits: 1},
		{YearTerm: "2026後期", Title: "試験特別講義 Ⅰ", Credits: 2},
		{YearTerm: "2026後期", Title: "試験特別講義Ⅱ", Credits: 2},
	}
	buildOutlook(gs, grades, reg, plan)
	if len(gs.Plan.Deviations) != 1 || gs.Plan.Deviations[0].Item.Title != "試験特別講義Ⅱ" {
		t.Fatalf("incorrect unregistered courses: %+v", gs.Plan.Deviations)
	}
	count := 0
	for _, year := range gs.Plan.Years {
		for _, term := range year.Terms {
			if term.State == "current" {
				for _, item := range term.Taken {
					count++
					if item.Passed {
						t.Fatal("enrollment counted as earned", item)
					}
				}
			}
		}
	}
	if count != 3 {
		t.Fatalf("registered courses = %d", count)
	}
}

func TestOutlookMetAndUnknown(t *testing.T) {
	gs, grades, reg := outlookFixture()
	gs.TotalShortfall, gs.Shortfalls = 0, nil
	buildOutlook(gs, grades, reg, nil)
	if gs.Outlook.Status != "met" {
		t.Fatalf("status = %s", gs.Outlook.Status)
	}
	empty := &GraduationStatus{}
	buildOutlook(empty, nil, nil, nil)
	if empty.Outlook.Status != "unknown" {
		t.Fatalf("status = %s", empty.Outlook.Status)
	}
}

func TestTermHelpers(t *testing.T) {
	if termKey("2026年度後期") != "2026後期" || nextTerm("2026後期") != "2027前期" || nextTerm("2027前期") != "2027後期" {
		t.Fatal("term helpers")
	}
	if graduationMonth("2027後期") != "2028年3月" || graduationMonth("2028前期") != "2028年9月" {
		t.Fatal("graduation month")
	}
}

func thesisFixture(required ...RequiredSubjectStatus) *GraduationStatus {
	gs, grades, reg := outlookFixture()
	gs.RequiredCourses = &RequiredCheck{Required: required}
	buildOutlook(gs, grades, reg, nil)
	return gs
}

func req(title, status string) RequiredSubjectStatus {
	return RequiredSubjectStatus{RequiredSubject: university.RequiredSubject{Title: title}, Status: status}
}

func conditionOK(t *testing.T, tc *ThesisCheck) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, c := range tc.Conditions {
		out[c.Kind] = c.OK
	}
	return out
}

// 知能情報（R6）: 6 個学期在学で取得単位数 100 以上、原則 3 年後学期までの専門必修をすべて修得
func TestEvaluateThesisChinot(t *testing.T) {
	rule := &university.ThesisRule{CreditsBySemesters: map[int]float64{5: 85, 6: 100}, RequiresMajorRequired: true}
	gs := thesisFixture(req("試験離散数学", "passed"), req("試験情報総合実験", "enrolled"), req("試験プログラミングⅡ", "not_passed"),
		req("卒業研究Ⅰ", "not_passed"), req("セミナーⅠ", "not_passed"))

	// 3年後期: 修得 90 + 今期 16 = 106 >= 100。ただし専門必修「試験プログラミングⅡ」が未修得（卒研・セミナーは対象外）
	tc := &ThesisCheck{Earned: gs.TotalEarned}
	evaluateThesis(tc, gs, "後期", rule, map[string]bool{})
	ok := conditionOK(t, tc)
	if !tc.Supported || tc.CheckTerm != "2027前期" || tc.Projected != 106 || !ok["credits_min"] || ok["all_required"] ||
		tc.Status != "at_risk" || strings.Join(tc.MissingRequired, ",") != "試験プログラミングⅡ" {
		t.Fatalf("thesis = %+v", tc)
	}

	// 修得の見込みがあれば（成績・登録・計画のいずれか）likely
	tc = &ThesisCheck{Earned: gs.TotalEarned}
	evaluateThesis(tc, gs, "後期", rule, map[string]bool{"試験プログラミングⅡ": true})
	if tc.Status != "likely" {
		t.Fatalf("thesis = %+v", tc)
	}

	// 単位が足りない（修得 70 + 今期 16 = 86 < 100）
	gs.TotalEarned = 70
	tc = &ThesisCheck{Earned: gs.TotalEarned}
	evaluateThesis(tc, gs, "後期", rule, map[string]bool{"試験プログラミングⅡ": true})
	if conditionOK(t, tc)["credits_min"] || tc.Status != "at_risk" {
		t.Fatalf("thesis = %+v", tc)
	}
}

// 社会基盤デザイン（R6）: 残り 20 単位以下、卒研以外の専門必修の未修得 4 科目以下、特定科目の修得
func TestEvaluateThesisShakai(t *testing.T) {
	four := 4
	rule := &university.ThesisRule{MaxRemainingCredits: 20, MaxMissingMajorRequired: &four,
		RequiredCourses: [][]string{{"社会基盤デザイン実験"}, {"鋼構造設計演習", "コンクリート構造設計演習"}}}
	gs := thesisFixture(req("A", "not_passed"), req("B", "not_passed"), req("卒業研究Ⅰ", "not_passed"))
	// 残り 40 - 今期 16 = 24 > 20
	tc := &ThesisCheck{}
	evaluateThesis(tc, gs, "後期", rule, map[string]bool{"社会基盤デザイン実験": true, "コンクリート構造設計演習": true})
	ok := conditionOK(t, tc)
	if tc.RemainingAtCheck != 24 || ok["remaining_max"] || !ok["missing_required_max"] || tc.Status != "at_risk" {
		t.Fatalf("thesis = %+v", tc)
	}
	if len(tc.Conditions) != 4 || !tc.Conditions[2].OK || !tc.Conditions[3].OK || tc.Conditions[3].Label != "鋼構造設計演習 または コンクリート構造設計演習 を修得" {
		t.Fatalf("conditions = %+v", tc.Conditions)
	}

	// 残り 18、どちらの設計演習も未修得
	gs.TotalShortfall = 34
	tc = &ThesisCheck{}
	evaluateThesis(tc, gs, "後期", rule, map[string]bool{"社会基盤デザイン実験": true})
	if !conditionOK(t, tc)["remaining_max"] || tc.Conditions[3].OK || tc.Status != "at_risk" {
		t.Fatalf("thesis = %+v", tc)
	}
	tc = &ThesisCheck{}
	evaluateThesis(tc, gs, "後期", rule, map[string]bool{"社会基盤デザイン実験": true, "鋼構造設計演習": true})
	if tc.Status != "likely" {
		t.Fatalf("thesis = %+v", tc)
	}
}

// 建築学（R6）: 残り 24 単位以下。3年前期なら 3年後期の計画も見込みに入れる
func TestEvaluateThesisKenchikuWithPlan(t *testing.T) {
	rule := &university.ThesisRule{MaxRemainingCredits: 24}
	gs, grades, reg := outlookFixture()
	gs.TotalShortfall = 60
	reg.YearTerm = "2026年度前期"
	buildOutlook(gs, grades, reg, []db.PlanItem{{YearTerm: "2026後期", Title: "建築設計演習", Credits: 20}})
	tc := &ThesisCheck{}
	evaluateThesis(tc, gs, "前期", rule, map[string]bool{})
	// 60 - 今期 16 - 計画 20 = 24 <= 24、判定は 2027前期
	if tc.CheckTerm != "2027前期" || tc.RemainingAtCheck != 24 || tc.Status != "likely" {
		t.Fatalf("thesis = %+v", tc)
	}
}

func TestEvaluateThesisWithoutRule(t *testing.T) {
	gs := thesisFixture()
	tc := &ThesisCheck{}
	evaluateThesis(tc, gs, "後期", nil, map[string]bool{})
	if tc.Status != "none" || tc.Mentioned || tc.Supported {
		t.Fatalf("no rule: %+v", tc)
	}
	tc = &ThesisCheck{}
	evaluateThesis(tc, gs, "後期", &university.ThesisRule{Evidence: university.Passage{Text: "※5 卒業研究を履修するためには，指導教員の許可を得なければならない。"}}, map[string]bool{})
	if tc.Status != "unknown" || !tc.Mentioned || tc.Supported {
		t.Fatalf("unparsed rule: %+v", tc)
	}
}

func TestThesisDoesNotReportMissingSubjectsWhenRegistrationIsUnavailable(t *testing.T) {
	s := New("https://example.test", nil)
	gs := thesisFixture(req("試験デザイン演習", "unverified"))
	prof := &kyomu.StudentProfile{EnterYear: 2024, Faculty: "工学部", Course: "知能情報コース", Grade: "３年後期"}
	tc := s.thesisCheck(context.Background(), gs, prof, &kyomu.GradeList{}, nil)
	if tc == nil || tc.Status != "unknown" || len(tc.MissingRequired) != 0 || len(tc.Conditions) != 0 {
		t.Fatalf("unavailable registration became missing subjects: %+v", tc)
	}
}

// 2024 年度入学の 3 年後期。1・2 年次は成績を振り返り、今期は履修登録、4 年次は計画を並べる
func TestPlanYears(t *testing.T) {
	gs, grades, reg := outlookFixture()
	gs.Profile = &kyomu.StudentProfile{EnterYear: 2024, Grade: "３年後期"}
	grades.Grades = append(grades.Grades,
		kyomu.Grade{Year: 2024, Term: "前期", Title: "試験英語", Credits: 2, Grade: "A", Passed: true},
		kyomu.Grade{Year: 2025, Term: "後期", Title: "試験離散数学", Credits: 2, Grade: "F"},
	)
	plan := []db.PlanItem{{YearTerm: "2027前期", Title: "特論", Section: "専門教育", Category: "知能情報アドバンスト", Credits: 26}}
	buildOutlook(gs, grades, reg, plan)
	ys := gs.Plan.Years
	if len(ys) != 4 || !ys[2].Current || ys[0].Current {
		t.Fatalf("years = %+v", ys)
	}
	if y1 := ys[0].Terms[0]; y1.YearTerm != "2024前期" || y1.State != "past" || y1.EarnedCredits != 2 || len(y1.Taken) != 1 || y1.Plannable() {
		t.Fatalf("1年前期 = %+v", y1)
	}
	if y2 := ys[1].Terms[1]; y2.EarnedCredits != 0 || len(y2.Taken) != 1 || y2.Taken[0].Passed {
		t.Fatalf("2年後期 = %+v", y2)
	}
	if cur := ys[2].Terms[1]; cur.State != "current" || cur.RegisteredCredits != 2 || cur.Taken[0].Title != "試験技術演習II" || !cur.Plannable() {
		t.Fatalf("3年後期 = %+v", cur)
	}
	// 3 年次: 前期の修得 2（試験機械学習。試験ロボット工学は不合格）+ 今期の登録 2
	if ys[2].Credits != 4 {
		t.Fatalf("3年次の単位 = %v", ys[2].Credits)
	}
	if f := ys[3].Terms[0]; f.State != "future" || f.PlannedCredits != 26 || !f.OverLimit || len(f.Items) != 1 {
		t.Fatalf("4年前期 = %+v", f)
	}

	// 入学年度が分からなければ年次から逆算する
	if enterYear(&kyomu.StudentProfile{Grade: "２年前期"}, 2026) != 2025 || enterYear(nil, 2026) != 2026 {
		t.Fatal("enterYear")
	}
}

func TestCategoryItems(t *testing.T) {
	reqs := []kyomu.Requirement{
		{Section: "専門教育", Name: "知能情報アドバンスト", Kind: "item", Required: fp(22), Earned: fp(2)},
		{Section: "共通教育", Name: "英語", Kind: "item", Required: fp(8), Earned: fp(2)},
	}
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{
		{Title: "試験機械学習", Credits: 2, Passed: true, Category: "専門教育科目（知能情報アドバンスト）"},
		{Title: "試験英語", Credits: 2, Passed: true, Category: "共通教育科目（英語）"},
		{Title: "教職", Credits: 2, Passed: true, Category: "（教職）教職に関する科目"},
	}}
	got := categoryItems(grades, &kyomu.Registration{}, reqs)
	if got["専門教育科目（知能情報アドバンスト）"] != "知能情報アドバンスト" || got["共通教育科目（英語）"] != "英語" || len(got) != 2 {
		t.Fatalf("category items = %v", got)
	}
}
