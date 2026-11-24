package academic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

// 区分ごとに出す候補の数。
const suggestionsPerGroup = 6

// commonFaculty はシラバス検索で共通教育の科目を開講している「学部」。
const commonFaculty = "共通教育等科目"

// PlanSuggestions は、ある学期の計画に足すと卒業要件の不足を埋められそうな科目（シラバスから選んだもの）。
type PlanSuggestions struct {
	YearTerm string `json:"year_term"`
	// SyllabusYear・SyllabusTerm は候補を探したシラバスの年度・学期（来年度以降のシラバスはまだ無いので今年度で代用する）。
	SyllabusYear int    `json:"syllabus_year"`
	SyllabusTerm string `json:"syllabus_term"`
	// RemainingCredits はこの学期に履修上限まであと何単位入れられるか（上限が分からなければ nil）。
	RemainingCredits *float64          `json:"remaining_credits,omitempty"`
	Groups           []SuggestionGroup `json:"groups"`
	Notes            []string          `json:"notes"`
}

// SuggestionGroup は不足のある区分 1 つと、そこに数えられそうな科目。
type SuggestionGroup struct {
	Section   string           `json:"section"`
	Name      string           `json:"name"`
	Shortfall float64          `json:"shortfall"` // 今期の登録と計画がすべて単位になった後の不足
	Items     []PlanSuggestion `json:"items"`
}

// PlanSuggestion は候補の科目 1 つ。区分は成績の科目区分から推定したもの。
type PlanSuggestion struct {
	Title         string  `json:"title"`
	LectureCode   string  `json:"lecture_code"`
	NumberingCode string  `json:"numbering_code,omitempty"`
	Credits       float64 `json:"credits"`
	DayPeriod     string  `json:"day_period,omitempty"`
	Instructors   string  `json:"instructors,omitempty"`
	URL           string  `json:"url,omitempty"`
	Required      bool    `json:"required,omitempty"` // 便覧の必修で、まだ修得していない
	Retake        bool    `json:"retake,omitempty"`   // 不合格のまま修得していない科目
	Basis         string  `json:"basis"`              // 区分の根拠
	// Section・Category は計画に入れるときの区分（個別成績表の大区分・区分名）。
	Section  string `json:"section"`
	Category string `json:"category"`
	year     int
	distance int
}

