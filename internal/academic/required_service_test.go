package academic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
	"github.com/ieyoukan/daigaku-hack/internal/provider/university"
)

type requiredTransport func(*http.Request) (*http.Response, error)

func (f requiredTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Keep every request local: the official transport uses synthetic PDF fixtures,
// and any uncached syllabus lookup receives an error from a local server.
func requiredServiceFixture(t *testing.T, brokenPDF bool) *Service {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "syllabus unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(ts.Close)
	s := New(ts.URL, nil)
	files := map[string][]byte{}
	for _, name := range []string{"chinot", "energy", "kenchiku"} {
		b, err := os.ReadFile("../provider/university/testdata/required2024_" + name + ".pdf")
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	s.University.HTTP.Transport = requiredTransport(func(r *http.Request) (*http.Response, error) {
		var b []byte
		switch {
		case r.URL.String() == university.HandbookURL:
			b = []byte(`<a href="/2024/">2024年度 学生便覧</a><a href="/2025/">2025年度 学生便覧</a><a href="/2026/">2026年度 学生便覧</a>`)
		case r.URL.Path == "/2024/" || r.URL.Path == "/2025/":
			b = []byte(`<div id="content"><h2>工学部</h2><a href="chinot.pdf">知能情報コース</a><a href="energy.pdf">エネルギー環境工学コース</a><a href="kenchiku.pdf">建築学コース</a></div>`)
		case strings.HasSuffix(r.URL.Path, ".pdf"):
			name := strings.TrimSuffix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], ".pdf")
			b = files[name]
			if brokenPDF {
				b = []byte("not a PDF")
			}
		default:
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not published")), Request: r}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Request: r}, nil
	})
	for user, course := range map[string]string{"info": "知能情報コース", "architecture": "建築学コース"} {
		s.profileCache.set(user, &kyomu.StudentProfile{EnterYear: 2024, Faculty: "工学部", Department: "工学科", Course: course})
		s.users[user] = &userSession{cookies: []*http.Cookie{{Name: "session", Value: "fixture", Path: "/"}}}
	}
	return s
}

func TestRequiredCatalogUsesStudentCohortAndKeepsUsersSeparate(t *testing.T) {
	s := requiredServiceFixture(t, false)
	ctx := context.Background()
	for _, tc := range []struct {
		user, course string
		count        int
	}{{"info", "知能情報コース", 31}, {"architecture", "建築学コース", 27}} {
		r, err := s.RequiredCourses(ctx, tc.user, 0, "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		if r.EnterYear != 2024 || len(r.Courses) != 1 || r.Courses[0].Course != tc.course || len(r.Courses[0].Subjects) != tc.count {
			t.Fatalf("wrong cohort or user: %+v", r)
		}
		for _, subject := range r.Courses[0].Subjects {
			if !strings.Contains(subject.Source.URL, "/2024/") {
				t.Fatal("current year's source substituted", subject.Source)
			}
		}
	}
	r, err := s.RequiredCourses(ctx, "info", 2025, "", "", false)
	if err != nil || r.EnterYear != 2025 || !strings.Contains(r.Courses[0].Subjects[0].Source.URL, "/2025/") {
		t.Fatalf("explicit cohort ignored: %+v %v", r, err)
	}
	r, err = s.RequiredCourses(ctx, "info", 0, "", "", true)
	if err != nil || r.EnterYear != 2024 || len(r.Courses) != 3 {
		t.Fatalf("all_courses restricted to own course: %+v %v", r, err)
	}
	if _, err := s.RequiredCourses(ctx, "", 0, "", "", false); err == nil {
		t.Fatal("anonymous request silently used current year")
	}
	if _, err := s.RequiredCourses(ctx, "info", 2026, "", "", false); err == nil {
		t.Fatal("unavailable handbook replaced with another cohort")
	}
	rules, err := s.StudyRules(ctx, "info", 0, "", "", "")
	if err != nil || rules.EnterYear != 2024 || rules.Course != "知能情報コース" || len(rules.Documents) != 1 || !strings.Contains(rules.Documents[0].URL, "/2024/") {
		t.Fatalf("study rules used wrong cohort: %+v %v", rules, err)
	}
}

func TestRequiredCheckUsesCohortForRulesAndActualLectureYearForIdentity(t *testing.T) {
	s := requiredServiceFixture(t, false)
	prof, _ := s.profileCache.get("info")
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{{Year: 2025, LectureCode: "old", Title: "試験データベース", Credits: 2, Passed: true}}}
	reg := &kyomu.Registration{YearTerm: "2026年度後期", Lectures: []kyomu.RegisteredLecture{{Rank: 1, LectureCode: "now", Title: "試験デザイン演習", Credits: 2}}}
	s.syllabusCache.set("2025/old", &kyomu.Syllabus{NumberingCode: "知能210"})
	s.syllabusCache.set("2026/now", &kyomu.Syllabus{NumberingCode: "工共302"})
	// Wrong entries in other years detect accidental cohort/current-year lookup.
	s.syllabusCache.set("2024/old", &kyomu.Syllabus{NumberingCode: "工共122"})
	s.syllabusCache.set("2026/old", &kyomu.Syllabus{NumberingCode: "工共122"})
	s.syllabusCache.set("2024/now", &kyomu.Syllabus{NumberingCode: "工共122"})
	r := s.requiredCheck(context.Background(), prof, grades, reg, nil, prof.EnterYear)
	if r.Catalog == nil || r.Catalog.EnterYear != 2024 || len(r.Required) != 31 || !r.GradesAvailable || !r.RegistrationAvailable {
		t.Fatalf("missing required catalog: %+v", r)
	}
	if r.Counts["passed"] != 1 || r.Counts["enrolled"] != 1 || r.Counts["not_passed"] != 29 || r.Counts["unverified"] != 0 {
		t.Fatal(r.Counts)
	}
	for _, row := range r.Required {
		if row.NumberingCode == "知能210" && (row.Status != "passed" || row.Evidence[0].Year != 2025) {
			t.Fatal(row)
		}
		if row.NumberingCode == "工共302" && (row.Status != "enrolled" || row.Evidence[0].Year != 2026) {
			t.Fatal(row)
		}
	}
	// A failed numbering lookup must not turn an absent match into a definitive failure.
	grades.Grades = append(grades.Grades, kyomu.Grade{Year: 2025, LectureCode: "unknown", Title: "名称が変更された科目", Category: "専門教育", Passed: true, Credits: 2})
	r = s.requiredCheck(context.Background(), prof, grades, reg, nil, prof.EnterYear)
	if r.Counts["passed"] != 1 || r.Counts["enrolled"] != 1 || r.Counts["unverified"] != 29 || r.Counts["not_passed"] != 0 {
		t.Fatal("missing numbering treated as definitive failure", r.Counts)
	}
}

