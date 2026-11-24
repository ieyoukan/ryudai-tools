package academic

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

// LectureAvailability distinguishes missing information from a known zero.
// Public syllabus caches never contain student-specific registration data.
type LectureAvailability struct {
	RegisteredByUser bool       `json:"registered_by_user,omitempty"`
	Status           string     `json:"status"`
	Capacity         *int       `json:"capacity,omitempty"`
	Registered       *int       `json:"registered,omitempty"`
	SeatsLeft        *int       `json:"seats_left,omitempty"`
	Addable          *bool      `json:"addable,omitempty"`
	Method           string     `json:"method,omitempty"`
	Note             string     `json:"note,omitempty"`
	Source           string     `json:"source,omitempty"`
	FetchedAt        *time.Time `json:"fetched_at,omitempty"`
}

type LectureDetails struct {
	*kyomu.Syllabus `tstype:",extends"`
	Availability    *LectureAvailability `json:"availability"`
}
type candidateBatch struct {
	Candidates []kyomu.Candidate
	FetchedAt  time.Time
}

func (s *Service) candidatesCached(c *kyomu.Client, user, yearTerm string, sl kyomu.Slot, scope kyomu.CandidateScope) (candidateBatch, error) {
	key := fmt.Sprintf("%s|%s|%s|%d|%s", user, yearTerm, sl.Day, sl.Period, scope)
	if batch, ok := s.candCache.get(key); ok {
		return batch, nil
	}
	cs, err := c.FetchCandidates(sl.Day, sl.Period, scope)
	if err != nil {
		return candidateBatch{}, err
	}
	batch := candidateBatch{Candidates: cs, FetchedAt: time.Now().UTC()}
	s.candCache.set(key, batch)
	return batch, nil
}

var quotaRE = regexp.MustCompile(`^\s*(\d+)\s*人?\s*$`)

func publicAvailability(quota, url string) *LectureAvailability {
	a := &LectureAvailability{Status: "public_only", Source: url, Note: "公開定員です。登録者数・残席はアカウント連携後に候補一覧から確認します。"}
	if m := quotaRE.FindStringSubmatch(quota); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			a.Capacity = &n
		}
	}
	return a
}

type lectureRef struct {
	Year         int
	Code, Term   string
	Slots        []kyomu.Slot
	Availability **LectureAvailability
	Faculty      string
}

func availabilityScopes(faculty string) []kyomu.CandidateScope {
	if faculty != "" && !strings.Contains(faculty, "共通教育") {
		return []kyomu.CandidateScope{kyomu.ScopeOwn, kyomu.ScopeOther, kyomu.ScopeGeneral}
	}
	return []kyomu.CandidateScope{kyomu.ScopeGeneral, kyomu.ScopeOwn, kyomu.ScopeOther}
}

func matchesRegistrationTerm(lecture, registration string) bool {
	if strings.Contains(lecture, "通年") {
		return true
	}
	if strings.Contains(registration, "前") {
		return strings.Contains(lecture, "前") || strings.Contains(lecture, "第1") || strings.Contains(lecture, "第2")
	}
	if strings.Contains(registration, "後") {
		return strings.Contains(lecture, "後") || strings.Contains(lecture, "第3") || strings.Contains(lecture, "第4")
	}
	return false
}

