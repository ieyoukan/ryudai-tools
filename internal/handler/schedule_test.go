package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
)

type scheduleCookies struct{}

func (scheduleCookies) KyomuCookies(string) ([]*http.Cookie, error) {
	return []*http.Cookie{{Name: "session", Value: "fixture"}}, nil
}
func (scheduleCookies) SaveKyomuCookies(string, []*http.Cookie) error { return nil }
func (scheduleCookies) DeleteKyomuCookies(string) error               { return nil }

func TestScheduleAPIAuthenticationValidationAndExport(t *testing.T) {
	body, err := os.ReadFile("../provider/kyomu/testdata/schedule_week.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	svc := academic.New(srv.URL+"/portal", nil)
	svc.Cookies = scheduleCookies{}
	for _, tc := range []struct {
		user, path string
		status     int
		ics        bool
	}{
		{"", "/api/me/schedule", 401, false},
		{"", "/api/me/schedule?format=ics", 401, false},
		{"student", "/api/me/schedule?days=abc", 400, false},
		{"student", "/api/me/schedule?days=0", 400, false},
		{"student", "/api/me/schedule?days=32", 400, false},
		{"student", "/api/me/schedule?date=2026-02-30", 400, false},
		{"student", "/api/me/schedule?date=2026-10-22", 200, false},
		{"student", "/api/me/schedule?date=2026-10-19&format=ics", 200, true},
	} {
		h := &AcademicHandler{Svc: svc, User: func(*http.Request) string { return tc.user }}
		w := httptest.NewRecorder()
		h.MySchedule(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s: %d, %s", tc.path, w.Code, w.Body)
		}
		if tc.ics {
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/calendar") || !strings.Contains(w.Body.String(), "BEGIN:VEVENT") || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") {
				t.Fatal("invalid calendar download")
			}
		} else if tc.status == 200 {
			var res academic.Schedule
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatal(err)
			}
			if len(res.Days) != 1 || len(res.Days[0].Events) != 0 {
				t.Fatal("empty date was fabricated")
			}
		}
	}
}

func TestLectureAPI(t *testing.T) {
	body, err := os.ReadFile("../provider/kyomu/testdata/lecture_list.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	svc := academic.New(srv.URL+"/portal", nil)
	svc.Cookies = scheduleCookies{}
	for _, tc := range []struct {
		user, year, code string
		status           int
	}{
		{"", "2026", "TEST40", 401},
		{"student", "abc", "TEST40", 400},
		{"student", "2026", "../x", 400},
		{"student", "2026", "TEST40", 200},
	} {
		h := &AcademicHandler{Svc: svc, User: func(*http.Request) string { return tc.user }}
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("year", tc.year)
		rctx.URLParams.Add("code", tc.code)
		req := httptest.NewRequest(http.MethodGet, "/api/me/lectures/x/y", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		h.MyLecture(w, req)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%+v: %d, %s", tc, w.Code, w.Body)
		}
		if tc.status == 200 {
			var res academic.LectureSchedule
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatal(err)
			}
			if res.WebClassCourseID != "26STEST40" || len(res.Sessions) != 3 || res.Sessions[0].Room != "試験棟1-322" {
				t.Fatalf("response: %+v", res)
			}
		}
	}
}
