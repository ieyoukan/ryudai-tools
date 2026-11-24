package academic

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
)

// GraduationStatus は卒業要件に対する現状と、今期の履修登録を合わせた見通し。
//
// 区分ごとの必須単位・修得単位は教務システムの個別成績表（PDF）の値そのもの。
// 今期の登録科目を区分へ割り当てる部分（Projection）は区分名からの推定で、学生便覧での確認が前提。
type GraduationStatus struct {
	RequiredCourses *RequiredCheck        `json:"required_courses,omitempty"`
	Curriculum      *CurriculumCheck      `json:"curriculum,omitempty"`
	Profile         *kyomu.StudentProfile `json:"profile,omitempty"`
	GPA             *float64              `json:"gpa,omitempty"`
	TotalRequired   float64               `json:"total_required"`  // 卒業に必要な単位（【合計】の必須）
	TotalEarned     float64               `json:"total_earned"`    // 修得済み単位（【合計】の修得）
	TotalShortfall  float64               `json:"total_shortfall"` // あと何単位必要か
	// Shortfalls は必須単位に足りていない区分・小計（小さい単位の区分から順）。
	Shortfalls []kyomu.Requirement `json:"shortfalls"`
	// Requirements は個別成績表の集計表の全行。
	Requirements []kyomu.Requirement `json:"requirements"`
	ThisTerm     *TermPlan           `json:"this_term,omitempty"`
	// RemainingTerms は今期を含めた残りの学期数（4 年制として年次から推定）。
	RemainingTerms int `json:"remaining_terms,omitempty"`
	// PerTermAfterThis は今期の登録がすべて単位になった場合に、残りの学期で 1 学期あたり必要な単位数。
	PerTermAfterThis *float64       `json:"per_term_after_this,omitempty"`
	FailedCourses    []FailedCourse `json:"failed_courses,omitempty"` // 不合格で、まだ同じ科目を修得していないもの
	GradeCounts      map[string]int `json:"grade_counts"`
	RemoteCredits    float64        `json:"remote_credits"`
	OtherUnivCredits float64        `json:"other_univ_credits"`
	Grades           []kyomu.Grade  `json:"grades,omitempty"` // include_grades のときだけ
	Notes            []string       `json:"notes"`
	// Outlook は卒業予定までに要件を満たせるかの予測、Plan は履修計画を当てはめた結果。
	Outlook *Outlook     `json:"outlook,omitempty"`
	Plan    *PlanOutlook `json:"plan,omitempty"`
	// Thesis は 3 年次にだけ付く、卒業研究Ⅰの登録条件（研究室配属の前提）の見込み。
	Thesis *ThesisCheck `json:"thesis,omitempty"`
	// CategoryItems は成績・今期の登録の科目区分（例: 専門教育科目（知能情報コア））→ 個別成績表の区分名（例: 知能情報コア）。
	// 画面がグラフを区分ごとに色分けするのに使う。対応が分からない科目区分は入らない。
	CategoryItems map[string]string `json:"category_items"`
}

// FailedCourse は未修得の不合格科目。CountsTowardRequirement は、その科目区分に必須単位があるか
// （false なら教職など卒業要件の必須区分外。nil は区分を特定できなかった）。
type FailedCourse struct {
	kyomu.Grade             `tstype:",extends"`
	CountsTowardRequirement *bool `json:"counts_toward_requirement,omitempty"`
}

// TermPlan は今期の履修登録（第1希望）を卒業要件の区分に当てはめたもの。
type TermPlan struct {
	YearTerm          string            `json:"year_term"`
	RegistrationOpen  bool              `json:"registration_open"`
	RegisteredCredits float64           `json:"registered_credits"` // 今期登録した単位（第1希望の合計）
	CreditLimit       float64           `json:"credit_limit,omitempty"`
	Projection        []ProjectedItem   `json:"projection"`         // 不足のある区分が、今期の登録で埋まるか
	Unmapped          []UnmappedLecture `json:"unmapped,omitempty"` // 区分を特定できなかった登録科目
	Inferred          []InferredLecture `json:"inferred,omitempty"` // 区分を推定・保存済みの値で補った登録科目
	// registered は区分（item）と構成の分かる小計ごとの今期の登録単位（キーは 区分の大区分 + "\x00" + 名前）。
	registered map[string]float64
}