func TestRequiredCheckRecognizesRegisteredDesignAndSeminarOutsideRegistrationPeriod(t *testing.T) {
	s := requiredServiceFixture(t, false)
	prof, _ := s.profileCache.get("info")
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{}}
	reg := &kyomu.Registration{YearTerm: "2026年度後期", Open: false, Lectures: []kyomu.RegisteredLecture{
		{Year: 2026, Rank: 1, LectureCode: "6001", Title: "試験デザイン演習 3組", Credits: 2},
		{Year: 2026, Rank: 1, LectureCode: "6002", Title: "試験セミナー 2組", Credits: 1},
		{Year: 2026, Rank: 1, LectureCode: "6003", Title: "試験特別講義I", Credits: 2},
	}}
	s.syllabusCache.set("2026/6001", &kyomu.Syllabus{NumberingCode: "工共302"})
	s.syllabusCache.set("2026/6002", &kyomu.Syllabus{NumberingCode: "工共405"})
	s.syllabusCache.set("2026/6003", &kyomu.Syllabus{NumberingCode: "知能393"})
	r := s.requiredCheck(context.Background(), prof, grades, reg, nil, prof.EnterYear)
	for _, number := range []string{"工共302", "工共405"} {
		found := false
		for _, row := range r.Required {
			if row.NumberingCode == number {
				found = true
				if row.Status != "enrolled" || len(row.Evidence) != 1 || row.Evidence[0].State != "enrolled" {
					t.Fatalf("registered class was not enrolled: %+v", row)
				}
			}
		}
		if !found {
			t.Fatal("required subject missing", number)
		}
	}
	if r.Counts["enrolled"] != 2 {
		t.Fatal(r.Counts)
	}
	r = s.requiredCheck(context.Background(), prof, grades, nil, nil, prof.EnterYear)
	if r.Counts["not_passed"] != 0 || r.Counts["unverified"] != len(r.Required) {
		t.Fatal("unavailable registration was counted as not_passed", r.Counts)
	}
}

func TestRequiredCheckDoesNotClaimCompletionWithoutData(t *testing.T) {
	for _, tc := range []struct {
		name   string
		broken bool
		course string
	}{{"grades unavailable", false, "知能情報コース"}, {"selective candidates", false, "エネルギー環境工学コース"}, {"unreadable PDF", true, "知能情報コース"}, {"course unknown", false, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			s := requiredServiceFixture(t, tc.broken)
			prof := &kyomu.StudentProfile{EnterYear: 2024, Faculty: "工学部", Course: tc.course}
			r := s.requiredCheck(context.Background(), prof, nil, nil, nil, 2024)
			if r.GradesAvailable || r.RegistrationAvailable || r.Counts["passed"] != 0 || r.Counts["not_passed"] != 0 {
				t.Fatal(r)
			}
			if tc.broken || tc.course == "" {
				if r.Catalog != nil || len(r.Required) != 0 || len(r.Notes) == 0 {
					t.Fatal("missing catalog not reported", r)
				}
			} else if tc.course == "知能情報コース" {
				if r.Counts["unverified"] != 31 {
					t.Fatal(r.Counts)
				}
			} else if len(r.Required) != 28 || len(r.SelectiveRequired) != 8 || r.Counts["unverified"] != 28 {
				t.Fatal("selective candidates counted as mandatory", r)
			}
		})
	}
}

func TestCohortToolsRetryWhenProfileSessionExpires(t *testing.T) {
	for _, op := range []string{"catalog", "status", "rules"} {
		t.Run(op, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "RichTimeOut.aspx") {
					fmt.Fprint(w, "session expired")
					return
				}
				http.Redirect(w, r, "/RichTimeOut.aspx", http.StatusFound)
			}))
			defer ts.Close()
			s := New(ts.URL, nil)
			s.users["expired"] = &userSession{cookies: []*http.Cookie{{Name: "session", Value: "expired", Path: "/"}}}
			var err error
			switch op {
			case "catalog":
				_, err = s.RequiredCourses(context.Background(), "expired", 0, "", "", false)
			case "status":
				_, err = s.RequiredCourseStatus(context.Background(), "expired", 0)
			case "rules":
				_, err = s.StudyRules(context.Background(), "expired", 0, "", "", "")
			}
			// No credentials are configured in this test, so the retry must reach
			// login and return ErrNoCredentials instead of hiding profile expiry.
			if !errors.Is(err, ErrNoCredentials) || s.users["expired"].cookies != nil {
				t.Fatalf("session renewal not attempted: %v", err)
			}
		})
	}
}
