package academic

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
	"github.com/ieyoukan/ryudai-tools/internal/provider/university"
)

func (s *Service) StudyRules(ctx context.Context, user string, year int, faculty, course, keyword string) (*university.StudyRules, error) {
	if user != "" && (year == 0 || faculty == "" || course == "") {
		var prof *kyomu.StudentProfile
		err := s.withUser(user, func(c *kyomu.Client) error {
			var err error
			prof, err = s.profileWithError(user, c)
			return err
		})
		if err != nil {
			return nil, err
		}
		if prof != nil {
			if year == 0 {
				year = prof.EnterYear
			}
			if faculty == "" {
				faculty = prof.Faculty
			}
			if course == "" && faculty == prof.Faculty {
				course = prof.Course
			}
		}
	}
	return s.University.StudyRules(ctx, year, faculty, course, keyword)
}

type FusionGroup struct {
	Course             string   `json:"course"`
	Earned             float64  `json:"earned_credits"`
	Registered         float64  `json:"registered_credits"`
	Added              float64  `json:"added_credits"`
	ShortfallAfterPlan float64  `json:"shortfall_after_plan"`
	Titles             []string `json:"titles"`
}
type CurriculumCheck struct {
	EnterYear    int                    `json:"enter_year,omitempty"`
	Course       string                 `json:"course,omitempty"`
	Rule         *university.FusionRule `json:"fusion_rule,omitempty"`
	Documents    []university.Document  `json:"documents,omitempty"`
	Groups       []FusionGroup          `json:"fusion_groups"`
	Unclassified []string               `json:"unclassified,omitempty"`
	Notes        []string               `json:"notes"`
}

var fusionNumberRE = regexp.MustCompile(`^(機械|エネ|電気|電情|社基|建築|知能)9[89][0-9]$`)
var fusionCourses = map[string]string{"機械": "機械工学コース", "エネ": "エネルギー環境工学コース", "電気": "電気システム工学コース", "電情": "電子情報通信コース", "社基": "社会基盤デザインコース", "建築": "建築学コース", "知能": "知能情報コース"}

type fusionCredit struct {
	Number, Title, State string
	Credits              float64
}

func fusionGroups(rule *university.FusionRule, own string, credits []fusionCredit) ([]FusionGroup, []string) {
	groups := map[string]*FusionGroup{}
	seen := map[string]bool{}
	var unknown []string
	// Caller supplies earned first, so retakes/current registrations never double count.
	for _, c := range credits {
		c.Number = strings.Join(strings.Fields(c.Number), "")
		m := fusionNumberRE.FindStringSubmatch(c.Number)
		if m == nil {
			unknown = append(unknown, c.Title)
			continue
		}
		if seen[c.Number] {
			continue
		}
		seen[c.Number] = true
		name := fusionCourses[m[1]]
		if university.CourseMatches(name, own) {
			unknown = append(unknown, c.Title+"（自コースの融合科目）")
			continue
		}
		g := groups[name]
		if g == nil {
			g = &FusionGroup{Course: name}
			groups[name] = g
		}
		switch c.State {
		case "earned":
			g.Earned += c.Credits
		case "registered":
			g.Registered += c.Credits
		case "added":
			g.Added += c.Credits
		}
		g.Titles = append(g.Titles, c.Title)
	}
	var out []FusionGroup
	for _, g := range groups {
		g.ShortfallAfterPlan = rule.MinimumCredits - g.Earned - g.Registered - g.Added
		if g.ShortfallAfterPlan < 0 {
			g.ShortfallAfterPlan = 0
		}
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ShortfallAfterPlan != out[j].ShortfallAfterPlan {
			return out[i].ShortfallAfterPlan < out[j].ShortfallAfterPlan
		}
		return out[i].Course < out[j].Course
	})
	return out, unknown
}