// RegisteredFor は区分（item）または小計に今期登録している単位を返す。不足の無い区分も含む。
func (tp *TermPlan) RegisteredFor(section, name string) float64 {
	if tp == nil {
		return 0
	}
	if tp.registered != nil {
		return tp.registered[section+"\x00"+name]
	}
	for _, p := range tp.Projection {
		if p.Section == section && p.Name == name {
			return p.RegisteredNow
		}
	}
	return 0
}

// ProjectedItem は不足のある区分 1 つについての見通し。
type ProjectedItem struct {
	Section          string   `json:"section"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Required         float64  `json:"required"`
	Earned           float64  `json:"earned"`
	RegisteredNow    float64  `json:"registered_now"`  // 今期この区分に登録している単位（推定）
	ShortfallAfter   float64  `json:"shortfall_after"` // 今期の登録がすべて単位になった後の不足
	RegisteredTitles []string `json:"registered_titles,omitempty"`
}

// UnmappedLecture は区分に割り当てられなかった今期の登録科目。
type UnmappedLecture struct {
	// Suggested は利用者が区分を選ぶときの候補（個別成績表の区分名）。
	Suggested   string  `json:"suggested,omitempty"`
	LectureCode string  `json:"lecture_code,omitempty"`
	Title       string  `json:"title"`
	Category    string  `json:"category,omitempty"`
	Credits     float64 `json:"credits"`
}

// InferredLecture は、教務システムの画面に区分が無かったため推定・保存済みの区分を使った今期の登録科目。
type InferredLecture struct {
	LectureCode string `json:"lecture_code,omitempty"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	Source      string `json:"source"` // saved（保存済み）/ grades（同じ科目の成績）/ series（番号違いの科目の成績）
	Basis       string `json:"basis,omitempty"`
}

// GraduationStatus は成績明細・個別成績表・今期の履修登録をまとめて、卒業要件の見通しを返す。
func (s *Service) GraduationStatus(username string, includeGrades bool) (*GraduationStatus, error) {
	return s.GraduationStatusContext(context.Background(), username, includeGrades)
}

func (s *Service) GraduationStatusContext(ctx context.Context, username string, includeGrades bool) (*GraduationStatus, error) {
	gs, _, _, err := s.graduation(ctx, username, includeGrades)
	return gs, err
}

// gradeSnapshot は教務システムから読んだ成績・個別成績表・今期の登録・所属。
// 履修計画に科目を足したり消したりするたびに 10〜20 秒かけて読み直さないよう、短い間だけ使い回す。
type gradeSnapshot struct {
	grades *kyomu.GradeList
	report *kyomu.GradeReport
	reg    *kyomu.Registration
	regErr error
	prof   *kyomu.StudentProfile
}

// registration は今期の登録の複製を返す（区分の補完で書き換えるため、使い回す元は変えない）。
func (g *gradeSnapshot) registration() *kyomu.Registration {
	if g.reg == nil {
		return nil
	}
	r := *g.reg
	r.Lectures = append([]kyomu.RegisteredLecture(nil), g.reg.Lectures...)
	return &r
}

