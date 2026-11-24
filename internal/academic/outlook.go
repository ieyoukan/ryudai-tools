package academic

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/ieyoukan/daigaku-hack/internal/db"
	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
	"github.com/ieyoukan/daigaku-hack/internal/provider/university"
)

// PlanStore は卒業までの履修計画を返す（db.DB が実装）。
type PlanStore interface {
	PlanItems(userID string) ([]db.PlanItem, error)
}

// Outlook は卒業要件を満たせるかの予測。
//
//   - met: 必須単位をすべて満たしている
//   - on_track: まだ満たしていないが、計画どおり（計画が無ければ今のペース）なら卒業予定までに満たせる
//   - at_risk: このままでは卒業予定までに満たせない（Reasons に理由）
//   - unknown: 判定に必要な情報が無い
type Outlook struct {
	Status             string   `json:"status"`
	Basis              string   `json:"basis,omitempty"` // plan（履修計画から）/ pace（今期の登録と履修上限から）
	ExpectedGraduation string   `json:"expected_graduation,omitempty"`
	Reasons            []string `json:"reasons"`
}

// PlanOutlook は履修計画を卒業要件に当てはめた結果。
type PlanOutlook struct {
	CurrentTerm string         `json:"current_term"`
	FutureTerms []string       `json:"future_terms"` // 計画を立てられる学期（来期から卒業予定まで）
	Terms       []PlanTerm     `json:"terms"`
	Categories  []PlanCategory `json:"categories"`
	// TotalPlanned は来期以降に計画している単位の合計。
	TotalPlanned float64 `json:"total_planned"`
	// TotalShortfallAfterPlan は今期の登録と計画がすべて単位になった後の、卒業までの不足単位。
	TotalShortfallAfterPlan float64         `json:"total_shortfall_after_plan"`
	CreditLimit             float64         `json:"credit_limit,omitempty"`
	Deviations              []PlanDeviation `json:"deviations"`
	// Years は入学からの年次ごとの学期。終わった学期は成績、今期は履修登録、来期以降は計画を並べる。
	Years []PlanYear `json:"years"`
}

// PlanYear は 1 年次分（前期・後期）。
type PlanYear struct {
	Level   int            `json:"level"` // 年次（1 年次 = 入学年度）
	Current bool           `json:"current,omitempty"`
	Credits float64        `json:"credits"` // 修得・今期の登録・計画の合計
	Terms   []PlanYearTerm `json:"terms"`
}

// PlanYearTerm は年次の中の 1 学期。
type PlanYearTerm struct {
	YearTerm string `json:"year_term"` // 例: 2027前期
	Label    string `json:"label"`     // 前期 / 後期
	// State は past（終わった学期）/ current（今期）/ future（卒業予定までの学期）/ after（卒業予定より後）。
	State             string        `json:"state" tstype:"'past' | 'current' | 'future' | 'after'"`
	Taken             []TakenCourse `json:"taken"` // 終わった学期は成績、今期は履修登録（第1希望）
	Items             []db.PlanItem `json:"items"` // この学期の計画
	EarnedCredits     float64       `json:"earned_credits"`
	RegisteredCredits float64       `json:"registered_credits"`
	PlannedCredits    float64       `json:"planned_credits"`
	OverLimit         bool          `json:"over_limit,omitempty"`
}

// Plannable は計画に科目を足せる学期（今期と、卒業予定までの来期以降）か。
func (t PlanYearTerm) Plannable() bool { return t.State == "current" || t.State == "future" }

// TakenCourse は成績または今期の履修登録の 1 科目。
type TakenCourse struct {
	LectureCode string  `json:"lecture_code,omitempty"`
	Title       string  `json:"title"`
	Credits     float64 `json:"credits"`
	Grade       string  `json:"grade,omitempty"`
	Passed      bool    `json:"passed,omitempty"`
	Category    string  `json:"category,omitempty"`
	// Unmapped は今期の登録で区分が分からない科目、Suggested はそのときの区分の候補。
	Unmapped  bool   `json:"unmapped,omitempty"`
	Suggested string `json:"suggested,omitempty"`
}

