package academic

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
	"github.com/ieyoukan/daigaku-hack/internal/provider/university"
)

// 空きコマとして扱う範囲（平日 1〜5 限）。
const defaultMaxPeriod = 5

var weekdaysMonFri = []string{"月", "火", "水", "木", "金"}

// Overview は履修状況の要約。
type Overview struct {
	Registration      *kyomu.Registration   `json:"registration"`
	Profile           *kyomu.StudentProfile `json:"profile,omitempty"`       // 学部・コース・年次・入学年度（履修条件の判断用）
	FirstChoiceCredit float64               `json:"first_choice_credits"`    // 第1希望の単位合計
	FreeSlots         []kyomu.Slot          `json:"free_slots"`              // 平日 1〜5 限の空きコマ
	LectureRooms      map[string]string     `json:"lecture_rooms,omitempty"` // 登録済み講義コード → 教室（シラバスより）
	Notes             []string              `json:"notes"`
}

// RegistrationStatus は本人の登録入口だけを確認する。履修一覧・成績・シラバスは読まない。
func (s *Service) RegistrationStatus(username string) (*kyomu.RegistrationStatus, error) {
	if username == "" {
		return nil, ErrNoCredentials
	}
	if status, ok := s.regStateCache.get(username); ok {
		return status, nil
	}
	var status *kyomu.RegistrationStatus
	err := s.withUser(username, func(c *kyomu.Client) error {
		// 同じ利用者からの同時呼び出しでも入口を重複取得しない。
		if cached, ok := s.regStateCache.get(username); ok {
			status = cached
			return nil
		}
		var err error
		status, err = c.FetchRegistrationStatus()
		if err == nil && status.State != "unknown" {
			s.regStateCache.set(username, status)
		}
		return err
	})
	return status, err
}

// Registration は教務システムから現在の履修登録状況を取得する。
func (s *Service) Registration(username string) (*kyomu.Registration, error) {
	var r *kyomu.Registration
	err := s.withUser(username, func(c *kyomu.Client) error {
		var err error
		r, err = c.FetchRegistration()
		return err
	})
	return r, err
}

// Overview は履修状況と空きコマ・注意点、所属情報、登録済み講義の教室をまとめて返す。
func (s *Service) Overview(username string) (*Overview, error) {
	var r *kyomu.Registration
	var prof *kyomu.StudentProfile
	err := s.withUser(username, func(c *kyomu.Client) error {
		var err error
		if r, err = c.FetchRegistration(); err != nil {
			return err
		}
		prof = s.profile(username, c)
		return nil
	})
	if err != nil {
		return nil, err
	}
	o := buildOverview(r)
	o.Profile = prof
	o.LectureRooms = s.lectureRooms(r)
	return o, nil
}

// profile は所属情報を返す（1 日キャッシュ。取れなくても履修機能は止めない）。withUser の中で呼ぶこと。
func (s *Service) profile(username string, c *kyomu.Client) *kyomu.StudentProfile {
	p, _ := s.profileWithError(username, c)
	return p
}

// profileWithError preserves session errors for operations that need the cohort
// and course. withUser can then renew the session rather than lose the error.
func (s *Service) profileWithError(username string, c *kyomu.Client) (*kyomu.StudentProfile, error) {
	if p, ok := s.profileCache.get(username); ok {
		return p, nil
	}
	p, err := c.FetchStudentProfile()
	if err != nil {
		return nil, err
	}
	s.profileCache.set(username, p)
	return p, nil
}

// lectureRooms は登録済み講義（第1希望）の教室をシラバスから集める。
func (s *Service) lectureRooms(r *kyomu.Registration) map[string]string {
	year := yearOf(r.YearTerm)
	rooms := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, l := range r.Lectures {
		if l.Rank != 1 {
			continue
		}
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			if syl, err := s.Syllabus(year, code); err == nil && syl.Room != "" {
				mu.Lock()
				rooms[code] = syl.Room
				mu.Unlock()
			}
		}(l.LectureCode)
	}
	wg.Wait()
	return rooms
}

