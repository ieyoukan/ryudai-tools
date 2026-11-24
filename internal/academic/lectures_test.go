package academic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
	"github.com/ieyoukan/daigaku-hack/internal/provider/university"
)

func TestConfirmedRegistrationStillReadsCandidateCounts(t *testing.T) {
	reg, err := os.ReadFile("../provider/kyomu/testdata/regist_list.html")
	if err != nil {
		t.Fatal(err)
	}
	cands, err := os.ReadFile("../provider/kyomu/testdata/candidates.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		faculty string
		scope   kyomu.CandidateScope
	}{
		{"共通教育等科目", kyomu.ScopeGeneral},
		{"工学部", kyomu.ScopeOwn},
	} {
		t.Run(tc.faculty, func(t *testing.T) {
			requests := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("candidate lookup changed data: %s", r.Method)
				}
				switch {
				case strings.Contains(r.URL.Path, "RegistEditStart.aspx"):
					http.Redirect(w, r, "/StudentApp/Regist/RegistListConfirm.aspx", http.StatusFound)
				case strings.Contains(r.URL.Path, "RegistListConfirm.aspx"):
					w.Write(reg)
				case strings.Contains(r.URL.Path, "RegistSearchResults.aspx"):
					requests++
					if (r.URL.Query().Get("fac_mode") == "1") != (tc.scope == kyomu.ScopeOwn) {
						t.Errorf("unexpected first scope: %s", r.URL)
					}
					w.Write(cands)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()
			s := New(ts.URL, nil)
			s.users["alice"] = &userSession{cookies: []*http.Cookie{{Name: "session", Value: "alice"}}}
			s.profileCache.set("alice", &kyomu.StudentProfile{Faculty: "工学部"})
			s.syllabusCache.set("2026/990000101", &kyomu.Syllabus{Year: 2026, LectureCode: "990000101", Faculty: tc.faculty, Term: "後学期", Slots: []kyomu.Slot{{Day: "月", Period: 2}}})
			l, err := s.Lecture("alice", 2026, "990000101")
			if err != nil {
				t.Fatal(err)
			}
			a := l.Availability
			if a.Status != "live" || a.Registered == nil || *a.Registered != 49 || a.SeatsLeft == nil || *a.SeatsLeft != 71 || a.Addable == nil || *a.Addable {
				t.Fatalf("confirmation blocked counts or claimed editing was allowed: %+v", a)
			}
			res, err := s.FindCandidatesContext(context.Background(), "alice", CandidateQuery{Slots: []kyomu.Slot{{Day: "月", Period: 2}}, Scopes: []kyomu.CandidateScope{tc.scope}, Keyword: "試験心理学", DeferDetails: true})
			if err != nil || len(res.Candidates) != 1 {
				t.Fatalf("candidate lookup failed: %+v %v", res, err)
			}
			v := res.Candidates[0]
			if res.RegistrationOpen || v.Addable || v.Registered != 49 || v.Source == "" || !v.FetchedAt.Equal(*a.FetchedAt) || requests != 1 {
				t.Fatalf("candidate lookup lost counts or cache provenance: %+v; requests=%d", res, requests)
			}
		})
	}
}

func TestAvailabilityMergesByYearAndCodeWithoutPublicCacheLeak(t *testing.T) {
	reg, e := os.ReadFile("../provider/kyomu/testdata/regist_edit.html")
	if e != nil {
		t.Fatal(e)
	}
	cands, e := os.ReadFile("../provider/kyomu/testdata/candidates.html")
	if e != nil {
		t.Fatal(e)
	}
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "RegistEditStart.aspx"):
			w.Write(reg)
		case strings.Contains(r.URL.Path, "RegistSearchResults.aspx"):
			requests++
			w.Write(cands)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	s := New(ts.URL, nil)
	s.users["alice"] = &userSession{cookies: []*http.Cookie{{Name: "session", Value: "alice"}}}
	sy := &kyomu.Syllabus{Year: 2026, LectureCode: "990000101", Title: "試験心理学", Quota: "100", Term: "後学期", Slots: []kyomu.Slot{{Day: "月", Period: 2}}, URL: "public"}
	s.syllabusCache.set("2026/990000101", sy)
	l, err := s.Lecture("alice", 2026, "990000101")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Availability
	if a.Status != "live" || a.Capacity == nil || *a.Capacity != 120 || a.Registered == nil || *a.Registered != 49 || a.SeatsLeft == nil || *a.SeatsLeft != 71 || a.FetchedAt == nil {
		t.Fatalf("%+v", a)
	}
	if requests != 1 {
		t.Fatalf("unnecessary slot fetches: %d", requests)
	}
	public, err := s.Lecture("", 2026, "990000101")
	if err != nil {
		t.Fatal(err)
	}
	if public.Availability.Registered != nil || public.Availability.Status != "public_only" || *public.Availability.Capacity != 100 {
		t.Fatal("private registration leaked into public syllabus", public.Availability)
	}
	old := *sy
	old.Year = 2025
	s.syllabusCache.set("2025/990000101", &old)
	l, err = s.Lecture("alice", 2025, "990000101")
	if err != nil || l.Availability.Status != "term_mismatch" || l.Availability.Registered != nil || requests != 1 {
		t.Fatalf("wrong year matched: %+v %v", l, err)
	}
	otherTerm := *sy
	otherTerm.Term = "前学期"
	s.syllabusCache.set("2026/990000101", &otherTerm)
	l, err = s.Lecture("alice", 2026, "990000101")
	if err != nil || l.Availability.Status != "term_mismatch" || requests != 1 {
		t.Fatalf("wrong semester matched: %+v %v", l, err)
	}
}