// PlanSuggestions は yearTerm（今期または卒業予定までの学期）の計画に足す候補を、シラバスと成績から選ぶ。
//
// 候補は、学生の学部と共通教育のその学期（前学期・後学期）の開講科目のうち、まだ修得・登録・計画していないもの。
// シラバスには卒業要件の区分が書かれていないので、同じ科目（講義コードの先頭 6 桁）の成績、無ければ同じ開講部署で
// 講義コードが一番近い成績の科目区分を使い、計画後も不足のある区分に数えられそうなものだけを残す。
func (s *Service) PlanSuggestions(ctx context.Context, username, yearTerm string) (*PlanSuggestions, error) {
	gs, grades, reg, err := s.graduation(ctx, username, false)
	if err != nil {
		return nil, err
	}
	po := gs.Plan
	if po == nil || (yearTerm != po.CurrentTerm && !contains(po.FutureTerms, yearTerm)) {
		return nil, errors.New("今期か、卒業予定までの学期を選んでください")
	}
	syllabusTerm := "後学期"
	if strings.Contains(yearTerm, "前") {
		syllabusTerm = "前学期"
	}
	out := &PlanSuggestions{YearTerm: yearTerm, SyllabusYear: kyomu.AcademicYear(time.Now()), SyllabusTerm: syllabusTerm, Groups: []SuggestionGroup{}, Notes: []string{}}
	if termYear, _ := strconv.Atoi(yearTerm[:4]); termYear > out.SyllabusYear {
		out.Notes = append(out.Notes, fmt.Sprintf("%d 年度のシラバスはまだ無いため、%d 年度の%sのシラバスから選んでいます。同じ学期に開講されるかは、その年度のシラバスで確認してください。", termYear, out.SyllabusYear, syllabusTerm))
	}
	if po.CreditLimit > 0 {
		used := 0.0
		for _, y := range po.Years {
			for _, t := range y.Terms {
				if t.YearTerm == yearTerm {
					used = t.RegisteredCredits + t.PlannedCredits
				}
			}
		}
		left := po.CreditLimit - used
		out.RemainingCredits = &left
	}

	groups, groupOf := suggestionGroups(gs)
	if len(groups) == 0 {
		out.Notes = append(out.Notes, "今期の登録と計画がすべて単位になれば、必須単位のある区分の不足はありません。")
		return out, nil
	}

	faculties := []string{commonFaculty}
	if gs.Profile != nil && gs.Profile.Faculty != "" {
		faculties = append([]string{gs.Profile.Faculty}, faculties...)
	}
	var offered []kyomu.SyllabusSummary
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, f := range faculties {
		wg.Add(1)
		go func(faculty string) {
			defer wg.Done()
			res, err := s.searchCached(kyomu.SearchParams{Year: out.SyllabusYear, Term: syllabusTerm, Faculty: faculty})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Notes = append(out.Notes, fmt.Sprintf("%s のシラバスを検索できませんでした: %v", faculty, err))
				return
			}
			offered = append(offered, res...)
		}(f)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	picked := pickSuggestions(gs, grades, reg, offered, groupOf)
	for i := range groups {
		g := &groups[i]
		cands := picked[i]
		sort.SliceStable(cands, func(a, b int) bool {
			x, y := cands[a], cands[b]
			if x.Required != y.Required {
				return x.Required
			}
			if x.Retake != y.Retake {
				return x.Retake
			}
			return x.distance < y.distance
		})
		// 単位数はシラバス詳細にしか無いので、上位の候補だけ詳細を読む（読めなかったものは外す）
		if len(cands) > suggestionsPerGroup+2 {
			cands = cands[:suggestionsPerGroup+2]
		}
		s.fillSuggestionCredits(cands)
		for _, c := range cands {
			if c.Credits > 0 && len(g.Items) < suggestionsPerGroup {
				g.Items = append(g.Items, c)
			}
		}
	}
	for _, g := range groups {
		if len(g.Items) > 0 {
			out.Groups = append(out.Groups, g)
		}
	}
	if len(out.Groups) < len(groups) {
		var names []string
		for _, g := range groups {
			if len(g.Items) == 0 {
				names = append(names, "「"+g.Name+"」")
			}
		}
		out.Notes = append(out.Notes, strings.Join(names, "")+"は、この学期のシラバスから候補を見つけられませんでした。シラバス検索で探して、下の「一覧に無い科目を追加」から入れてください。")
	}
	out.Notes = append(out.Notes, "区分は、同じ科目や講義コードの近い科目の成績から推定しています。卒業要件にどう数えるかは学生便覧で確認してください。")
	return out, nil
}

// suggestionGroups は計画後も不足のある区分を返す。groupOf は区分（item）の 大区分 + "\\x00" + 名前 → 何番目のグループか。
// 小計（例: （数情等計））が足りないときは、その小計に含まれる区分の科目を小計のグループに入れる。
func suggestionGroups(gs *GraduationStatus) ([]SuggestionGroup, map[string]int) {
	var groups []SuggestionGroup
	groupOf := map[string]int{}
	members := subtotalMembers(gs.Requirements)
	kinds := map[string]string{}
	for _, r := range gs.Requirements {
		kinds[r.Section+"\x00"+r.Name] = r.Kind
	}
	// 細かい区分を先に割り当て、小計にはまだどこにも入っていない区分だけを入れる
	for _, pass := range []string{"item", "subtotal"} {
		for _, c := range gs.Plan.Categories {
			key := c.Section + "\x00" + c.Name
			if c.ShortfallAfterPlan <= 0 || kinds[key] != pass {
				continue
			}
			keys := []string{key}
			if pass == "subtotal" {
				keys = members[key]
			}
			idx := len(groups)
			added := false
			for _, k := range keys {
				if _, ok := groupOf[k]; !ok {
					groupOf[k] = idx
					added = true
				}
			}
			if added || pass == "item" {
				groups = append(groups, SuggestionGroup{Section: c.Section, Name: c.Name, Shortfall: c.ShortfallAfterPlan, Items: []PlanSuggestion{}})
			}
		}
	}
	return groups, groupOf
}