// enrichAvailability reads only the relevant slots, sharing requests across all
// results. It matches academic year, semester and lecture code, never the title.
func (s *Service) enrichAvailability(user string, refs []lectureRef) {
	if user == "" || len(refs) == 0 {
		return
	}
	public := make([]LectureAvailability, len(refs))
	for i, ref := range refs {
		public[i] = **ref.Availability
	}
	err := s.withUser(user, func(c *kyomu.Client) error {
		// A re-login may land on a different registration period. Start each
		// attempt from the public values so earlier private counts cannot linger.
		for i, ref := range refs {
			a := public[i]
			*ref.Availability = &a
		}
		reg, err := c.FetchRegistration()
		if err != nil {
			return err
		}
		for _, ref := range refs {
			a := *ref.Availability
			if ref.Year != yearOf(reg.YearTerm) || !matchesRegistrationTerm(ref.Term, reg.YearTerm) {
				a.Status = "term_mismatch"
				a.Note = "現在の履修登録画面と開講年度・学期が一致しないため公開定員だけを返します。"
				continue
			}
			for _, l := range firstChoiceLectures(reg) {
				if l.LectureCode == ref.Code {
					a.RegisteredByUser = true
					break
				}
			}
			if len(ref.Slots) == 0 {
				a.Status = "no_schedule"
				a.Note = "集中講義等で照会コマを特定できないため登録者数・残席は不明です。"
				continue
			}
			found := false
			var lookupErrors []string
			for _, sl := range ref.Slots {
				for _, scope := range availabilityScopes(ref.Faculty) {
					batch, e := s.candidatesCached(c, user, reg.YearTerm, sl, scope)
					if e == kyomu.ErrSessionExpired {
						return e
					}
					if e != nil {
						lookupErrors = append(lookupErrors, e.Error())
						continue
					}
					for _, cd := range batch.Candidates {
						if cd.LectureCode != ref.Code || cd.Year != 0 && cd.Year != ref.Year {
							continue
						}
						a.Status = "live"
						a.Method = cd.Method
						a.Note = cd.Note
						addable := reg.Open && cd.Addable
						a.Addable = &addable
						if cd.CapacityKnown && cd.Capacity > 0 {
							a.Capacity = &cd.Capacity
						} else {
							a.Capacity = nil
						}
						if cd.RegisteredKnown {
							a.Registered = &cd.Registered
						}
						if a.Capacity != nil && a.Registered != nil {
							n := *a.Capacity - *a.Registered
							a.SeatsLeft = &n
						}
						a.Source = s.baseURL + "/StudentApp/Regist/RegistSearchResults.aspx"
						a.FetchedAt = &batch.FetchedAt
						found = true
						break
					}
					if found {
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				a.Status = "not_found"
				a.Note = "本人の候補一覧で一致する講義を確認できませんでした。履修不可とは断定できません。"
				if a.RegisteredByUser {
					a.Status = "registered_not_listed"
					a.Note = "この科目は今期の第1希望で登録済みです。追加候補一覧には表示されておらず、登録者数・残席は取得できませんでした。"
				}
				if len(lookupErrors) > 0 {
					a.Status = "unavailable"
					a.Note = strings.Join(lookupErrors, "; ")
				}
			}
		}
		return nil
	})
	if err != nil {
		for _, ref := range refs {
			a := *ref.Availability
			if a.Status != "live" {
				a.Status = "unavailable"
				a.Note = "履修登録情報を取得できません: " + err.Error()
			}
		}
	}
}

func (s *Service) SearchLectures(user string, p kyomu.SearchParams, limit int, details bool) (*SearchResult, error) {
	r, err := s.SearchSyllabus(p, limit, details)
	if err != nil {
		return nil, err
	}
	refs := make([]lectureRef, 0, len(r.Results))
	for i := range r.Results {
		d := &r.Results[i]
		d.Availability = publicAvailability(d.Quota, d.URL)
		refs = append(refs, lectureRef{Year: d.Year, Code: d.LectureCode, Term: d.Term, Slots: d.Slots, Availability: &d.Availability, Faculty: d.Faculty})
	}
	s.enrichAvailability(user, refs)
	return r, nil
}
func (s *Service) Lecture(user string, year int, code string) (*LectureDetails, error) {
	sy, err := s.Syllabus(year, code)
	if err != nil {
		return nil, err
	}
	r := &LectureDetails{Syllabus: sy, Availability: publicAvailability(sy.Quota, sy.URL)}
	s.enrichAvailability(user, []lectureRef{{Year: sy.Year, Code: sy.LectureCode, Term: sy.Term, Slots: sy.Slots, Availability: &r.Availability, Faculty: sy.Faculty}})
	return r, nil
}
func (s *Service) DescribeLecturesForUser(user string, year int, codes []string) ([]LectureDigest, error) {
	ds, err := s.DescribeLectures(year, codes)
	if err != nil {
		return nil, err
	}
	var refs []lectureRef
	for i := range ds {
		d := &ds[i]
		d.Availability = publicAvailability(d.Quota, d.URL)
		if d.Error == "" {
			refs = append(refs, lectureRef{Year: d.Year, Code: d.LectureCode, Term: d.Term, Slots: d.Slots, Availability: &d.Availability, Faculty: d.Faculty})
		}
	}
	s.enrichAvailability(user, refs)
	return ds, nil
}