func buildOverview(r *kyomu.Registration) *Overview {
	o := &Overview{Registration: r, Notes: []string{}}
	occ := occupied(r)
	for _, d := range weekdaysMonFri {
		for p := 1; p <= defaultMaxPeriod; p++ {
			if _, ok := occ[slotKey(d, p)]; !ok {
				o.FreeSlots = append(o.FreeSlots, kyomu.Slot{Day: d, Period: p})
			}
		}
	}
	for _, l := range firstChoiceLectures(r) {
		o.FirstChoiceCredit += l.Credits
	}

	if r.Open {
		o.Notes = append(o.Notes, "現在は履修登録を編集できます。登録・削除は教務システムの「履修登録」画面で行ってください。")
	} else {
		o.Notes = append(o.Notes, "現在は履修登録を編集できません（教員への確認依頼後・登録期間外など）。候補一覧で定員・登録者数を確認してください。")
	}
	if cs := r.Credits; cs != nil {
		switch {
		case cs.RemainingCredit < 0:
			o.Notes = append(o.Notes, fmt.Sprintf("履修上限 %s 単位を %s 単位超えています。", num(cs.Limit), num(-cs.RemainingCredit)))
		case cs.Limit > 0:
			o.Notes = append(o.Notes, fmt.Sprintf("履修上限まであと %s 単位登録できます（上限 %s / 制限対象の登録 %s / 対象外 %s）。",
				num(cs.RemainingCredit), num(cs.Limit), num(cs.LimitedCredits), num(cs.ExemptCredits)))
		}
	}
	// 抽選結果・登録調整の状況（当選・空きあり・定員充足など）は時間割の各コマに出すので、ここには書かない
	for _, l := range r.Lectures {
		where := slotLabel(l.Day, l.Period)
		if l.Rank > 1 {
			o.Notes = append(o.Notes, fmt.Sprintf("%s は第%d希望として「%s」も申請中です。", where, l.Rank, l.Title))
		}
	}
	return o
}

// CandidateQuery は候補講義探しの条件。
type CandidateQuery struct {
	// DeferDetails returns candidates immediately; the UI loads university
	// notes, room information and summaries in a separate authenticated request.
	DeferDetails bool                   `json:"defer_details,omitempty"`
	Slots        []kyomu.Slot           `json:"slots,omitempty"`  // 空なら平日 1〜5 限の空きコマ全部
	Scopes       []kyomu.CandidateScope `json:"scopes,omitempty"` // 空なら共通教育＋自学部
	IncludeFull  bool                   `json:"include_full,omitempty"`
	Keyword      string                 `json:"keyword,omitempty"` // 科目名・教員名で絞り込み
	// SyllabusKeywords はシラバス全文（授業内容・計画など）で探す語。どれか 1 つでも含む講義だけに絞る。
	// 科目名に出てこない内容（例: 「発音」で「会話C1F」）を見つけるためのもの。類義語を複数渡すとよい。
	SyllabusKeywords []string `json:"syllabus_keywords,omitempty"`
	// WithSyllabus なら返す講義それぞれにシラバスの要点（内容・負担の目安・履修条件の注意）を付ける。
	WithSyllabus bool `json:"with_syllabus,omitempty"`
	Offset       int  `json:"offset,omitempty"`
	Limit        int  `json:"limit,omitempty"` // 0 なら全件
}

// CandidateView は候補講義に、今の時間割との関係を加えたもの。
type CandidateView struct {
	Source            string                      `json:"source"`
	FetchedAt         time.Time                   `json:"fetched_at"`
	Timetable         *university.TimetableCourse `json:"timetable,omitempty"`
	kyomu.Candidate   `tstype:",extends"`
	Scope             kyomu.CandidateScope `json:"scope"`
	SeatsLeft         *int                 `json:"seats_left,omitempty"` // 定員が分かる場合の残席
	FitsFreeSlots     bool                 `json:"fits_free_slots"`      // 全コマが空いている
	Conflicts         []string             `json:"conflicts,omitempty"`  // 重なる登録済み講義
	AlreadyRegistered bool                 `json:"already_registered"`
	MatchedKeywords   []string             `json:"matched_keywords,omitempty"` // シラバス全文で一致した語
	// Neighbors は同じ曜日の直前・直後のコマに入っている登録済み講義（教室と休み時間つき）。移動の判断材料。
	Neighbors []Neighbor     `json:"neighbors,omitempty"`
	Syllabus  *LectureDigest `json:"syllabus,omitempty"`
}