// pickSuggestions は開講科目を、区分を推定してグループごとに振り分ける（グループの番号 → 候補）。
func pickSuggestions(gs *GraduationStatus, grades *kyomu.GradeList, reg *kyomu.Registration, offered []kyomu.SyllabusSummary, groupOf map[string]int) map[int][]PlanSuggestion {
	out := map[int][]PlanSuggestion{}
	items := requirementItems(gs.Requirements)
	aliases := categoryAliases(grades, gs.Requirements)

	taken := map[string]bool{} // 修得済み・今期の登録・計画の科目名
	type graded struct {
		code     int
		category string
		title    string
		passed   bool
	}
	var history []graded
	bySubject := map[string]graded{} // 講義コードの先頭 6 桁 → 成績（修得したものを優先）
	if grades != nil {
		for _, g := range grades.Grades {
			if g.Passed {
				taken[subjectNameKey(baseTitle(g.Title))] = true
			}
			n, err := strconv.Atoi(prefixDigits(g.LectureCode, 6))
			if err != nil || g.Category == "" {
				continue
			}
			h := graded{code: n, category: g.Category, title: baseTitle(g.Title), passed: g.Passed}
			history = append(history, h)
			if prev, ok := bySubject[prefixDigits(g.LectureCode, 6)]; !ok || !prev.passed {
				bySubject[prefixDigits(g.LectureCode, 6)] = h
			}
		}
	}
	if reg != nil {
		for _, l := range firstChoiceLectures(reg) {
			taken[subjectNameKey(baseTitle(l.Title))] = true
		}
	}
	for _, y := range gs.Plan.Years {
		for _, t := range y.Terms {
			for _, it := range t.Items {
				taken[subjectNameKey(baseTitle(it.Title))] = true
			}
		}
	}
	requiredOpen := map[string]bool{} // まだ修得・登録していない必修の科目番号
	if rc := gs.RequiredCourses; rc != nil {
		for _, r := range rc.Required {
			if r.Status != "passed" && r.Status != "enrolled" && r.NumberingCode != "" {
				requiredOpen[r.NumberingCode] = true
			}
		}
	}

	seen := map[string]bool{}
	for _, sm := range offered {
		subject := prefixDigits(sm.LectureCode, 6)
		n, err := strconv.Atoi(subject)
		if err != nil || seen[subject] || taken[subjectNameKey(baseTitle(sm.Title))] {
			continue
		}
		seen[subject] = true
		sug := PlanSuggestion{Title: baseTitle(sm.Title), LectureCode: sm.LectureCode, NumberingCode: sm.NumberingCode,
			DayPeriod: sm.DayPeriod, Instructors: sm.Instructors, URL: sm.URL, year: sm.Year,
			Required: requiredOpen[strings.Join(strings.Fields(sm.NumberingCode), "")]}
		category := ""
		if h, ok := bySubject[subject]; ok {
			if h.passed {
				continue // 組違いなどで名前が違うが、同じ科目を修得済み
			}
			category, sug.Retake, sug.Basis = h.category, true, "不合格だった科目"
		} else {
			best := -1
			for _, h := range history {
				if h.code/1000 != n/1000 {
					continue
				}
				d := h.code - n
				if d < 0 {
					d = -d
				}
				if best < 0 || d < best {
					best, category = d, h.category
					sug.Basis = "講義コードが近い「" + h.title + "」と同じ区分（推定）"
				}
			}
			if best < 0 {
				continue
			}
			sug.distance = best
		}
		key := normalizeCategory(category)
		if a, ok := aliases[key]; ok {
			key = a
		}
		it, ok := items[key]
		if !ok {
			continue
		}
		idx, ok := groupOf[it.Section+"\x00"+it.Name]
		if !ok {
			continue
		}
		sug.Section, sug.Category = it.Section, it.Name
		out[idx] = append(out[idx], sug)
	}
	return out
}

// fillSuggestionCredits は候補の単位数をシラバス詳細から埋める。
func (s *Service) fillSuggestionCredits(cands []PlanSuggestion) {
	var wg sync.WaitGroup
	for i := range cands {
		wg.Add(1)
		go func(c *PlanSuggestion) {
			defer wg.Done()
			if syl, err := s.Syllabus(c.year, c.LectureCode); err == nil {
				c.Credits = syl.Credits
			}
		}(&cands[i])
	}
	wg.Wait()
}