// PlanTerm は 1 学期分の計画。
type PlanTerm struct {
	YearTerm  string        `json:"year_term"`
	Past      bool          `json:"past,omitempty"`
	Current   bool          `json:"current,omitempty"`
	Credits   float64       `json:"credits"`
	OverLimit bool          `json:"over_limit,omitempty"`
	Items     []db.PlanItem `json:"items"`
}

// PlanCategory は区分ごとの計画後の見通し。
type PlanCategory struct {
	Section            string  `json:"section"`
	Name               string  `json:"name"`
	Required           float64 `json:"required"`
	Earned             float64 `json:"earned"`
	Registered         float64 `json:"registered"`
	Planned            float64 `json:"planned"`
	ShortfallAfterPlan float64 `json:"shortfall_after_plan"`
}

// PlanDeviation は計画どおりに進まなかった科目（成績が出た学期の不合格・未修得、今期の未登録）。
type PlanDeviation struct {
	Item   db.PlanItem `json:"item"`
	Reason string      `json:"reason"`
}

// ThesisCheck は卒業研究の登録条件（研究室への仮配属・卒研着手の前提）を満たせそうか。3 年次にだけ作る。
// 条件はコースごとに違うので、便覧のコースの資料から読み取った条件（Rule）ごとに判定する。
type ThesisCheck struct {
	// Mentioned は便覧に卒業研究の登録条件の注記があるか、Supported はそこから自動判定できる条件を読み取れたか。
	Mentioned bool                   `json:"mentioned"`
	Supported bool                   `json:"supported"`
	Rule      *university.ThesisRule `json:"rule,omitempty"`
	CheckTerm string                 `json:"check_term,omitempty"` // 条件を判定する時点（4 年次の 4 月の学期）
	Earned    float64                `json:"earned"`
	// Projected は判定時点までに取れる見込みの単位（修得済み + 今期の登録 + それまでの計画）、
	// RemainingAtCheck はその時点での卒業までの残り単位の見込み。
	Projected        float64           `json:"projected"`
	RemainingAtCheck float64           `json:"remaining_at_check"`
	Conditions       []ThesisCondition `json:"conditions"`
	MissingRequired  []string          `json:"missing_required,omitempty"` // 判定時点までに修得の見込みがない専門必修（卒研以外）
	Status           string            `json:"status"`                     // likely / at_risk / unknown / none（便覧に条件の記載なし）
	Notes            []string          `json:"notes"`
	DocumentURL      string            `json:"document_url,omitempty"`
}

// ThesisCondition は卒業研究の登録条件の 1 項目の判定。
type ThesisCondition struct {
	Kind   string   `json:"kind"` // credits_min / remaining_max / all_required / missing_required_max / courses
	Label  string   `json:"label"`
	OK     bool     `json:"ok"`
	Value  float64  `json:"value"`
	Target float64  `json:"target"`
	Items  []string `json:"items,omitempty"` // 足りない科目など
}

var yearTermRe = regexp.MustCompile(`(\d{4}).*?(前|後)`)

// termKey は 2026年度後期・2026後期 などを "2026後期" に揃える。並べ替えは文字列比較でよい（前 < 後）。
func termKey(s string) string {
	m := yearTermRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1] + m[2] + "期"
}

func nextTerm(key string) string {
	y, _ := strconv.Atoi(key[:4])
	if strings.Contains(key, "前") {
		return fmt.Sprintf("%d後期", y)
	}
	return fmt.Sprintf("%d前期", y+1)
}

// currentTermKey は今期（履修登録の年度・学期、無ければ日付から）を返す。
func currentTermKey(reg *kyomu.Registration) string {
	if reg != nil {
		if k := termKey(reg.YearTerm); k != "" {
			return k
		}
	}
	term := "後期"
	if strings.Contains(university.DefaultTerm(), "前") {
		term = "前期"
	}
	return fmt.Sprintf("%d%s", university.CurrentYear(), term)
}

// graduationMonth は最後の学期から卒業予定の年月を返す（後期なら翌年 3 月、前期なら 9 月）。
func graduationMonth(lastTerm string) string {
	y, _ := strconv.Atoi(lastTerm[:4])
	if strings.Contains(lastTerm, "後") {
		return fmt.Sprintf("%d年3月", y+1)
	}
	return fmt.Sprintf("%d年9月", y)
}

