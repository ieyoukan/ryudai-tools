package academic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

func TestScheduleReusesLoginResponse(t *testing.T) {
	fixture, err := os.ReadFile("../provider/kyomu/testdata/schedule_week.html")
	if err != nil {
		t.Fatal(err)
	}
	monday := scheduleMonday(time.Now().In(scheduleZone))
	body := string(fixture)
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		body = strings.Replace(body, fmt.Sprintf("10／%d（%s）", 19+i, []string{"月", "火", "水", "木", "金", "土", "日"}[i]), fmt.Sprintf("%d／%d（%s）", d.Month(), d.Day(), []string{"日", "月", "火", "水", "木", "金", "土"}[d.Weekday()]), 1)
	}
	var tops atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "Login.aspx") {
			if r.Method == http.MethodPost {
				http.Redirect(w, r, "/portal/StudentApp/Top.aspx", http.StatusFound)
				return
			}
			fmt.Fprint(w, `<form action="Login.aspx"><input name="txtID"><input name="txtPassWord"><input id="ctl00_btnLogin" name="btnLogin" type="submit" value="ログイン"></form>`)
			return
		}
		tops.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "ASP.NET_SessionId", Value: "logged-in", Path: "/"})
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	for _, startSession := range []bool{true, false} {
		svc := New(srv.URL+"/portal", func(string) (string, bool) { return "password", true })
		before := tops.Load()
		if startSession {
			if err := svc.StartSession("student", "password"); err != nil {
				t.Fatal(err)
			}
		}
		res, err := svc.MySchedule(context.Background(), "student", monday.Format("2006-01-02"), 7, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Days) != 7 || len(res.Days[0].Events) != 1 || tops.Load() != before+1 {
			t.Fatalf("login response fetched twice: requests=%d, %+v", tops.Load()-before, res)
		}
		if startSession && !res.Cached {
			t.Fatal("login did not warm week cache")
		}
	}
}

func TestScheduleCacheIsolationRefreshAndConcurrentCalls(t *testing.T) {
	body, err := os.ReadFile("../provider/kyomu/testdata/schedule_week.html")
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if fail.Load() {
			http.Error(w, "down", 503)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	svc := New(srv.URL+"/portal", nil)
	svc.Cookies = memCookies{"a": {{Name: "session", Value: "a"}}, "b": {{Name: "session", Value: "b"}}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.MySchedule(context.Background(), "a", "2026-10-22", 1, false)
			if err != nil {
				t.Error(err)
				return
			}
			if len(res.Days) != 1 || len(res.Days[0].Events) != 0 {
				t.Errorf("empty date: %+v", res)
			}
		}()
	}
	wg.Wait()
	initial := count.Load()
	if initial < 1 || initial > 2 {
		t.Fatalf("concurrent requests not coalesced: %d", initial)
	}
	res, err := svc.MySchedule(context.Background(), "a", "2026-10-19", 1, false)
	if err != nil || !res.Cached || len(res.Days[0].Events) != 1 || count.Load() != initial {
		t.Fatalf("same-week cache: %+v, %v, requests=%d", res, err, count.Load())
	}
	if res.Days[0].Events[0].GoogleCalendarURL == "" {
		t.Fatal("missing Google Calendar URL")
	}
	_, err = svc.MySchedule(context.Background(), "b", "2026-10-19", 1, false)
	if err != nil || count.Load() <= initial {
		t.Fatalf("user isolation: %v", err)
	}
	before := count.Load()
	_, err = svc.MySchedule(context.Background(), "a", "2026-10-19", 1, true)
	if err != nil || count.Load() <= before {
		t.Fatalf("refresh: %v", err)
	}
	fail.Store(true)
	if res, err := svc.MySchedule(context.Background(), "a", "2026-10-19", 1, true); err == nil || res != nil {
		t.Fatal("upstream failure disguised as empty schedule")
	}
	fail.Store(false)
	// ログアウト後、同じアカウントでも週キャッシュから個人の予定を返さない。
	svc.Forget("a")
	if _, err := svc.MySchedule(context.Background(), "a", "2026-10-19", 1, false); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("cache survived logout: %v", err)
	}
}

func TestScheduleRangeValidation(t *testing.T) {
	for _, q := range []struct {
		date string
		days int
	}{{"2026-02-30", 1}, {"2026-1-2", 1}, {"yesterday", 1}, {"2026-10-22", -1}, {"2026-10-22", 32}, {"1999-01-01", 1}} {
		if _, _, err := ScheduleRange(q.date, q.days); !errors.Is(err, ErrScheduleQuery) {
			t.Errorf("accepted invalid query %+v", q)
		}
	}
	d, n, err := ScheduleRange("", 0)
	if err != nil || n != 1 || d.Format("2006-01-02") != time.Now().In(scheduleZone).Format("2006-01-02") {
		t.Fatalf("today default: %v %d %v", d, n, err)
	}
}

func TestScheduleCalendarExports(t *testing.T) {
	start := time.Date(2026, 10, 22, 8, 30, 0, 0, scheduleZone)
	e := kyomu.ScheduleEvent{ID: "stable", Title: "講義,演習;" + strings.Repeat("あ", 50) + "\n続き", Date: "2026-10-22", Start: start, End: start.Add(90 * time.Minute), Room: "工1-101", Note: "変更\r\nBEGIN:VEVENT"}
	u, err := url.Parse(googleCalendarURL(e, "https://example.test/portal"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("text") != e.Title || q.Get("dates") != "20261021T233000Z/20261022T010000Z" || q.Get("location") != "工1-101（工学部1号館）" || q.Get("ctz") != "Asia/Tokyo" {
		t.Fatalf("Google link: %v", q)
	}
	s := &Schedule{Source: "https://example.test/portal", Days: []ScheduleDay{{Date: e.Date, FetchedAt: start, Events: []ScheduleItem{{ScheduleEvent: e}}}}}
	ics := ScheduleICS("student", s)
	for _, line := range strings.Split(ics, "\r\n") {
		if len(line) > 75 || !utf8.ValidString(line) {
			t.Fatalf("invalid folded line: %q", line)
		}
	}
	unfolded := strings.ReplaceAll(ics, "\r\n ", "")
	for _, want := range []string{"DTSTART:20261021T233000Z", "DTEND:20261022T010000Z", "SUMMARY:講義\\,演習\\;", "\\n続き", "\\nBEGIN:VEVENT", "STATUS:CONFIRMED", "LOCATION:工1-101（工学部1号館）"} {
		if !strings.Contains(unfolded, want) {
			t.Errorf("ICS missing %q", want)
		}
	}
	if strings.Count(ics, "\r\nBEGIN:VEVENT\r\n") != 1 || strings.Contains(ics, "RRULE") || strings.Contains(ics, "student") {
		t.Fatal("unsafe or recurring ICS")
	}
	if ScheduleICS("student", s) != ics || ScheduleICS("other", s) == ics {
		t.Fatal("UID not stable / user-specific")
	}
	s.Days[0].Events[0].Cancelled = true
	if !strings.Contains(ScheduleICS("student", s), "STATUS:CANCELLED") {
		t.Fatal("lost cancellation")
	}
}
