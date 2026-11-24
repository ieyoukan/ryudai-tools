package academic

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

func sampleRegistration() *kyomu.Registration {
	return &kyomu.Registration{
		YearTerm: "2026年度後期",
		Open:     true,
		Credits:  &kyomu.CreditSummary{Limit: 20, LimitedCredits: 10, ExemptCredits: 2, TotalCredits: 12, RemainingCredit: 10},
		Lectures: []kyomu.RegisteredLecture{
			{Day: "火", Period: 4, Rank: 1, LectureCode: "A", Title: "演習", Credits: 2},
			{Day: "水", Period: 3, Rank: 1, LectureCode: "B", Title: "プログラミング入門", Credits: 2},
			{Day: "水", Period: 3, Rank: 2, LectureCode: "C", Title: "試験データ構造", Credits: 2},
			{Day: "集中等", Period: 1, Rank: 1, LectureCode: "D", Title: "特別講義", Credits: 2, LotResult: "当選"},
		},
	}
}

func TestBuildOverview(t *testing.T) {
	o := buildOverview(sampleRegistration())
	if len(o.FreeSlots) != 25-2 {
		t.Errorf("空きコマ = %d, want 23", len(o.FreeSlots))
	}
	for _, s := range o.FreeSlots {
		if (s.Day == "火" && s.Period == 4) || (s.Day == "水" && s.Period == 3) {
			t.Errorf("埋まっているコマが空き扱い: %+v", s)
		}
	}
	if o.FirstChoiceCredit != 6 {
		t.Errorf("第1希望単位 = %v, want 6", o.FirstChoiceCredit)
	}
	joined := strings.Join(o.Notes, "\n")
	for _, want := range []string{"履修登録を編集できます", "あと 10 単位", "第2希望"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Notes に %q が無い:\n%s", want, joined)
		}
	}

	// 抽選結果・登録調整（当選・空きあり・定員充足）は時間割のコマに出すので、注意書きには重ねて書かない
	r := sampleRegistration()
	r.Lectures[0].Adjustment = "定員充足"
	if joined := strings.Join(buildOverview(r).Notes, "\n"); strings.Contains(joined, "定員充足") || strings.Contains(joined, "抽選結果") {
		t.Errorf("Notes に登録調整が残っている:\n%s", joined)
	}
}