// buildOutlook は計画（あれば）または今期の登録と履修上限から、卒業予定までに要件を満たせるかを予測する。
func buildOutlook(gs *GraduationStatus, grades *kyomu.GradeList, reg *kyomu.Registration, plan []db.PlanItem) {
	current := currentTermKey(reg)
	po := &PlanOutlook{CurrentTerm: current, FutureTerms: []string{}, Terms: []PlanTerm{}, Categories: []PlanCategory{}, Deviations: []PlanDeviation{}}
	if gs.ThisTerm != nil {
		po.CreditLimit = gs.ThisTerm.CreditLimit
	}
	t := current
	for i := 1; i < gs.RemainingTerms; i++ {
		t = nextTerm(t)
		po.FutureTerms = append(po.FutureTerms, t)
	}
	last := current
	if n := len(po.FutureTerms); n > 0 {
		last = po.FutureTerms[n-1]
	}
	out := &Outlook{Reasons: []string{}}
	if gs.RemainingTerms > 0 {
		out.ExpectedGraduation = graduationMonth(last)
	}
	gs.Outlook = out
	gs.Plan = po

	registered := 0.0
	if gs.ThisTerm != nil {
		registered = gs.ThisTerm.RegisteredCredits
	}

	// 計画を学期ごとにまとめ、成績が出た学期・今期の計画は実績と照らし合わせる
	passed, failed := map[string]bool{}, map[string]bool{}
	if grades != nil {
		for _, g := range grades.Grades {
			if g.Passed {
				passed[subjectTitleKey(g.Title)] = true
			} else {
				failed[subjectTitleKey(g.Title)] = true
			}
		}
	}
	registeredTitles := map[string]bool{}
	if reg != nil {
		for _, l := range firstChoiceLectures(reg) {
			registeredTitles[subjectTitleKey(l.Title)] = true
		}
	}
	terms := map[string]*PlanTerm{}
	plannedByCat := map[string]float64{}
	for _, it := range plan {
		pt := terms[it.YearTerm]
		if pt == nil {
			pt = &PlanTerm{YearTerm: it.YearTerm, Past: it.YearTerm < current, Current: it.YearTerm == current}
			terms[it.YearTerm] = pt
		}
		pt.Items = append(pt.Items, it)
		pt.Credits += it.Credits
		title := subjectTitleKey(it.Title)
		switch {
		case pt.Past:
			if passed[title] {
				break
			}
			reason := "成績に見つかりません（未履修または未登録）"
			if failed[title] {
				reason = "不合格でした"
			}
			po.Deviations = append(po.Deviations, PlanDeviation{Item: it, Reason: reason})
		case pt.Current:
			if reg != nil && !registeredTitles[title] && !passed[title] {
				po.Deviations = append(po.Deviations, PlanDeviation{Item: it, Reason: "今期の履修登録（第1希望）にありません"})
			}
		default:
			po.TotalPlanned += it.Credits
			plannedByCat[it.Section+"\x00"+it.Category] += it.Credits
		}
	}
	for _, k := range sortedKeys(terms) {
		pt := terms[k]
		pt.OverLimit = !pt.Past && !pt.Current && po.CreditLimit > 0 && pt.Credits > po.CreditLimit
		po.Terms = append(po.Terms, *pt)
	}
	for key, ms := range subtotalMembers(gs.Requirements) {
		for _, m := range ms {
			plannedByCat[key] += plannedByCat[m]
		}
	}
	for _, r := range gs.Requirements {
		if (r.Kind != "item" && r.Kind != "subtotal") || r.Required == nil || *r.Required <= 0 {
			continue
		}
		key := r.Section + "\x00" + r.Name
		c := PlanCategory{Section: r.Section, Name: r.Name, Required: *r.Required, Registered: gs.ThisTerm.RegisteredFor(r.Section, r.Name), Planned: plannedByCat[key]}
		if r.Earned != nil {
			c.Earned = *r.Earned
		}
		c.ShortfallAfterPlan = math.Max(0, c.Required-c.Earned-c.Registered-c.Planned)
		po.Categories = append(po.Categories, c)
	}
	po.TotalShortfallAfterPlan = math.Max(0, gs.TotalShortfall-registered-po.TotalPlanned)
	po.Years = planYears(gs, grades, reg, plan, last)

	unmet := len(gs.Shortfalls) > 0
	switch {
	case gs.TotalRequired <= 0 && len(gs.Requirements) == 0:
		out.Status = "unknown"
		return
	case gs.TotalShortfall <= 0 && !unmet:
		out.Status = "met"
		return
	}

	for _, d := range po.Deviations {
		out.Reasons = append(out.Reasons, fmt.Sprintf("計画していた「%s」（%s）: %s。計画を見直してください。", d.Item.Title, d.Item.YearTerm, d.Reason))
	}
	if po.TotalPlanned > 0 {
		out.Basis = "plan"
		for _, pt := range po.Terms {
			if pt.OverLimit {
				out.Reasons = append(out.Reasons, fmt.Sprintf("%s の計画 %s 単位が履修上限 %s 単位を超えています。", pt.YearTerm, num(pt.Credits), num(po.CreditLimit)))
			}
		}
		for _, c := range po.Categories {
			if c.ShortfallAfterPlan > 0 {
				out.Reasons = append(out.Reasons, fmt.Sprintf("「%s」（%s）は計画どおりに取れても %s 単位足りません。", c.Name, c.Section, num(c.ShortfallAfterPlan)))
			}
		}
		if po.TotalShortfallAfterPlan > 0 {
			out.Reasons = append(out.Reasons, fmt.Sprintf("計画どおりに取れても、卒業まで合計 %s 単位足りません。", num(po.TotalShortfallAfterPlan)))
		}
		for _, it := range plan {
			if it.YearTerm > last {
				out.Reasons = append(out.Reasons, fmt.Sprintf("「%s」は卒業予定（%s）より後の %s に計画されています。", it.Title, out.ExpectedGraduation, it.YearTerm))
			}
		}
	} else {
		out.Basis = "pace"
		after := math.Max(0, gs.TotalShortfall-registered)
		left := len(po.FutureTerms)
		switch {
		case gs.RemainingTerms == 0:
			out.Status = "unknown"
			out.Reasons = append(out.Reasons, "年次が分からないため、卒業予定までに間に合うかは判定できません。")
			return
		case left == 0 && after > 0:
			out.Reasons = append(out.Reasons, fmt.Sprintf("今期が最後の学期ですが、今期の登録がすべて単位になっても %s 単位足りません。", num(after)))
		case po.CreditLimit > 0 && after > po.CreditLimit*float64(left):
			out.Reasons = append(out.Reasons, fmt.Sprintf("残り %s 単位を、あと %d 学期で取る必要があります。履修上限（各学期 %s 単位）いっぱいに取っても %s 単位までしか取れません。",
				num(after), left, num(po.CreditLimit), num(po.CreditLimit*float64(left))))
		}
	}
	if len(out.Reasons) > 0 {
		out.Status = "at_risk"
	} else {
		out.Status = "on_track"
	}
}