func TestMissingAvailabilityIsNotZero(t *testing.T) {
	for _, q := range []string{"", "若干名", "0"} {
		a := publicAvailability(q, "source")
		if a.Capacity != nil || a.Registered != nil || a.SeatsLeft != nil {
			t.Fatalf("unknown quota %q: %+v", q, a)
		}
		b, _ := json.Marshal(a)
		if strings.Contains(string(b), `"registered":0`) {
			t.Fatal(string(b))
		}
	}
}

func TestRegisteredLectureMissingFromCandidatesKeepsRegistrationState(t *testing.T) {
	reg, err := os.ReadFile("../provider/kyomu/testdata/regist_edit.html")
	if err != nil {
		t.Fatal(err)
	}
	cands, err := os.ReadFile("../provider/kyomu/testdata/candidates.html")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "RegistEditStart.aspx") {
			w.Write(reg)
		} else if strings.Contains(r.URL.Path, "RegistSearchResults.aspx") {
			w.Write(cands)
		} else {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	s := New(ts.URL, nil)
	s.users["alice"] = &userSession{cookies: []*http.Cookie{{Name: "session", Value: "alice"}}}
	for _, tc := range []struct {
		code, state string
		registered  bool
	}{
		{"990000001", "registered_not_listed", true}, // first choice in the fixture
		{"990000002", "not_found", false},            // second choice, not enrolled
		{"999999999", "not_found", false},
	} {
		sy := &kyomu.Syllabus{Year: 2026, LectureCode: tc.code, Quota: "45", Term: "後学期", Slots: []kyomu.Slot{{Day: "水", Period: 3}}}
		s.syllabusCache.set("2026/"+tc.code, sy)
		l, err := s.Lecture("alice", 2026, tc.code)
		if err != nil {
			t.Fatal(err)
		}
		a := l.Availability
		if a.Status != tc.state || a.RegisteredByUser != tc.registered || a.Registered != nil || a.SeatsLeft != nil || a.Capacity == nil || *a.Capacity != 45 {
			t.Fatalf("%s: %+v", tc.code, a)
		}
		public, err := s.Lecture("", 2026, tc.code)
		if err != nil || public.Availability.RegisteredByUser || public.Availability.Status != "public_only" {
			t.Fatalf("private registration leaked: %+v %v", public, err)
		}
	}
}

func TestFusionGroupsRequireOneCourseAndDeduplicateCredits(t *testing.T) {
	rule := &university.FusionRule{MinimumCredits: 4, SameCourse: true}
	cs := []fusionCredit{{"機械991", "機械導入", "earned", 2}, {"電気991", "電気導入", "earned", 2}, {"機械991", "再履修", "registered", 2}}
	gs, unknown := fusionGroups(rule, "知能情報コース", cs)
	if len(gs) != 2 || len(unknown) != 0 {
		t.Fatalf("%+v %v", gs, unknown)
	}
	for _, g := range gs {
		if g.Earned != 2 || g.Registered != 0 || g.ShortfallAfterPlan != 2 {
			t.Fatal("mixed courses/retake counted:", g)
		}
	}
	cs = append(cs, fusionCredit{"機械983", "流体機械学", "added", 2}, fusionCredit{"知能984", "自コース科目", "added", 2}, fusionCredit{"不明", "未分類", "earned", 2})
	gs, unknown = fusionGroups(rule, "知能情報コース", cs)
	if len(unknown) != 2 || gs[0].Course != "機械工学コース" || gs[0].Earned != 2 || gs[0].Added != 2 || gs[0].ShortfallAfterPlan != 0 {
		t.Fatal(fmt.Sprint(gs, unknown))
	}
}

func TestMultiPeriodCourseCreditsCountOnce(t *testing.T) {
	reg := &kyomu.Registration{Lectures: []kyomu.RegisteredLecture{
		{LectureCode: "123456789", Title: "演習", Rank: 1, Day: "火", Period: 1, Credits: 2},
		{LectureCode: "123456789", Title: "演習", Rank: 1, Day: "火", Period: 2, Credits: 2},
	}}
	if o := buildOverview(reg); o.FirstChoiceCredit != 2 {
		t.Fatal("double counted timetable cells", o.FirstChoiceCredit)
	}
	if p := projectTerm(reg, nil, nil, nil); p.RegisteredCredits != 2 {
		t.Fatal("double counted projected credits", p.RegisteredCredits)
	}
}