// Neighbor は候補講義の前後のコマの登録済み講義。
type Neighbor struct {
	Position     string `json:"position"` // "前" / "後"
	Slot         string `json:"slot"`     // 例: 木2
	Title        string `json:"title"`
	Room         string `json:"room,omitempty"`
	BreakMinutes int    `json:"break_minutes,omitempty"` // 2 つのコマの間の休み時間（分）
}

// CandidateResult は候補講義探しの結果。
type CandidateResult struct {
	DetailsDeferred      bool                  `json:"details_deferred,omitempty"`
	TimetableCorrections []university.Passage  `json:"timetable_corrections,omitempty"`
	YearTerm             string                `json:"year_term"`
	RegistrationOpen     bool                  `json:"registration_open"` // 編集可能か。確認依頼後も false になる。
	RemainingCredits     *float64              `json:"remaining_credits,omitempty"`
	Profile              *kyomu.StudentProfile `json:"profile,omitempty"`
	SearchedSlots        []kyomu.Slot          `json:"searched_slots"`
	Total                int                   `json:"total"`  // 条件に合う全件数（ページング前）
	Offset               int                   `json:"offset"` // この結果の先頭位置
	Candidates           []CandidateView       `json:"candidates"`
	Errors               []string              `json:"errors,omitempty"`
}

// FindCandidates は指定コマ（既定は空きコマ全部）に追加できる講義を集め、
// 時間割との重なり・定員を判定して「入れやすい順」に並べる。
func (s *Service) FindCandidates(username string, q CandidateQuery) (*CandidateResult, error) {
	return s.FindCandidatesContext(context.Background(), username, q)
}

