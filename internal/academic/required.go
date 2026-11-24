package academic

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
	"github.com/ieyoukan/ryudai-tools/internal/provider/university"
)

type RequiredEvidence struct {
	Year          int     `json:"year"`
	LectureCode   string  `json:"lecture_code"`
	Title         string  `json:"title"`
	Credits       float64 `json:"credits"`
	Grade         string  `json:"grade,omitempty"`
	NumberingCode string  `json:"numbering_code,omitempty"`
	State         string  `json:"state"`
	Note          string  `json:"note,omitempty"`
}

type RequiredSubjectStatus struct {
	university.RequiredSubject `tstype:",extends"`
	Status                     string             `json:"status"` // passed, enrolled, not_passed, unverified, planned
	Evidence                   []RequiredEvidence `json:"evidence"`
	Notes                      []string           `json:"notes,omitempty"`
}

type RequiredCheck struct {
	Profile               *kyomu.StudentProfile   `json:"profile,omitempty"`
	Catalog               *RequiredCatalogInfo    `json:"catalog,omitempty"`
	FetchedAt             time.Time               `json:"fetched_at"`
	GradesAvailable       bool                    `json:"grades_available"`
	RegistrationAvailable bool                    `json:"registration_available"`
	Required              []RequiredSubjectStatus `json:"required"`
	SelectiveRequired     []RequiredSubjectStatus `json:"selective_required"`
	Counts                map[string]int          `json:"counts"` // mandatory subjects only
	Notes                 []string                `json:"notes"`
}

type RequiredCatalogInfo struct {
	EnterYear int                 `json:"enter_year"`
	Faculty   string              `json:"faculty"`
	Course    string              `json:"course"`
	Scope     string              `json:"scope"`
	Sources   []university.Source `json:"sources"`
}

func (s *Service) RequiredCourses(ctx context.Context, user string, year int, faculty, course string, all bool) (*university.RequiredCatalog, error) {
	if user != "" && (year == 0 || faculty == "" || course == "" && !all) {
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
			if course == "" && !all && faculty == prof.Faculty {
				course = prof.Course
			}
		}
	}
	if all {
		course = ""
	}
	return s.University.RequiredCourses(ctx, year, faculty, course)
}