func TestConflicts(t *testing.T) {
	occ := occupied(sampleRegistration())
	got := conflictsWith([]kyomu.Slot{{Day: "水", Period: 3}, {Day: "木", Period: 1}}, occ, "B")
	want := []string{"水3: 試験データ構造"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if !slotsOverlap(kyomu.Slot{Day: "月", Period: 3, Half: "前半"}, kyomu.Slot{Day: "月", Period: 3}) {
		t.Error("前半と通常コマは重なる")
	}
	if slotsOverlap(kyomu.Slot{Day: "月", Period: 3, Half: "前半"}, kyomu.Slot{Day: "月", Period: 3, Half: "後半"}) {
		t.Error("前半と後半は重ならない")
	}
}

func TestMethodRank(t *testing.T) {
	if !(methodRank("自動登録制") < methodRank("Web教員許可制") && methodRank("Web教員許可制") < methodRank("抽選") && methodRank("抽選") < methodRank("許可コード制")) {
		t.Error("登録しやすさの順序が想定と違う")
	}
}

func TestYearOf(t *testing.T) {
	if y := yearOf("2026年度後期"); y != 2026 {
		t.Errorf("yearOf = %d", y)
	}
	if y := yearOf(""); y != 0 {
		t.Errorf("yearOf empty = %d", y)
	}
}

func TestTTLCache(t *testing.T) {
	c := newTTLCache[int](time.Hour, 2)
	c.set("a", 1)
	c.set("b", 2)
	c.set("c", 3) // 上限 2 なので最も古い a が消える
	if _, ok := c.get("a"); ok {
		t.Error("a が残っている")
	}
	if v, ok := c.get("c"); !ok || v != 3 {
		t.Error("c が取れない")
	}
	exp := newTTLCache[int](-time.Second, 10)
	exp.set("x", 1)
	if _, ok := exp.get("x"); ok {
		t.Error("期限切れが返った")
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	s := New("", nil)
	if _, err := s.SearchSyllabus(kyomu.SearchParams{Year: 2026}, 10, false); err != ErrEmptyQuery {
		t.Errorf("err = %v", err)
	}
	if err := s.withUser("x", func(*kyomu.Client) error { return nil }); err != ErrNoCredentials {
		t.Errorf("認証情報なしで err = %v", err)
	}
}

func TestExcerpt(t *testing.T) {
	cases := map[string]string{
		"知的システ\nムの設計":      "知的システムの設計",
		"hello\nworld":     "hello world",
		"  a  b \n\n 日本語 ": "a b日本語",
		"あいうえおかきくけこ":       "あいうえお…",
	}
	for in, want := range cases {
		n := 100
		if in == "あいうえおかきくけこ" {
			n = 5
		}
		if got := excerpt(in, n); got != want {
			t.Errorf("excerpt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkloadAndEligibility(t *testing.T) {
	syl := &kyomu.Syllabus{
		Title:        "会話C1F(振替)",
		Grading:      "期末試験 50%、毎回の課題 30%、発表 20%",
		Plan:         "第1回 ガイダンス\n第2回 グループワーク",
		LectureTypes: []string{"演習又は実験"},
		Requirements: "留学生のみ対象とする。日本語能力試験N2程度。\n工学科の学生は履修不可",
		Message:      "初回は必ず出席すること。教室は国際教育センター",
	}
	sig := workloadSignals(syl)
	for _, want := range []string{"期末試験", "毎回の課題", "発表・プレゼン", "グループワーク"} {
		if !contains(sig, want) {
			t.Errorf("WorkloadSignals に %q が無い: %v", want, sig)
		}
	}
	notes := eligibilityNotes(syl)
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"振替科目", "留学生のみ対象とする", "工学科の学生は履修不可"} {
		if !strings.Contains(joined, want) {
			t.Errorf("EligibilityNotes に %q が無い: %v", want, notes)
		}
	}
	if strings.Contains(joined, "日本語能力試験") {
		t.Errorf("対象者と無関係な文まで拾っている: %v", notes)
	}
}

func TestNeighborsAndBreakMinutes(t *testing.T) {
	s := New("", nil)
	reg := &kyomu.Registration{Lectures: []kyomu.RegisteredLecture{
		{Day: "木", Period: 2, Rank: 1, LectureCode: "P", Title: "前の講義"},
		{Day: "木", Period: 4, Rank: 1, LectureCode: "N", Title: "後の講義"},
		{Day: "金", Period: 2, Rank: 1, LectureCode: "F", Title: "別の曜日"},
	}}
	rooms := map[string]string{"P": "[木2]工1－221", "N": "[木4]国セ101"}
	got := s.neighbors(reg, rooms, []kyomu.Slot{{Day: "木", Period: 3}}, "X")
	if len(got) != 2 {
		t.Fatalf("neighbors = %+v", got)
	}
	if got[0].Position != "前" || got[0].Slot != "木2" || got[0].Room != "[木2]工1－221" || got[0].BreakMinutes != 60 {
		t.Errorf("前: %+v（2限と3限の間は昼休み 60 分）", got[0])
	}
	if got[1].Position != "後" || got[1].Slot != "木4" || got[1].BreakMinutes != 20 {
		t.Errorf("後: %+v（3限と4限の間は 20 分）", got[1])
	}
	// 2 コマ続きの講義では、自分のもう一方のコマは前後に数えない
	if got := s.neighbors(reg, rooms, []kyomu.Slot{{Day: "木", Period: 3}, {Day: "木", Period: 4}}, "X"); len(got) != 1 {
		t.Errorf("連続コマ: %+v", got)
	}
}

func TestJapaneseCourseNote(t *testing.T) {
	notes := eligibilityNotes(&kyomu.Syllabus{Title: "日本語A1", Faculty: "国際教育センター", Requirements: "協力的にグループで学習する意志があること"})
	if !contains(notes, japaneseCourseNote) {
		t.Errorf("留学生向けの可能性の注意が無い: %v", notes)
	}
	if notes := eligibilityNotes(&kyomu.Syllabus{Title: "日本語のしくみ", Faculty: "共通教育等科目"}); contains(notes, japaneseCourseNote) {
		t.Errorf("共通教育の日本語学の科目まで留学生向け扱いしている: %v", notes)
	}
}

func f64(v float64) *float64 { return &v }

func TestNormalizeCategory(t *testing.T) {
	pairs := [][2]string{
		{"専門教育科目（研究・実験）", "研究実験"},
		{"選択科目（工学融合科目）", "工学融合（選択）"},
		{"専門教育科目（知能情報関連）", "知能情報関連"},
		{"共通教育科目（人文系科目）", "人文"},
		{"共通教育科目（その他の外国語）", "その他"},
		{"共通教育科目（情報関係科目）", "情報関係"},
		{"（教職）教職に関する科目", "教職"},
	}
	for _, p := range pairs {
		if normalizeCategory(p[0]) != normalizeCategory(p[1]) {
			t.Errorf("%q (%q) と %q (%q) が一致しない", p[0], normalizeCategory(p[0]), p[1], normalizeCategory(p[1]))
		}
	}
	if normalizeCategory("専門教育科目（知能情報コア）") == normalizeCategory("知能情報関連") {
		t.Error("別の区分が一致してしまう")
	}
}

func TestBuildGraduationStatus(t *testing.T) {
	report := &kyomu.GradeReport{GPA: f64(3.1), Requirements: []kyomu.Requirement{
		{Section: "専門教育", Name: "研究実験", Kind: "item", Required: f64(16), Earned: f64(4), Shortfall: f64(12)},
		{Section: "専門教育", Name: "工学融合（選択）", Kind: "item", Required: f64(4), Earned: f64(2), Shortfall: f64(2)},
		{Section: "専門教育", Name: "知能情報コア", Kind: "item", Required: f64(26), Earned: f64(26), Shortfall: f64(0)},
		{Section: "共通教育", Name: "小計１", Kind: "subtotal", Required: f64(12), Earned: f64(11), Shortfall: f64(1)},
		{Section: "専門教育", Name: "知能情報関連", Kind: "item", Earned: f64(4)},
		{Section: "専門教育", Name: "教職", Kind: "item"},
		{Section: "専門教育", Name: "専門計", Kind: "total", Required: f64(94), Earned: f64(64), Shortfall: f64(30)},
		{Section: "専門教育", Name: "合計", Kind: "total", Required: f64(130), Earned: f64(98), Shortfall: f64(32)},
	}}
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{
		{Title: "試験データ構造", Grade: "F", Passed: false, Category: "専門教育科目（研究・実験）"},
		{Title: "試験教職入門", Grade: "F", Passed: false, Category: "（教職）教職に関する科目"},
		{Title: "試験英語 01組", Grade: "F", Passed: false},
		{Title: "試験英語 02組", Grade: "B", Passed: true}, // 別の組で再履修して合格
		{Title: "試験微分積分", Grade: "A", Passed: true},
	}}
	reg := &kyomu.Registration{YearTerm: "2026年度後期", Open: true, Credits: &kyomu.CreditSummary{Limit: 26},
		Lectures: []kyomu.RegisteredLecture{
			{Rank: 1, Title: "演習", Credits: 2, Category: "専門教育科目（研究・実験）"},
			{Rank: 1, Title: "試験電気電子基礎", Credits: 2, Category: "選択科目（工学融合科目）"},
			{Rank: 1, Title: "試験機械学習", Credits: 2, Category: "専門教育科目（知能情報関連）"},
			{Rank: 2, Title: "第2希望", Credits: 2, Category: "専門教育科目（研究・実験）"}, // 数えない
		}}
	prof := &kyomu.StudentProfile{Grade: "３年後期", Department: "工学科"}
	gs := buildGraduationStatus(grades, report, reg, prof)

	if gs.TotalRequired != 130 || gs.TotalEarned != 98 || gs.TotalShortfall != 32 {
		t.Errorf("合計 = %v / %v / %v", gs.TotalRequired, gs.TotalEarned, gs.TotalShortfall)
	}
	if len(gs.Shortfalls) != 5 || gs.Shortfalls[0].Kind != "item" || gs.Shortfalls[len(gs.Shortfalls)-1].Kind != "total" {
		t.Errorf("不足一覧の並び = %+v", gs.Shortfalls)
	}
	if gs.ThisTerm.RegisteredCredits != 6 {
		t.Errorf("今期の登録単位 = %v（第2希望は数えない）", gs.ThisTerm.RegisteredCredits)
	}
	proj := map[string]ProjectedItem{}
	for _, p := range gs.ThisTerm.Projection {
		proj[p.Name] = p
	}
	if p := proj["研究実験"]; p.RegisteredNow != 2 || p.ShortfallAfter != 10 {
		t.Errorf("研究実験 = %+v", p)
	}
	if p := proj["工学融合（選択）"]; p.RegisteredNow != 2 || p.ShortfallAfter != 0 {
		t.Errorf("工学融合 = %+v", p)
	}
	if p := proj["合計"]; p.RegisteredNow != 6 || p.ShortfallAfter != 26 {
		t.Errorf("合計 = %+v", p)
	}
	if _, ok := proj["小計１"]; ok {
		t.Error("構成の分からない小計まで見通しを出している")
	}
	if !strings.Contains(strings.Join(gs.Notes, "\n"), "修得していない科目が 1 件") {
		t.Errorf("必須区分の不合格だけ数える: %v", gs.Notes)
	}
	if gs.RemainingTerms != 3 || gs.PerTermAfterThis == nil || *gs.PerTermAfterThis != 13 {
		t.Errorf("残り学期 = %d, 1 学期あたり = %v（今期後に 26 単位を 2 学期で）", gs.RemainingTerms, gs.PerTermAfterThis)
	}
	if p := proj["専門計"]; p.RegisteredNow != 6 || p.ShortfallAfter != 24 {
		t.Errorf("専門計 = %+v（専門教育の区分に登録した 6 単位を足す）", p)
	}
	if len(gs.FailedCourses) != 2 || gs.FailedCourses[0].Title != "試験データ構造" {
		t.Fatalf("未修得の不合格 = %+v（別の組で合格した試験英語は除く）", gs.FailedCourses)
	}
	if c := gs.FailedCourses[0].CountsTowardRequirement; c == nil || !*c {
		t.Errorf("研究実験の不合格は必須区分 = %v", c)
	}
	if c := gs.FailedCourses[1].CountsTowardRequirement; c == nil || *c {
		t.Errorf("教職の不合格は必須区分外 = %v", c)
	}
	if !strings.Contains(strings.Join(gs.Notes, "\n"), "「研究実験」（専門教育）は今期の登録だけでは 10 単位足りません") {
		t.Errorf("Notes = %v", gs.Notes)
	}
}

func TestRemainingTerms(t *testing.T) {
	cases := map[string]int{"１年前期": 8, "３年後期": 3, "４年後期": 1, "４年前期": 2, "": 0}
	for g, want := range cases {
		if got := remainingTerms(&kyomu.StudentProfile{Grade: g}); got != want {
			t.Errorf("%q → %d, want %d", g, got, want)
		}
	}
	if got := remainingTerms(&kyomu.StudentProfile{Grade: "１年前期", Department: "医学科"}); got != 12 {
		t.Errorf("医学科 1 年前期 → %d", got)
	}
}