func (s *Service) FindCandidatesContext(ctx context.Context, username string, q CandidateQuery) (*CandidateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(q.Scopes) == 0 {
		q.Scopes = []kyomu.CandidateScope{kyomu.ScopeGeneral, kyomu.ScopeOwn}
	}
	var reg *kyomu.Registration
	var prof *kyomu.StudentProfile
	res := &CandidateResult{Candidates: []CandidateView{}, DetailsDeferred: q.DeferDetails}
	byCode := map[string]*CandidateView{}
	var order []string

	err := s.withUser(username, func(c *kyomu.Client) error {
		// A session retry must not retain candidates from a previous attempt.
		byCode = map[string]*CandidateView{}
		order = nil
		res.Errors = nil
		var err error
		reg, err = c.FetchRegistration()
		if err != nil {
			return err
		}
		prof = s.profile(username, c)
		slots := q.Slots
		if len(slots) == 0 {
			slots = buildOverview(reg).FreeSlots
		}
		res.SearchedSlots = slots
		for _, sl := range slots {
			for _, sc := range q.Scopes {
				if err := ctx.Err(); err != nil {
					return err
				}
				batch, err := s.candidatesCached(c, username, reg.YearTerm, sl, sc)
				if err != nil {
					if err == kyomu.ErrSessionExpired {
						return err
					}
					res.Errors = append(res.Errors, fmt.Sprintf("%s %s: %v", slotLabel(sl.Day, sl.Period), sc, err))
					continue
				}
				cands := batch.Candidates
				for _, cd := range cands {
					if _, seen := byCode[cd.LectureCode]; seen {
						continue
					}
					v := &CandidateView{Candidate: cd, Scope: sc}
					v.Source = s.baseURL + "/StudentApp/Regist/RegistSearchResults.aspx"
					v.FetchedAt = batch.FetchedAt
					v.Addable = reg.Open && cd.Addable
					if len(v.Slots) == 0 {
						v.Slots = []kyomu.Slot{sl}
					}
					byCode[cd.LectureCode] = v
					order = append(order, cd.LectureCode)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	res.YearTerm = reg.YearTerm
	res.RegistrationOpen = reg.Open
	res.Profile = prof
	year := yearOf(reg.YearTerm)
	var matches map[string][]string
	if len(q.SyllabusKeywords) > 0 {
		matches = s.keywordMatches(year, q.SyllabusKeywords, &res.Errors)
	}
	if reg.Credits != nil {
		rem := reg.Credits.RemainingCredit
		res.RemainingCredits = &rem
	}
	occ := occupied(reg)
	registered := map[string]bool{}
	for _, l := range reg.Lectures {
		registered[l.LectureCode] = true
	}
	kw := strings.TrimSpace(q.Keyword)
	for _, code := range order {
		v := byCode[code]
		if kw != "" && !strings.Contains(v.Title, kw) && !strings.Contains(v.Instructors, kw) {
			continue
		}
		if v.CapacityKnown && v.RegisteredKnown && v.Capacity > 0 {
			left := v.Capacity - v.Registered
			v.SeatsLeft = &left
		}
		if v.OverCapacity && !q.IncludeFull {
			continue
		}
		if matches != nil {
			if v.MatchedKeywords = matches[code]; len(v.MatchedKeywords) == 0 {
				continue
			}
		}
		v.AlreadyRegistered = registered[code]
		v.Conflicts = conflictsWith(v.Slots, occ, code)
		v.FitsFreeSlots = len(v.Conflicts) == 0
		res.Candidates = append(res.Candidates, *v)
	}
	sort.SliceStable(res.Candidates, func(i, j int) bool {
		a, b := res.Candidates[i], res.Candidates[j]
		if a.AlreadyRegistered != b.AlreadyRegistered {
			return !a.AlreadyRegistered
		}
		if a.FitsFreeSlots != b.FitsFreeSlots {
			return a.FitsFreeSlots
		}
		if len(a.MatchedKeywords) != len(b.MatchedKeywords) {
			return len(a.MatchedKeywords) > len(b.MatchedKeywords)
		}
		if a.OverCapacity != b.OverCapacity {
			return !a.OverCapacity
		}
		return methodRank(a.Method) < methodRank(b.Method)
	})

	res.Total = len(res.Candidates)
	res.Offset = q.Offset
	if q.Offset > 0 {
		if q.Offset >= len(res.Candidates) {
			res.Candidates = []CandidateView{}
		} else {
			res.Candidates = res.Candidates[q.Offset:]
		}
	}
	if q.Limit > 0 && len(res.Candidates) > q.Limit {
		res.Candidates = res.Candidates[:q.Limit]
	}

	rooms := map[string]string{}
	if !q.DeferDetails && len(res.Candidates) > 0 {
		rooms = s.lectureRooms(reg)
	}
	for i := range res.Candidates {
		res.Candidates[i].Neighbors = s.neighbors(reg, rooms, res.Candidates[i].Slots, res.Candidates[i].LectureCode)
	}
	if q.WithSyllabus && !q.DeferDetails {
		var wg sync.WaitGroup
		for i := range res.Candidates {
			wg.Add(1)
			go func(v *CandidateView) {
				defer wg.Done()
				d := s.digest(year, v.LectureCode)
				if strings.Contains(v.Title, "振替") && !contains(d.EligibilityNotes, transferNote) {
					d.EligibilityNotes = append([]string{transferNote}, d.EligibilityNotes...)
				}
				v.Syllabus = &d
			}(&res.Candidates[i])
		}
		wg.Wait()
	}
	if !q.DeferDetails && prof != nil && prof.Faculty != "" && len(res.Candidates) > 0 {
		var codes []string
		for _, v := range res.Candidates {
			codes = append(codes, v.LectureCode)
		}
		tt, e := s.University.Timetable(ctx, year, termOf(reg.YearTerm), prof.Faculty, prof.Course, codes)
		if e != nil {
			res.Errors = append(res.Errors, "公式時間割の備考は未取得です: "+e.Error())
		} else {
			res.Errors = append(res.Errors, tt.Warnings...)
			res.TimetableCorrections = tt.Corrections
			for i := range res.Candidates {
				for j := range tt.Courses {
					row := &tt.Courses[j]
					if row.LectureCode == res.Candidates[i].LectureCode {
						res.Candidates[i].Timetable = row
						break
					}
				}
			}
		}
	}
	if !q.DeferDetails && len(res.Candidates) > 0 && (prof == nil || prof.Faculty == "") {
		res.Errors = append(res.Errors, "所属情報を取得できないため、大学のコース別履修条件は未確認です。")
	}

	return res, nil
}

// keywordMatches はシラバス全文検索でキーワードごとに講義コードを集め、講義コード → 一致した語 を返す。
func (s *Service) keywordMatches(year int, keywords []string, errs *[]string) map[string][]string {
	out := map[string][]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := map[string]bool{}
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" || seen[kw] {
			continue
		}
		seen[kw] = true
		wg.Add(1)
		go func(kw string) {
			defer wg.Done()
			hits, err := s.searchCached(kyomu.SearchParams{Year: year, Keyword: kw})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				*errs = append(*errs, fmt.Sprintf("シラバス全文検索「%s」: %v", kw, err))
				return
			}
			for _, h := range hits {
				out[h.LectureCode] = append(out[h.LectureCode], kw)
			}
		}(kw)
	}
	wg.Wait()
	for code := range out {
		sort.Strings(out[code])
	}
	return out
}

// neighbors は候補講義の各コマについて、同じ曜日の直前・直後のコマにある登録済み講義（第1希望）を返す。
func (s *Service) neighbors(reg *kyomu.Registration, rooms map[string]string, slots []kyomu.Slot, self string) []Neighbor {
	type key struct {
		day    string
		period int
	}
	at := map[key]kyomu.RegisteredLecture{}
	for _, l := range reg.Lectures {
		if l.Rank == 1 && l.LectureCode != self {
			at[key{l.Day, l.Period}] = l
		}
	}
	inSlots := map[key]bool{}
	for _, sl := range slots {
		inSlots[key{sl.Day, sl.Period}] = true
	}
	var out []Neighbor
	for _, sl := range slots {
		for _, d := range []struct {
			pos    string
			period int
		}{{"前", sl.Period - 1}, {"後", sl.Period + 1}} {
			k := key{sl.Day, d.period}
			l, ok := at[k]
			if !ok || inSlots[k] {
				continue
			}
			n := Neighbor{Position: d.pos, Slot: slotLabel(sl.Day, d.period), Title: l.Title, Room: rooms[l.LectureCode]}
			if d.pos == "前" {
				n.BreakMinutes = s.breakMinutes(d.period, sl.Period)
			} else {
				n.BreakMinutes = s.breakMinutes(sl.Period, d.period)
			}
			out = append(out, n)
		}
	}
	return out
}

// lectureMinutes は 1 コマの長さ（琉球大学は 90 分）。
const lectureMinutes = 90

// breakMinutes は period の終了から next の開始までの分数（時程が不明なら 0）。
func (s *Service) breakMinutes(period, next int) int {
	a, okA := parseHHMM(s.PeriodStarts[period])
	b, okB := parseHHMM(s.PeriodStarts[next])
	if !okA || !okB {
		return 0
	}
	if m := b - (a + lectureMinutes); m > 0 {
		return m
	}
	return 0
}

func parseHHMM(v string) (int, bool) {
	h, m, ok := strings.Cut(v, ":")
	if !ok {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return hh*60 + mm, true
}

// methodRank は登録のしやすさ（小さいほど確実に登録できる）。
func methodRank(m string) int {
	switch {
	case strings.Contains(m, "自動"):
		return 0
	case strings.Contains(m, "許可制"):
		return 1
	case strings.Contains(m, "抽選"):
		return 2
	case strings.Contains(m, "許可コード"):
		return 3
	}
	return 1
}

// PlanItem は履修プランに入れたい講義 1 件のチェック結果。
type PlanItem struct {
	NumberingCode      string                      `json:"numbering_code,omitempty"`
	Term               string                      `json:"term,omitempty"`
	Availability       *LectureAvailability        `json:"availability,omitempty"`
	Timetable          *university.TimetableCourse `json:"timetable,omitempty"`
	EligibilityNotes   []string                    `json:"eligibility_notes,omitempty"`
	LectureCode        string                      `json:"lecture_code"`
	Title              string                      `json:"title,omitempty"`
	Credits            float64                     `json:"credits"`
	DayPeriod          string                      `json:"day_period,omitempty"`
	Slots              []kyomu.Slot                `json:"slots,omitempty"`
	RegistrationMethod string                      `json:"registration_method,omitempty"`
	AlreadyRegistered  bool                        `json:"already_registered"`
	Conflicts          []string                    `json:"conflicts,omitempty"`
	Error              string                      `json:"error,omitempty"`
}

// PlanCheck は履修プラン全体のチェック結果。
type PlanCheck struct {
	RequiredCourses      *RequiredCheck       `json:"required_courses,omitempty"`
	TimetableCorrections []university.Passage `json:"timetable_corrections,omitempty"`
	Curriculum           *CurriculumCheck     `json:"curriculum,omitempty"`
	YearTerm             string               `json:"year_term"`
	Year                 int                  `json:"year"`
	Limit                float64              `json:"limit"`
	CurrentCredits       float64              `json:"current_credits"`   // 履修制限対象の登録単位
	AddedCredits         float64              `json:"added_credits"`     // 追加分（登録済みを除く）
	ProjectedCredits     float64              `json:"projected_credits"` // 追加後（概算）
	OverLimit            bool                 `json:"over_limit"`
	Items                []PlanItem           `json:"items"`
	Notes                []string             `json:"notes"`
}

// CheckPlan は追加したい講義コードの一覧について、コマの重なりと単位上限を判定する。
// 追加分はすべて履修制限対象として概算する（対象外科目かどうかは教務システムの判定次第）。
func (s *Service) CheckPlan(username string, codes []string) (*PlanCheck, error) {
	return s.CheckPlanContext(context.Background(), username, codes)
}

func (s *Service) CheckPlanContext(ctx context.Context, username string, codes []string) (*PlanCheck, error) {
	if len(codes) == 0 || len(codes) > 20 {
		return nil, fmt.Errorf("講義コードは 1〜20 件で指定してください")
	}
	var unique []string
	seenCodes := map[string]bool{}
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || seenCodes[code] {
			continue
		}
		unique = append(unique, code)
		seenCodes[code] = true
	}
	codes = unique
	if len(codes) == 0 {
		return nil, fmt.Errorf("講義コードを指定してください")
	}
	var reg *kyomu.Registration
	var prof *kyomu.StudentProfile
	var grades *kyomu.GradeList
	err := s.withUser(username, func(c *kyomu.Client) error {
		var e error
		reg, e = c.FetchRegistration()
		if e != nil {
			return e
		}
		prof = s.profile(username, c)
		grades, e = c.FetchGrades()
		if e == kyomu.ErrSessionExpired {
			return e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	year := yearOf(reg.YearTerm)
	pc := &PlanCheck{YearTerm: reg.YearTerm, Year: year, Items: make([]PlanItem, 0, len(codes)), Notes: []string{}}
	if reg.Credits != nil {
		pc.Limit = reg.Credits.Limit
		pc.CurrentCredits = reg.Credits.LimitedCredits
	}
	registered := map[string]bool{}
	for _, l := range reg.Lectures {
		registered[l.LectureCode] = true
	}

	items := make([]PlanItem, len(codes))
	var wg sync.WaitGroup
	for i, code := range codes {
		wg.Add(1)
		go func(i int, code string) {
			defer wg.Done()
			it := PlanItem{LectureCode: strings.TrimSpace(code)}
			syl, err := s.Syllabus(year, it.LectureCode)
			if err != nil {
				it.Error = err.Error()
			} else {
				it.Title, it.Credits, it.DayPeriod, it.Slots = syl.Title, syl.Credits, syl.DayPeriod, syl.Slots
				it.RegistrationMethod = syl.RegistrationMethod
				it.NumberingCode, it.Term = syl.NumberingCode, syl.Term
				it.EligibilityNotes = eligibilityNotes(syl)
				it.Availability = publicAvailability(syl.Quota, syl.URL)
			}
			items[i] = it
		}(i, code)
	}
	wg.Wait()

	occ := occupied(reg)
	for i := range items {
		it := &items[i]
		it.AlreadyRegistered = registered[it.LectureCode]
		if it.Error != "" {
			pc.Items = append(pc.Items, *it)
			continue
		}
		it.Conflicts = conflictsWith(it.Slots, occ, it.LectureCode)
		// プラン内の講義同士の重なり
		for j := 0; j < i; j++ {
			other := items[j]
			if other.Error != "" || other.AlreadyRegistered {
				continue
			}
			for _, a := range it.Slots {
				for _, b := range other.Slots {
					if slotsOverlap(a, b) {
						it.Conflicts = append(it.Conflicts, fmt.Sprintf("%s: %s（プラン内）", slotLabel(a.Day, a.Period), other.Title))
					}
				}
			}
		}
		if !it.AlreadyRegistered {
			pc.AddedCredits += it.Credits
		}
		if len(it.Slots) == 0 && it.DayPeriod != "" {
			pc.Notes = append(pc.Notes, fmt.Sprintf("「%s」は曜日時限が「%s」のため重なり判定の対象外です。", it.Title, it.DayPeriod))
		}
		pc.Items = append(pc.Items, *it)
	}
	var refs []lectureRef
	for i := range pc.Items {
		it := &pc.Items[i]
		if it.Availability != nil {
			refs = append(refs, lectureRef{Year: year, Code: it.LectureCode, Term: it.Term, Slots: it.Slots, Availability: &it.Availability})
		}
	}
	s.enrichAvailability(username, refs)
	if prof != nil && prof.Faculty != "" {
		tt, e := s.University.Timetable(ctx, year, termOf(reg.YearTerm), prof.Faculty, prof.Course, codes)
		if e != nil {
			pc.Notes = append(pc.Notes, "時間割の補足情報は未取得です: "+e.Error())
		} else {
			pc.Notes = append(pc.Notes, tt.Warnings...)
			pc.TimetableCorrections = tt.Corrections
			for i := range pc.Items {
				for j := range tt.Courses {
					row := &tt.Courses[j]
					if row.LectureCode == pc.Items[i].LectureCode {
						pc.Items[i].Timetable = row
						pc.Items[i].EligibilityNotes = append(pc.Items[i].EligibilityNotes, row.Notes)
						break
					}
				}
			}
		}
	}
	pc.Curriculum = s.curriculumCheck(ctx, prof, grades, reg, pc.Items)
	if prof != nil {
		pc.RequiredCourses = s.requiredCheck(ctx, prof, grades, reg, pc.Items, prof.EnterYear)
	}
	pc.ProjectedCredits = pc.CurrentCredits + pc.AddedCredits
	pc.OverLimit = pc.Limit > 0 && pc.ProjectedCredits > pc.Limit
	if pc.OverLimit {
		pc.Notes = append(pc.Notes, fmt.Sprintf("追加後 %s 単位で履修上限 %s 単位を超えます（対象外科目が含まれる場合は超えないこともあります）。",
			num(pc.ProjectedCredits), num(pc.Limit)))
	}
	if !reg.Open {
		pc.Notes = append(pc.Notes, "現在は履修登録を編集できません（教員への確認依頼後・登録期間外など）。このプランの登録については教務システムで確認してください。")
	}
	return pc, nil
}

// occupied は登録済み講義（全希望順位）をコマ→講義名の一覧にする。
func occupied(r *kyomu.Registration) map[string][]string {
	occ := map[string][]string{}
	for _, l := range r.Lectures {
		if l.Day == "集中等" {
			continue
		}
		k := slotKey(l.Day, l.Period)
		occ[k] = append(occ[k], l.LectureCode+"\t"+l.Title)
	}
	return occ
}

func conflictsWith(slots []kyomu.Slot, occ map[string][]string, selfCode string) []string {
	var out []string
	for _, sl := range slots {
		for _, e := range occ[slotKey(sl.Day, sl.Period)] {
			code, title, _ := strings.Cut(e, "\t")
			if code == selfCode {
				continue
			}
			out = append(out, fmt.Sprintf("%s: %s", slotLabel(sl.Day, sl.Period), title))
		}
	}
	return out
}

func slotsOverlap(a, b kyomu.Slot) bool {
	return a.Day == b.Day && a.Period == b.Period && (a.Half == "" || b.Half == "" || a.Half == b.Half)
}

func slotKey(day string, period int) string { return day + strconv.Itoa(period) }

func slotLabel(day string, period int) string {
	if day == "集中等" {
		return "集中等"
	}
	return fmt.Sprintf("%s%d", day, period)
}

func yearOf(yearTerm string) int {
	if i := strings.Index(yearTerm, "年度"); i >= 4 {
		if y, err := strconv.Atoi(yearTerm[i-4 : i]); err == nil {
			return y
		}
	}
	return 0
}

// num は 2.0 → "2"、1.5 → "1.5" のように単位数を表示する。
func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// A multi-period course occupies multiple timetable cells but earns its credits once.
func firstChoiceLectures(reg *kyomu.Registration) []kyomu.RegisteredLecture {
	var out []kyomu.RegisteredLecture
	seen := map[string]bool{}
	for _, l := range reg.Lectures {
		if l.Rank != 1 {
			continue
		}
		key := l.LectureCode
		if key == "" {
			key = l.Title
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}