// planYears は入学から卒業予定（それより後の成績・計画があればそこ）までを年次・学期に並べる。
func planYears(gs *GraduationStatus, grades *kyomu.GradeList, reg *kyomu.Registration, plan []db.PlanItem, last string) []PlanYear {
	po := gs.Plan
	current := po.CurrentTerm
	curYear, _ := strconv.Atoi(current[:4])
	enter, years := enterYear(gs.Profile, curYear), 4
	if gs.Profile != nil && strings.Contains(gs.Profile.Department, "医学科") {
		years = 6
	}
	lastYear, _ := strconv.Atoi(last[:4])
	years = max(years, lastYear-enter+1, curYear-enter+1)
	if grades != nil {
		for _, g := range grades.Grades {
			if g.Year >= enter {
				years = max(years, g.Year-enter+1)
			}
		}
	}
	for _, it := range plan {
		if y, err := strconv.Atoi(it.YearTerm[:4]); err == nil && y >= enter {
			years = max(years, y-enter+1)
		}
	}

	terms := map[string]*PlanYearTerm{}
	var out []PlanYear
	for level := 1; level <= years; level++ {
		y := PlanYear{Level: level, Current: enter+level-1 == curYear}
		for _, label := range []string{"前期", "後期"} {
			key := fmt.Sprintf("%d%s", enter+level-1, label)
			t := PlanYearTerm{YearTerm: key, Label: label, Taken: []TakenCourse{}, Items: []db.PlanItem{}}
			switch {
			case key < current:
				t.State = "past"
			case key == current:
				t.State = "current"
			case key <= last:
				t.State = "future"
			default:
				t.State = "after"
			}
			y.Terms = append(y.Terms, t)
		}
		out = append(out, y)
	}
	for i := range out {
		for j := range out[i].Terms {
			terms[out[i].Terms[j].YearTerm] = &out[i].Terms[j]
		}
	}

	// 終わった学期は成績。入学年度より前の成績（認定など）は 1 年次の前期に入れる
	if grades != nil {
		for _, g := range grades.Grades {
			label := "後期"
			if strings.Contains(g.Term, "前") {
				label = "前期"
			}
			t := terms[fmt.Sprintf("%d%s", g.Year, label)]
			if t == nil {
				t = &out[0].Terms[0]
			}
			t.Taken = append(t.Taken, TakenCourse{LectureCode: g.LectureCode, Title: g.Title, Credits: g.Credits, Grade: g.Grade, Passed: g.Passed, Category: g.Category})
			if g.Passed {
				t.EarnedCredits += g.Credits
			}
		}
	}
	// 今期は履修登録（第1希望）
	if t := terms[current]; t != nil && reg != nil {
		unmapped := map[string]UnmappedLecture{}
		if gs.ThisTerm != nil {
			for _, u := range gs.ThisTerm.Unmapped {
				unmapped[u.LectureCode] = u
			}
		}
		graded := map[string]bool{}
		for _, c := range t.Taken {
			graded[subjectTitleKey(c.Title)] = true
		}
		for _, l := range firstChoiceLectures(reg) {
			if graded[subjectTitleKey(l.Title)] {
				continue // 今期のうちに成績が出た科目（集中講義など）
			}
			c := TakenCourse{LectureCode: l.LectureCode, Title: l.Title, Credits: l.Credits, Category: l.Category}
			if u, ok := unmapped[l.LectureCode]; ok && l.LectureCode != "" {
				c.Unmapped, c.Suggested = true, u.Suggested
			}
			t.Taken = append(t.Taken, c)
			t.RegisteredCredits += l.Credits
		}
	}
	for _, it := range plan {
		t := terms[it.YearTerm]
		if t == nil {
			continue
		}
		t.Items = append(t.Items, it)
		t.PlannedCredits += it.Credits
	}
	for i := range out {
		for j := range out[i].Terms {
			t := &out[i].Terms[j]
			switch t.State {
			case "past":
				out[i].Credits += t.EarnedCredits
			case "current":
				out[i].Credits += t.EarnedCredits + t.RegisteredCredits + t.PlannedCredits
			default:
				out[i].Credits += t.PlannedCredits
				t.OverLimit = po.CreditLimit > 0 && t.PlannedCredits > po.CreditLimit
			}
		}
	}
	return out
}