func (s *Service) RequiredCourseStatus(ctx context.Context, user string, enterYear int) (*RequiredCheck, error) {
	var prof *kyomu.StudentProfile
	var grades *kyomu.GradeList
	var reg *kyomu.Registration
	var gradeErr, regErr error
	err := s.withUser(user, func(c *kyomu.Client) error {
		var err error
		prof, err = s.profileWithError(user, c)
		if err != nil {
			return err
		}
		grades, gradeErr = c.FetchGrades()
		if gradeErr == kyomu.ErrSessionExpired {
			return gradeErr
		}
		reg, regErr = c.FetchRegistration()
		if regErr == kyomu.ErrSessionExpired {
			return regErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if prof == nil || prof.Faculty == "" || prof.Course == "" || prof.EnterYear == 0 {
		return nil, fmt.Errorf("学籍情報から所属コースと入学年度を確認できません。学籍番号からコースは推測しません")
	}
	if enterYear == 0 {
		enterYear = prof.EnterYear
	}
	report := s.requiredCheck(ctx, prof, grades, reg, nil, enterYear)
	if gradeErr != nil {
		report.Notes = append(report.Notes, "成績は未取得です: "+gradeErr.Error())
	}
	if regErr != nil {
		report.Notes = append(report.Notes, "今期の履修登録は未取得です: "+regErr.Error())
	}
	return report, nil
}

func subjectNameKey(s string) string {
	s = strings.NewReplacer("Ⅰ", "I", "Ⅱ", "II", "Ⅲ", "III", "Ⅳ", "IV", "Ⅴ", "V", "Ⅵ", "VI", "Ⅶ", "VII", "Ⅷ", "VIII", "Ⅸ", "IX", "Ⅹ", "X", "Ⅺ", "XI", "Ⅻ", "XII").Replace(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		if r >= '！' && r <= '～' {
			return r - '！' + '!'
		}
		return r
	}, s)
}

// subjectTitleKey は組番号・空白・ローマ数字の表記差をそろえる。ⅠとⅡなどの科目番号は区別する。
func subjectTitleKey(title string) string { return subjectNameKey(baseTitle(title)) }

type syllabusIdentity struct {
	Number string
	Err    error
}

func syllabusKey(year int, code string) string { return fmt.Sprintf("%d/%s", year, code) }

func (s *Service) requiredCheck(ctx context.Context, prof *kyomu.StudentProfile, grades *kyomu.GradeList, reg *kyomu.Registration, added []PlanItem, year int) *RequiredCheck {
	r := &RequiredCheck{Profile: prof, FetchedAt: time.Now().UTC(), GradesAvailable: grades != nil, RegistrationAvailable: reg != nil, Required: []RequiredSubjectStatus{}, SelectiveRequired: []RequiredSubjectStatus{}, Counts: map[string]int{}, Notes: []string{}}
	if prof == nil || prof.Faculty == "" || prof.Course == "" || year == 0 {
		r.Notes = append(r.Notes, "所属コース・適用便覧年度が不明のため必修科目は未確認です。")
		return r
	}
	catalog, err := s.University.RequiredCourses(ctx, year, prof.Faculty, prof.Course)
	if err != nil {
		r.Notes = append(r.Notes, "必修科目表は未取得です: "+err.Error())
		return r
	}
	r.Catalog = &RequiredCatalogInfo{EnterYear: catalog.EnterYear, Faculty: catalog.Faculty, Course: prof.Course, Scope: catalog.Scope, Sources: catalog.Sources}
	r.Notes = append(r.Notes, catalog.Warnings...)
	var subjects []university.RequiredSubject
	for _, course := range catalog.Courses {
		subjects = append(subjects, course.Subjects...)
	}
	identities := map[string]syllabusIdentity{}
	type ref struct {
		Year int
		Code string
	}
	refs := map[string]ref{}
	names := map[string]bool{}
	for _, subject := range subjects {
		names[subjectTitleKey(subject.Title)] = true
	}
	add := func(year int, code, title, category string) {
		if names[subjectTitleKey(title)] || strings.Contains(category, "専門") || strings.HasPrefix(code, "6") {
			refs[syllabusKey(year, code)] = ref{year, code}
		}
	}
	if grades != nil {
		for _, g := range grades.Grades {
			add(g.Year, g.LectureCode, g.Title, g.Category)
		}
	}
	if reg != nil {
		for _, l := range firstChoiceLectures(reg) {
			add(yearOf(reg.YearTerm), l.LectureCode, l.Title, l.Category)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for key, target := range refs {
		wg.Add(1)
		go func(key string, ref ref) {
			defer wg.Done()
			id := syllabusIdentity{}
			if ctx.Err() != nil {
				id.Err = ctx.Err()
			} else if ref.Year == 0 || ref.Code == "" {
				id.Err = fmt.Errorf("開講年度または講義コードが不明です")
			} else {
				sy, e := s.Syllabus(ref.Year, ref.Code)
				id.Err = e
				if sy != nil {
					id.Number = strings.Join(strings.Fields(sy.NumberingCode), "")
				}
			}
			mu.Lock()
			identities[key] = id
			mu.Unlock()
		}(key, target)
	}
	wg.Wait()
	identityErrors := 0
	for _, id := range identities {
		if id.Err != nil || id.Number == "" {
			identityErrors++
		}
	}
	if identityErrors > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d件の科目番号を確認できませんでした。未修得と断定できない科目は確認不能にしています。", identityErrors))
	}
	for _, subject := range subjects {
		item := checkRequiredSubject(subject, grades, reg, added, identities)
		if item.Status == "not_passed" && identityErrors > 0 {
			item.Status = "unverified"
		}
		if subject.Selection == "required" {
			r.Required = append(r.Required, item)
			r.Counts[item.Status]++
		} else {
			r.SelectiveRequired = append(r.SelectiveRequired, item)
		}
	}
	r.Notes = append(r.Notes, "科目番号と単位数で照合しています。修得済みの単位を再履修分と合算しません。履修中・追加予定は修得済みとして扱いません。共通教育・先修条件・選択必修の組合せ・振替は別途確認してください。")
	if grades == nil {
		r.Notes = append(r.Notes, "成績を取得できないため、修得済みか確認できません。")
	}
	if reg == nil {
		r.Notes = append(r.Notes, "今期の履修情報を取得できないため、履修中か確認できません。")
	}
	return r
}

func checkRequiredSubject(subject university.RequiredSubject, grades *kyomu.GradeList, reg *kyomu.Registration, added []PlanItem, ids map[string]syllabusIdentity) RequiredSubjectStatus {
	r := RequiredSubjectStatus{RequiredSubject: subject, Status: "not_passed", Evidence: []RequiredEvidence{}}
	passed, enrolled, planned, ambiguous := false, false, false, false
	consider := func(year int, code, title, state, grade string, credits float64) {
		id := ids[syllabusKey(year, code)]
		matched := id.Number != "" && id.Number == subject.NumberingCode
		if !matched && subjectTitleKey(title) != subjectTitleKey(subject.Title) {
			return
		}
		e := RequiredEvidence{Year: year, LectureCode: code, Title: title, Credits: credits, Grade: grade, NumberingCode: id.Number, State: state}
		if !matched || credits < subject.Credits {
			if state == "passed" || state == "enrolled" {
				ambiguous = true
				e.Note = "科目番号の一致または便覧の必要単位数を確認できません。振替・同名科目・年度間の変更は個別確認が必要です。"
			}
		} else if state == "passed" {
			passed = true
		} else if state == "enrolled" {
			enrolled = true
		}
		r.Evidence = append(r.Evidence, e)
	}
	if grades != nil {
		for _, g := range grades.Grades {
			state := "failed"
			if g.Passed {
				state = "passed"
			}
			consider(g.Year, g.LectureCode, g.Title, state, g.Grade, g.Credits)
		}
	}
	if reg != nil {
		for _, l := range firstChoiceLectures(reg) {
			consider(yearOf(reg.YearTerm), l.LectureCode, l.Title, "enrolled", "", l.Credits)
		}
	}
	for _, l := range added {
		if l.Error == "" && l.NumberingCode == subject.NumberingCode && l.Credits >= subject.Credits {
			planned = true
			r.Evidence = append(r.Evidence, RequiredEvidence{LectureCode: l.LectureCode, Title: l.Title, Credits: l.Credits, NumberingCode: l.NumberingCode, State: "planned"})
		}
	}
	switch {
	case passed:
		r.Status = "passed"
	case enrolled:
		r.Status = "enrolled"
	case planned:
		r.Status = "planned"
	case ambiguous || grades == nil || reg == nil:
		r.Status = "unverified"
	}
	if ambiguous {
		r.Notes = append(r.Notes, "科目名が似た成績または単位数が異なる成績があります。根拠を確認してください。")
	}
	return r
}
