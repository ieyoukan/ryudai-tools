package academic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
)

func TestDeferredCandidatesKeepCountsFiltersAndConflictsWithoutEnrichment(t *testing.T) {
	reg, err := os.ReadFile("../provider/kyomu/testdata/regist_edit.html")
	if err != nil {
		t.Fatal(err)
	}
	cands, err := os.ReadFile("../provider/kyomu/testdata/candidates.html")
	if err != nil {
		t.Fatal(err)
	}
	candidateCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "RegistEditStart.aspx"):
			w.Write(reg)
		case strings.Contains(r.URL.Path, "RegistSearchResults.aspx"):
			candidateCalls++
			w.Write(cands)
		default:
			t.Errorf("fast response made an enrichment request: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	s := New(ts.URL, nil)
	s.users["alice"] = &userSession{cookies: []*http.Cookie{{Name: "session", Value: "alice"}}}
	s.profileCache.set("alice", &kyomu.StudentProfile{EnterYear: 2024, Faculty: "工学部", Course: "知能情報コース"})
	q := CandidateQuery{Slots: []kyomu.Slot{{Day: "月", Period: 2}}, Scopes: []kyomu.CandidateScope{kyomu.ScopeGeneral}, DeferDetails: true, WithSyllabus: true, IncludeFull: true, Keyword: "試験心理学"}
	first, err := s.FindCandidatesContext(context.Background(), "alice", q)
	if err != nil || len(first.Candidates) != 1 {
		t.Fatalf("%+v %v", first, err)
	}
	v := first.Candidates[0]
	if v.Syllabus != nil || v.Timetable != nil || v.Registered != 49 || v.Capacity != 120 || v.SeatsLeft == nil || *v.SeatsLeft != 71 || !v.FitsFreeSlots {
		t.Fatal("fast result lost counts or added slow enrichment", v)
	}
	again, err := s.FindCandidatesContext(context.Background(), "alice", q)
	if err != nil || candidateCalls != 1 || !reflect.DeepEqual(first.Candidates, again.Candidates) {
		t.Fatal("candidate cache not reused", err, candidateCalls)
	}
	// Default callers (including MCP) still receive full information.
	s.syllabusCache.set("2026/990000101", &kyomu.Syllabus{Year: 2026, LectureCode: "990000101", Outline: "授業内容"})
	for _, code := range []string{"990000001", "990000003", "990000004"} {
		s.syllabusCache.set("2026/"+code, &kyomu.Syllabus{Room: "教室"})
	}
	s.University.HTTP.Transport = requiredTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<a href="/2026/">2026年度 学部生用 後学期 配当表</a>`)), Request: r}, nil
	})
	q.DeferDetails = false
	full, err := s.FindCandidatesContext(context.Background(), "alice", q)
	if err != nil || len(full.Candidates) != 1 || full.Candidates[0].Syllabus == nil || full.Candidates[0].Syllabus.Outline != "授業内容" || full.Candidates[0].Registered != v.Registered || len(full.Errors) == 0 {
		t.Fatalf("full enrichment or warnings lost: %+v %v", full, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.FindCandidatesContext(ctx, "alice", q); err == nil {
		t.Fatal("cancelled candidate search continued")
	}
}