// CurriculumCheck complements the official credit totals with course grouping.
// It deliberately does not certify graduation: prerequisites, substitutions and
// cohort-specific exceptions remain visible in the cited handbook.
func (s *Service) curriculumCheck(ctx context.Context, prof *kyomu.StudentProfile, grades *kyomu.GradeList, reg *kyomu.Registration, added []PlanItem) *CurriculumCheck {
	r := &CurriculumCheck{Groups: []FusionGroup{}, Notes: []string{}}
	if prof == nil || prof.EnterYear == 0 || prof.Course == "" {
		r.Notes = append(r.Notes, "所属コースと適用便覧年度が不明のため、コース別履修ルールは未確認です。")
		return r
	}
	r.EnterYear = prof.EnterYear
	r.Course = prof.Course
	rules, err := s.University.StudyRules(ctx, prof.EnterYear, prof.Faculty, prof.Course, "工学融合")
	if err != nil {
		r.Notes = append(r.Notes, "便覧の取得に失敗しました: "+err.Error())
		return r
	}
	r.Documents = rules.Documents
	r.Rule = rules.FusionRule
	r.Notes = append(r.Notes, rules.Warnings...)
	for _, src := range rules.Sources {
		if src.Stale {
			r.Notes = append(r.Notes, src.Warning+"（"+src.URL+"）")
		}
	}
	if r.Rule == nil {
		r.Notes = append(r.Notes, "この便覧から同一コースの工学融合単位条件を自動抽出できませんでした。get_study_rules で本文を確認してください。")
		return r
	}
	var credits []fusionCredit
	collect := func(year int, code, title, state string, points float64) {
		syl, e := s.Syllabus(year, code)
		if e != nil {
			r.Unclassified = append(r.Unclassified, title+"（科目番号を確認できません）")
			return
		}
		credits = append(credits, fusionCredit{Number: syl.NumberingCode, Title: title, State: state, Credits: points})
	}
	if grades != nil {
		for _, g := range grades.Grades {
			if g.Passed && strings.Contains(g.Category, "工学融合") {
				collect(g.Year, g.LectureCode, g.Title, "earned", g.Credits)
			}
		}
	} else {
		r.Notes = append(r.Notes, "修得済みの成績を取得できないため過去の単位は未確認です。")
	}
	if reg != nil {
		seen := map[string]bool{}
		for _, l := range reg.Lectures {
			if l.Rank == 1 && !seen[l.LectureCode] && (strings.Contains(l.Category, "工学融合") || strings.HasPrefix(l.LectureCode, "618")) {
				seen[l.LectureCode] = true
				collect(yearOf(reg.YearTerm), l.LectureCode, l.Title, "registered", l.Credits)
			}
		}
	}
	for _, l := range added {
		if !l.AlreadyRegistered && l.Error == "" && fusionNumberRE.MatchString(strings.Join(strings.Fields(l.NumberingCode), "")) {
			credits = append(credits, fusionCredit{Number: l.NumberingCode, Title: l.Title, State: "added", Credits: l.Credits})
		}
	}
	groups, unknown := fusionGroups(r.Rule, prof.Course, credits)
	r.Groups = groups
	r.Unclassified = append(r.Unclassified, unknown...)
	r.Notes = append(r.Notes, "科目番号から提供コースを分類した単位の見通しです。異なるコースの単位を合算して条件を満たしたと判断しないでください。個別成績表の区分合計が足りていても組合せ条件を満たすとは限りません。今期・追加予定の単位は未修得です。")
	if r.Rule.RequiresIntro {
		r.Notes = append(r.Notes, "選択必修（導入科目）を含む条件があります。単位数に加えて便覧の対象科目を確認してください。")
	}
	if len(r.Groups) > 1 {
		r.Notes = append(r.Notes, fmt.Sprintf("融合科目が %d コースに分散しています。同一コース %g 単位の条件をコースごとに確認してください。", len(r.Groups), r.Rule.MinimumCredits))
	}
	return r
}

func termOf(yearTerm string) string {
	if strings.Contains(yearTerm, "前") {
		return "前学期"
	}
	if strings.Contains(yearTerm, "後") {
		return "後学期"
	}
	return ""
}