// enterYear は入学年度を返す。学籍情報に無ければ年次（例: ３年後期）と今の年度から逆算し、それも無ければ今の年度を 1 年次とする。
func enterYear(p *kyomu.StudentProfile, curYear int) int {
	if p == nil {
		return curYear
	}
	if p.EnterYear > 0 {
		return p.EnterYear
	}
	if m := gradeLevelRe.FindStringSubmatch(p.Grade); m != nil {
		level, _ := strconv.Atoi(normalizeDigit(m[1]))
		return curYear - level + 1
	}
	return curYear
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// thesisCheck は 3 年次の学生について、4 年次 4 月に卒業研究を登録する条件を満たせそうかを見る。
func (s *Service) thesisCheck(ctx context.Context, gs *GraduationStatus, prof *kyomu.StudentProfile, grades *kyomu.GradeList, reg *kyomu.Registration) *ThesisCheck {
	if prof == nil || s.University == nil {
		return nil
	}
	m := gradeLevelRe.FindStringSubmatch(prof.Grade)
	if m == nil || normalizeDigit(m[1]) != "3" {
		return nil
	}
	tc := &ThesisCheck{Earned: gs.TotalEarned, Status: "unknown", Conditions: []ThesisCondition{}, Notes: []string{}}
	if reg == nil {
		tc.Notes = append(tc.Notes, "今期の履修情報を取得できないため、卒業研究の登録条件は確認できませんでした。履修中か不明の科目を未修得とは判定していません。")
		return tc
	}
	// コースが無い学部（学科・プログラム単位）でも資料を見つけられるよう、所属名から「コース」などを外して探す
	affiliation := university.AffiliationKey(prof.Course, prof.Department)
	if affiliation == "" {
		tc.Notes = append(tc.Notes, "所属コース・学科が分からないため、卒業研究の登録条件は確認できませんでした。")
		return tc
	}
	rules, err := s.University.StudyRules(ctx, prof.EnterYear, prof.Faculty, affiliation, "卒業研究")
	if err != nil {
		tc.Notes = append(tc.Notes, "便覧を取得できませんでした: "+err.Error())
		return tc
	}
	courseDoc := ""
	for _, d := range rules.Documents {
		if university.CourseMatches(d.Title, affiliation) {
			tc.DocumentURL, courseDoc = d.URL, d.Title
			break
		}
	}
	if courseDoc == "" {
		tc.Notes = append(tc.Notes, "所属（"+affiliation+"）の便覧の資料が見つからないため、卒業研究の登録条件は確認できませんでした。")
		return tc
	}
	evaluateThesis(tc, gs, m[2], rules.ThesisRule, achievedTitles(gs, grades, reg))
	if tc.Status == "none" {
		tc.Notes = append(tc.Notes, "便覧（"+courseDoc+"）には、卒業研究を始めるための単位などの条件の記載が見つかりませんでした。研究室配属の方法はコースの案内を確認してください。")
	}
	return tc
}

// achievedTitles は 4 年次 4 月までに修得している見込みの科目名（修得済み・今期の登録・それまでの計画）。
func achievedTitles(gs *GraduationStatus, grades *kyomu.GradeList, reg *kyomu.Registration) map[string]bool {
	out := map[string]bool{}
	if grades != nil {
		for _, g := range grades.Grades {
			if g.Passed {
				out[subjectTitleKey(g.Title)] = true
			}
		}
	}
	if reg != nil {
		for _, l := range firstChoiceLectures(reg) {
			out[subjectTitleKey(l.Title)] = true
		}
	}
	return out
}

// evaluateThesis は便覧の条件（rule）に照らして、卒業研究の登録条件を満たせそうかを tc に書き込む。
// term は今の年次の前期・後期、achieved は判定時点までに修得の見込みがある科目名。
func evaluateThesis(tc *ThesisCheck, gs *GraduationStatus, term string, rule *university.ThesisRule, achieved map[string]bool) {
	if rule == nil {
		tc.Status = "none"
		return
	}
	tc.Mentioned = true
	tc.Rule = rule
	if !rule.HasConditions() {
		tc.Status = "unknown"
		tc.Notes = append(tc.Notes, "便覧には卒業研究の着手条件への言及がありますが、自動で判定できる形の条件は読み取れませんでした（条件の中身が別の資料にある場合もあります）。下の原文を確認し、学科・コースの案内で条件を確かめてください。")
		return
	}
	tc.Supported = true

	// 呼び出し元の科目名も登録・成績と同じ基準で照合する。
	normalized := map[string]bool{}
	for title, available := range achieved {
		if available {
			normalized[subjectTitleKey(title)] = true
		}
	}
	achieved = normalized

	// 判定時点は 4 年次の 4 月。そこまでの今期の登録と計画を見込みに入れる
	po := gs.Plan
	registered := 0.0
	if gs.ThisTerm != nil {
		registered = gs.ThisTerm.RegisteredCredits
	}
	planned := 0.0
	if po != nil {
		// 3年後期なら来期、3年前期なら再来期が 4 年次の 4 月
		tc.CheckTerm = nextTerm(po.CurrentTerm)
		if term == "前期" {
			tc.CheckTerm = nextTerm(tc.CheckTerm)
		}
		for _, pt := range po.Terms {
			if !pt.Past && !pt.Current && pt.YearTerm < tc.CheckTerm {
				planned += pt.Credits
				for _, it := range pt.Items {
					achieved[subjectTitleKey(it.Title)] = true
				}
			}
		}
	}
	tc.Projected = gs.TotalEarned + registered + planned
	tc.RemainingAtCheck = math.Max(0, gs.TotalShortfall-registered-planned)

	if need := rule.CreditsBySemesters[6]; need > 0 {
		tc.Conditions = append(tc.Conditions, ThesisCondition{Kind: "credits_min", Label: "取得単位数（6 個学期在学時）",
			OK: tc.Projected >= need, Value: tc.Projected, Target: need})
		tc.Notes = append(tc.Notes, "取得単位数には、成績表の卒業要件の修得単位の合計を使っています。卒業要件に数えない科目も含める場合は少し多くなります。")
	}
	if max := rule.MaxRemainingCredits; max > 0 {
		tc.Conditions = append(tc.Conditions, ThesisCondition{Kind: "remaining_max", Label: "卒業までに今後修得しなければならない単位数（卒研を含む）",
			OK: tc.RemainingAtCheck <= max, Value: tc.RemainingAtCheck, Target: max})
	}

	// 卒研・卒設以外で、判定時点までに修得の見込みがない専門必修
	if gs.RequiredCourses != nil && (rule.RequiresMajorRequired || rule.MaxMissingMajorRequired != nil) {
		var missing, missingByThird []string
		for _, r := range gs.RequiredCourses.Required {
			if r.Status == "passed" || r.Status == "enrolled" || achieved[subjectTitleKey(r.Title)] ||
				strings.Contains(r.Title, "卒業研究") || strings.Contains(r.Title, "卒業設計") {
				continue
			}
			missing = append(missing, r.Title)
			// セミナーは 4 年次の科目なので「3 年後学期まで」には入れない
			if !strings.Contains(r.Title, "セミナー") {
				missingByThird = append(missingByThird, r.Title)
			}
		}
		if rule.RequiresMajorRequired {
			tc.MissingRequired = missingByThird
			tc.Conditions = append(tc.Conditions, ThesisCondition{Kind: "all_required", Label: "3 年後学期までの専門必修科目をすべて修得（原則）",
				OK: len(missingByThird) == 0, Value: float64(len(missingByThird)), Items: missingByThird})
			tc.Notes = append(tc.Notes, "専門必修科目の配当年次はこの画面では判定していません。未修得として挙げた科目が 3 年後学期までの科目かは、便覧で確認してください。")
		}
		if max := rule.MaxMissingMajorRequired; max != nil {
			tc.MissingRequired = missing
			tc.Conditions = append(tc.Conditions, ThesisCondition{Kind: "missing_required_max", Label: fmt.Sprintf("卒研以外の専門必修の未修得が %d 科目以下", *max),
				OK: len(missing) <= *max, Value: float64(len(missing)), Target: float64(*max), Items: missing})
		}
		if term == "前期" {
			tc.Notes = append(tc.Notes, "3 年後期の科目は、計画に入れるか履修登録すると修得の見込みとして数えます。")
		}
	}
	for _, anyOf := range rule.RequiredCourses {
		ok := false
		for _, c := range anyOf {
			if achieved[subjectTitleKey(c)] {
				ok = true
			}
		}
		label := strings.Join(anyOf, " または ") + " を修得"
		cond := ThesisCondition{Kind: "courses", Label: label, OK: ok}
		if !ok {
			cond.Items = anyOf
		}
		tc.Conditions = append(tc.Conditions, cond)
	}

	tc.Status = "likely"
	for _, c := range tc.Conditions {
		if !c.OK {
			tc.Status = "at_risk"
		}
	}
	tc.Notes = append(tc.Notes, "便覧にあるのは卒業研究の登録条件です。研究室の仮配属の手続きや条件が別に定められている場合は、コースの案内に従ってください。")
}
