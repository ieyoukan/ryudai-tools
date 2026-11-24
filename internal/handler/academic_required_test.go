package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/provider/university"
)

type requiredTransport func(*http.Request) (*http.Response, error)

func (f requiredTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRequiredCoursesAPIUsesRequestedCohort(t *testing.T) {
	pdf, err := os.ReadFile("../provider/university/testdata/required2024_chinot.pdf")
	if err != nil {
		t.Fatal(err)
	}
	s := academic.New("http://unused.invalid", nil)
	s.University.HTTP.Transport = requiredTransport(func(r *http.Request) (*http.Response, error) {
		var body []byte
		switch r.URL.String() {
		case university.HandbookURL:
			body = []byte(`<a href="/2026/">2026年度 学生便覧</a><a href="/2024/">2024年度 学生便覧</a>`)
		case "https://rais.skr.u-ryukyu.ac.jp/2024/":
			body = []byte(`<h2>工学部</h2><a href="chinot.pdf">知能情報コース</a>`)
		case "https://rais.skr.u-ryukyu.ac.jp/2024/chinot.pdf":
			body = pdf
		default:
			t.Errorf("unexpected cohort request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	h := &AcademicHandler{Svc: s}
	w := httptest.NewRecorder()
	h.RequiredCourses(w, httptest.NewRequest(http.MethodGet, "/api/academic/required?enter_year=2024&all_courses=1", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("response %d %s", w.Code, w.Body)
	}
	var catalog university.RequiredCatalog
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.EnterYear != 2024 || len(catalog.Courses) != 1 || len(catalog.Courses[0].Subjects) != 31 {
		t.Fatal(catalog)
	}
}

func TestRequiredAPIsRejectInvalidYearsAndUnauthenticatedPersonalAccess(t *testing.T) {
	// A nil service proves these requests are rejected before upstream access.
	for _, tc := range []struct {
		name, path, user string
		personal         bool
		status           int
	}{
		{"anonymous missing year", "/?", "", false, 400},
		{"malformed public year", "/?enter_year=abc", "", false, 400},
		{"malformed override", "/?enter_year=abc", "student", true, 400},
		{"zero override", "/?enter_year=0", "student", true, 400},
		{"out of range override", "/?enter_year=1999", "student", true, 400},
		{"future override", "/?enter_year=9999", "student", true, 400},
		{"personal requires auth", "/?enter_year=2024", "", true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &AcademicHandler{User: func(*http.Request) string { return tc.user }}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.personal {
				h.MyRequiredCourses(w, r)
			} else {
				h.RequiredCourses(w, r)
			}
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("response %d %s", w.Code, w.Body)
			}
		})
	}
}