func (s *Service) gradeSnapshot(username string) (*gradeSnapshot, error) {
	key := strings.ToLower(username)
	if v, ok := s.snapshotCache.get(key); ok {
		return v, nil
	}
	snap := &gradeSnapshot{}
	err := s.withUser(username, func(c *kyomu.Client) error {
		var err error
		if snap.grades, err = c.FetchGrades(); err != nil {
			return err
		}
		if snap.report, err = c.FetchGradeReport(); err != nil {
			return err
		}
		snap.reg, snap.regErr = c.FetchRegistration()
		if snap.regErr == kyomu.ErrSessionExpired {
			return snap.regErr
		}
		snap.prof = s.profile(username, c)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.snapshotCache.set(key, snap)
	return snap, nil
}

// graduation は卒業要件の見通しと、その元にした成績・今期の登録（区分を補ったもの）を返す。
func (s *Service) graduation(ctx context.Context, username string, includeGrades bool) (*GraduationStatus, *kyomu.GradeList, *kyomu.Registration, error) {
	snap, err := s.gradeSnapshot(username)
	if err != nil {
		return nil, nil, nil, err
	}
	grades, report, reg, prof := snap.grades, snap.report, snap.registration(), snap.prof
	s.fillLectureCategories(username, reg, grades)
	gs := buildGraduationStatus(grades, report, reg, prof)
	gs.Curriculum = s.curriculumCheck(ctx, prof, grades, reg, nil)
	if prof != nil {
		gs.RequiredCourses = s.requiredCheck(ctx, prof, grades, reg, nil, prof.EnterYear)
	}
	buildOutlook(gs, grades, reg, s.planItems(username))
	gs.Thesis = s.thesisCheck(ctx, gs, prof, grades, reg)
	gs.CategoryItems = categoryItems(grades, reg, gs.Requirements)
	if snap.regErr != nil {
		gs.Notes = append(gs.Notes, "今期の履修登録を取得できなかったため、今期の見通しは含めていません: "+snap.regErr.Error())
	}
	if includeGrades {
		gs.Grades = grades.Grades
	}
	return gs, grades, reg, nil
}

func buildGraduationStatus(grades *kyomu.GradeList, report *kyomu.GradeReport, reg *kyomu.Registration, prof *kyomu.StudentProfile) *GraduationStatus {
	gs := &GraduationStatus{
		Profile:          prof,
		GPA:              report.GPA,
		Requirements:     report.Requirements,
		Shortfalls:       []kyomu.Requirement{},
		GradeCounts:      map[string]int{},
		RemoteCredits:    grades.RemoteCredits,
		OtherUnivCredits: grades.OtherUnivCredits,
		Notes:            []string{},
	}
	for _, r := range report.Requirements {
		if r.Kind == "total" && r.Name == "合計" && r.Required != nil {
			gs.TotalRequired = *r.Required
			if r.Earned != nil {
				gs.TotalEarned = *r.Earned
			}
			gs.TotalShortfall = math.Max(0, gs.TotalRequired-gs.TotalEarned)
		}
		if r.Shortfall != nil && *r.Shortfall > 0 && r.Kind != "info" {
			gs.Shortfalls = append(gs.Shortfalls, r)
		}
	}
	// 区分（item）→ 小計 → 合計 の順に並べ、細かい不足から見えるようにする
	kindOrder := map[string]int{"item": 0, "subtotal": 1, "total": 2}
	sort.SliceStable(gs.Shortfalls, func(i, j int) bool {
		return kindOrder[gs.Shortfalls[i].Kind] < kindOrder[gs.Shortfalls[j].Kind]
	})

	// 不合格のうち、同じ科目名をあとで修得していないもの
	passedTitles := map[string]bool{}
	for _, g := range grades.Grades {
		gs.GradeCounts[g.Grade]++
		if g.Passed {
			passedTitles[subjectTitleKey(g.Title)] = true
		}
	}
	items := requirementItems(report.Requirements)
	failedRequired := 0
	for _, g := range grades.Grades {
		if g.Passed || passedTitles[subjectTitleKey(g.Title)] {
			continue
		}
		fc := FailedCourse{Grade: g}
		if it, ok := items[normalizeCategory(g.Category)]; ok {
			counts := it.Required != nil
			fc.CountsTowardRequirement = &counts
		}
		if fc.CountsTowardRequirement == nil || *fc.CountsTowardRequirement {
			failedRequired++
		}
		gs.FailedCourses = append(gs.FailedCourses, fc)
	}

	if prof != nil {
		gs.RemainingTerms = remainingTerms(prof)
	}
	if reg != nil {
		gs.ThisTerm = projectTerm(reg, report.Requirements, gs.Shortfalls, categoryAliases(grades, report.Requirements))
		after := gs.TotalShortfall - gs.ThisTerm.RegisteredCredits
		if prof != nil {
			if n := gs.RemainingTerms; n > 0 {
				if n > 1 {
					v := math.Ceil(math.Max(0, after)/float64(n-1)*10) / 10
					gs.PerTermAfterThis = &v
				}
			}
		}
		gs.Notes = append(gs.Notes, fmt.Sprintf("卒業まであと %s 単位（%s / %s）。今期の登録 %s 単位がすべて単位になると、残り %s 単位。",
			num(gs.TotalShortfall), num(gs.TotalEarned), num(gs.TotalRequired), num(gs.ThisTerm.RegisteredCredits), num(math.Max(0, after))))
		for _, p := range gs.ThisTerm.Projection {
			if p.Kind != "total" && p.ShortfallAfter > 0 {
				gs.Notes = append(gs.Notes, fmt.Sprintf("「%s」（%s）は今期の登録だけでは %s 単位足りません（必須 %s / 修得 %s / 今期 %s）。",
					p.Name, p.Section, num(p.ShortfallAfter), num(p.Required), num(p.Earned), num(p.RegisteredNow)))
			}
		}
	}
	if failedRequired > 0 {
		gs.Notes = append(gs.Notes, fmt.Sprintf("必須単位のある区分（または区分不明）で、不合格のまま修得していない科目が %d 件あります。必修科目なら再履修が必要です。", failedRequired))
	} else if len(gs.FailedCourses) > 0 {
		gs.Notes = append(gs.Notes, "不合格のまま修得していない科目はありますが、いずれも卒業要件の必須区分の外です（教職など）。")
	}
	gs.Notes = append(gs.Notes,
		"区分ごとの必須単位・修得単位は教務システムの個別成績表の値です。どの科目が必修か（科目単位の必修）は学生便覧で確認してください。",
		"今期の登録科目を区分に当てはめた部分は区分名からの推定です。")
	return gs
}

// categoryAliases は、成績の科目区分の名前が個別成績表の区分名と違う場合（例: 成績「専門教育科目（知能情報）」と
// 個別成績表「知能情報アドバンスト」）の対応を、修得単位の合計が一致することから見つける。
// 名前で対応しない成績の区分ごとに、名前で対応しない個別成績表の区分のうち修得単位が同じものが 1 つだけなら対応とみなす。
func categoryAliases(grades *kyomu.GradeList, reqs []kyomu.Requirement) map[string]string {
	out := map[string]string{}
	if grades == nil {
		return out
	}
	items := requirementItems(reqs)
	sums := map[string]float64{}
	for _, g := range grades.Grades {
		if g.Passed && g.Category != "" {
			sums[normalizeCategory(g.Category)] += g.Credits
		}
	}
	matched := map[string]bool{}
	for key := range sums {
		if _, ok := items[key]; ok {
			matched[key] = true
		}
	}
	for key, sum := range sums {
		if matched[key] || sum == 0 {
			continue
		}
		candidate, n := "", 0
		for name, it := range items {
			if matched[name] || it.Earned == nil || *it.Earned != sum {
				continue
			}
			candidate, n = name, n+1
		}
		if n == 1 {
			out[key] = candidate
		}
	}
	return out
}

// categoryItems は成績・今期の登録に出てくる科目区分を、個別成績表の区分名に対応づける。
func categoryItems(grades *kyomu.GradeList, reg *kyomu.Registration, reqs []kyomu.Requirement) map[string]string {
	out := map[string]string{}
	items := requirementItems(reqs)
	aliases := categoryAliases(grades, reqs)
	add := func(raw string) {
		key := normalizeCategory(raw)
		if a, ok := aliases[key]; ok {
			key = a
		}
		if it, ok := items[key]; ok && raw != "" {
			out[raw] = it.Name
		}
	}
	if grades != nil {
		for _, g := range grades.Grades {
			add(g.Category)
		}
	}
	if reg != nil {
		for _, l := range reg.Lectures {
			add(l.Category)
		}
	}
	return out
}

// requirementItems は集計表の区分（item）を、正規化した名前で引けるようにする。
func requirementItems(reqs []kyomu.Requirement) map[string]kyomu.Requirement {
	out := map[string]kyomu.Requirement{}
	for _, r := range reqs {
		if r.Kind == "item" {
			out[normalizeCategory(r.Name)] = r
		}
	}
	return out
}

// sectionOfCategory は区分が見つからない科目を、科目区分の書き出しから大区分に寄せる。
func sectionOfCategory(category string) string {
	switch {
	case strings.Contains(category, "専門基礎"):
		return "専門基礎教育"
	case strings.HasPrefix(category, "共通教育"):
		return "共通教育"
	case strings.HasPrefix(category, "専門教育"), strings.HasPrefix(category, "選択科目"):
		return "専門教育"
	}
	return ""
}

// sectionTotals は【】の合計行と大区分の対応。
var sectionTotals = map[string]string{"共通計": "共通教育", "専門計": "専門教育", "専門基礎教育": "専門基礎教育"}

// projectTerm は今期の登録（第1希望）を卒業要件の区分に当てはめ、不足のある区分の見通しを出す。
func projectTerm(reg *kyomu.Registration, all, shortfalls []kyomu.Requirement, aliases map[string]string) *TermPlan {
	tp := &TermPlan{YearTerm: reg.YearTerm, RegistrationOpen: reg.Open, Projection: []ProjectedItem{}}
	if reg.Credits != nil {
		tp.CreditLimit = reg.Credits.Limit
	}
	// 区分（item）の名前 → 今期の登録
	type bucket struct {
		credits float64
		titles  []string
	}
	byReq := map[string]*bucket{}
	bySection := map[string]*bucket{}
	add := func(m map[string]*bucket, key string, l kyomu.RegisteredLecture) {
		b := m[key]
		if b == nil {
			b = &bucket{}
			m[key] = b
		}
		b.credits += l.Credits
		b.titles = append(b.titles, l.Title)
	}
	items := requirementItems(all)
	for _, l := range firstChoiceLectures(reg) {
		if l.CategorySource != "" && l.Category != "" {
			tp.Inferred = append(tp.Inferred, InferredLecture{LectureCode: l.LectureCode, Title: l.Title, Category: l.Category, Source: l.CategorySource, Basis: l.CategoryBasis})
		}
		tp.RegisteredCredits += l.Credits
		key := normalizeCategory(l.Category)
		if a, ok := aliases[key]; ok {
			key = a
		}
		section := ""
		if it, ok := items[key]; ok && key != "" {
			add(byReq, key, l)
			section = it.Section
		} else {
			u := UnmappedLecture{LectureCode: l.LectureCode, Title: l.Title, Category: l.Category, Credits: l.Credits}
			if s := normalizeCategory(l.SuggestedCategory); s != "" {
				if a, ok := aliases[s]; ok {
					s = a
				}
				if it, ok := items[s]; ok {
					u.Suggested = it.Name
				}
			}
			tp.Unmapped = append(tp.Unmapped, u)
			section = sectionOfCategory(l.Category)
		}
		if section != "" {
			add(bySection, section, l)
		}
	}
	tp.registered = map[string]float64{}
	titles := map[string][]string{}
	for _, r := range all {
		if b := byReq[normalizeCategory(r.Name)]; r.Kind == "item" && b != nil {
			tp.registered[r.Section+"\x00"+r.Name] = b.credits
			titles[r.Section+"\x00"+r.Name] = b.titles
		}
	}
	members := subtotalMembers(all)
	for key, ms := range members {
		for _, m := range ms {
			tp.registered[key] += tp.registered[m]
			titles[key] = append(titles[key], titles[m]...)
		}
	}
	for _, r := range shortfalls {
		if r.Required == nil {
			continue
		}
		p := ProjectedItem{Section: r.Section, Name: r.Name, Kind: r.Kind, Required: *r.Required}
		if r.Earned != nil {
			p.Earned = *r.Earned
		}
		key := r.Section + "\x00" + r.Name
		switch r.Kind {
		case "item":
			p.RegisteredNow, p.RegisteredTitles = tp.registered[key], titles[key]
		case "subtotal":
			if _, ok := members[key]; !ok {
				continue // 構成が分からない小計は見通しを出さない
			}
			p.RegisteredNow, p.RegisteredTitles = tp.registered[key], titles[key]
		case "total":
			if r.Name == "合計" {
				p.RegisteredNow = tp.RegisteredCredits
			} else if b := bySection[sectionTotals[r.Name]]; b != nil {
				p.RegisteredNow = b.credits
			}
		default:
			continue
		}
		p.ShortfallAfter = math.Max(0, p.Required-p.Earned-p.RegisteredNow)
		tp.Projection = append(tp.Projection, p)
	}
	return tp
}

// subtotalMembers は個別成績表の小計（subtotal）ごとに、含まれる区分（item）を返す（キーは 大区分 + "\x00" + 名前）。
// 様式に小計の構成は書かれていないので、小計の直前の行から上へたどり、修得単位の和が小計の修得単位に初めて一致するまでを
// その小計の構成とみなす（下位の小計はその構成ごと 1 行として数える）。例: 知能情報コースの「（数情計）」は
// 知能情報関連 + 知能情報アドバンスト、「（数情等計）」は 教職 + 自由 + （数情計） + 数学基礎（選択） + 工学融合（選択）。
// 修得単位が 0 の小計や、和が一致しない小計は構成が分からないとして含めない。
func subtotalMembers(reqs []kyomu.Requirement) map[string][]string {
	out := map[string][]string{}
	start := map[int]int{} // 構成の分かった小計の行 → 構成の先頭の行
	for i, r := range reqs {
		if r.Kind != "subtotal" || r.Earned == nil || *r.Earned <= 0 {
			continue
		}
		var members []string
		sum, ok := 0.0, false
		for j := i - 1; j >= 0 && reqs[j].Section == r.Section && !ok; j-- {
			row := reqs[j]
			switch row.Kind {
			case "info":
				continue
			case "item":
				members = append(members, row.Section+"\x00"+row.Name)
			case "subtotal":
				s, found := start[j]
				if !found {
					j = -1 // 構成の分からない小計をまたがない
					continue
				}
				members = append(members, out[row.Section+"\x00"+row.Name]...)
				j = s
			default:
				j = -1
				continue
			}
			if row.Earned != nil {
				sum += *row.Earned
			}
			if math.Abs(sum-*r.Earned) < 1e-9 {
				ok = true
				start[i] = j
			} else if sum > *r.Earned {
				break
			}
		}
		if ok {
			out[r.Section+"\x00"+r.Name] = members
		}
	}
	return out
}

var (
	categoryParen  = regexp.MustCompile(`[（(]([^）)]*)[）)]`)
	categoryPrefix = regexp.MustCompile(`^(共通教育等?科目|専門教育科目|専門基礎教育科目|選択科目|共通教育|専門教育)`)
	categoryAlias  = map[string]string{"その他の外国語": "その他"}
)

// normalizeCategory は「専門教育科目（研究・実験）」「研究実験」「選択科目（工学融合科目）」「工学融合（選択）」
// のような表記ゆれを揃える（区分の突き合わせ用）。
func normalizeCategory(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "［］[]")
	if m := categoryParen.FindStringSubmatch(s); m != nil && (categoryPrefix.MatchString(s) || strings.Index(s, m[0]) == 0) {
		s = m[1] // 「専門教育科目（研究・実験）」→「研究・実験」、「（教職）教職に関する科目」→「教職」
	}
	s = strings.NewReplacer("（選択）", "", "(選択)", "", "・", "", " ", "", "　", "").Replace(s)
	s = strings.TrimSuffix(s, "科目")
	s = strings.TrimSuffix(s, "系")
	if a, ok := categoryAlias[s]; ok {
		s = a
	}
	return s
}

var titleSuffix = regexp.MustCompile(`\s*([0-9０-９]+組|\(振替\)|（振替）)$`)

// baseTitle は「大学英語 01組」のような組番号を除いた科目名（再履修の判定用）。
func baseTitle(t string) string {
	for {
		n := titleSuffix.ReplaceAllString(strings.TrimSpace(t), "")
		if n == t {
			return t
		}
		t = n
	}
}

var gradeLevelRe = regexp.MustCompile(`([1-6１-６])\s*年\s*(前期|後期)`)

// remainingTerms は年次（例: ３年後期）から、今期を含めた残り学期数を返す（4 年制、医学科は 6 年制として推定）。
func remainingTerms(p *kyomu.StudentProfile) int {
	m := gradeLevelRe.FindStringSubmatch(p.Grade)
	if m == nil {
		return 0
	}
	year, _ := strconv.Atoi(normalizeDigit(m[1]))
	years := 4
	if strings.Contains(p.Department, "医学科") {
		years = 6
	}
	left := (years-year)*2 + 1
	if m[2] == "前期" {
		left++
	}
	if left < 1 {
		return 1
	}
	return left
}

func normalizeDigit(s string) string {
	r := []rune(s)[0]
	if r >= '０' && r <= '９' {
		return string(r - '０' + '0')
	}
	return s
}
