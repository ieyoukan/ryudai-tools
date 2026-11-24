package kyomu

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func scheduleFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/schedule_week.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestScheduleWeekActualDatesAndRowspans(t *testing.T) {
	p, _ := parseHTML("https://example.test/portal/StudentApp/Top.aspx", scheduleFixture(t))
	d := time.Date(2026, 10, 22, 0, 0, 0, 0, japan)
	w, err := parseScheduleWeek(p, d)
	if err != nil {
		t.Fatal(err)
	}
	if w.StartDate != "2026-10-19" || w.EndDate != "2026-10-25" || len(w.Events) != 4 {
		t.Fatalf("week: %+v", w)
	}
	for _, e := range w.Events {
		if e.Start.Format(time.RFC3339) != e.Date+"T08:30:00+09:00" || e.End.Format("15:04") != "10:00" {
			t.Errorf("wrong actual time: %+v", e)
		}
		if e.Room != "試験棟101" || e.Instructors != "（試験 教員）" || e.Theme != "第1回" || e.Note != "連絡事項" || e.ID == "" {
			t.Errorf("lost details: %+v", e)
		}
		if e.Date == "2026-10-22" {
			t.Fatal("fabricated lecture on empty Thursday")
		}
		if (e.Date == "2026-10-21") != e.Cancelled {
			t.Errorf("cancellation: %+v", e)
		}
	}
	if w.Events[1].Date != "2026-10-20" || w.Events[2].Date != "2026-10-20" {
		t.Fatal("overlapping lectures were lost")
	}
}

func TestScheduleWeekYearBoundaryAndLayoutErrors(t *testing.T) {
	body := scheduleFixture(t)
	for i := 0; i < 7; i++ {
		d := time.Date(2026, 12, 28+i, 0, 0, 0, 0, japan)
		body = strings.Replace(body, fmt.Sprintf("10／%d（%s）", 19+i, []string{"月", "火", "水", "木", "金", "土", "日"}[i]), fmt.Sprintf("%d／%d（%s）", d.Month(), d.Day(), []string{"日", "月", "火", "水", "木", "金", "土"}[d.Weekday()]), 1)
	}
	p, _ := parseHTML("https://example.test/Top.aspx", body)
	w, err := parseScheduleWeek(p, time.Date(2027, 1, 1, 0, 0, 0, 0, japan))
	if err != nil || w.StartDate != "2026-12-28" || w.EndDate != "2027-01-03" {
		t.Fatalf("year boundary: %+v, %v", w, err)
	}
	for _, bad := range []string{"<html>login</html>", strings.Replace(body, "tblKoma", "changed", -1), strings.Replace(body, "12／29（火）", "12／30（火）", 1), strings.ReplaceAll(strings.ReplaceAll(body, "08:00", "unknown"), "09:00", "unknown")} {
		p, _ := parseHTML("https://example.test/Top.aspx", bad)
		if _, err := parseScheduleWeek(p, time.Date(2027, 1, 1, 0, 0, 0, 0, japan)); err == nil {
			t.Fatal("malformed page accepted as empty schedule")
		}
	}
}

func TestScheduleReaderJumpPreservesSplitViewState(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method == http.MethodPost {
			r.ParseForm()
			for key, want := range map[string]string{"__VIEWSTATE": "state-0", "__VIEWSTATE1": "state-1", "__VIEWSTATEFIELDCOUNT": "2", "__EVENTVALIDATION": "validation", "ctl00$phContents$ucScheduleWeek$dsJump$txtDate": "2026/11/02", "ctl00$phContents$ucScheduleWeek$ctl00$btnJump": "指定した日付へジャンプ"} {
				if r.Form.Get(key) != want {
					t.Errorf("%s=%q", key, r.Form.Get(key))
				}
			}
			body := scheduleFixture(t)
			for i := 0; i < 7; i++ {
				body = strings.Replace(body, fmt.Sprintf("10／%d（%s）", 19+i, []string{"月", "火", "水", "木", "金", "土", "日"}[i]), fmt.Sprintf("11／%d（%s）", 2+i, []string{"月", "火", "水", "木", "金", "土", "日"}[i]), 1)
			}
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, scheduleFixture(t))
	}))
	defer srv.Close()
	c, _ := New(srv.URL + "/portal")
	w, err := c.ScheduleReader().Week(context.Background(), time.Date(2026, 11, 2, 0, 0, 0, 0, japan))
	if err != nil || w.StartDate != "2026-11-02" || requests != 2 {
		t.Fatalf("jump: %+v, %v; requests=%d", w, err, requests)
	}
}

func TestScheduleReaderSessionExpiryAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/portal/Login.aspx" {
			http.Redirect(w, r, "/portal/Login.aspx", http.StatusFound)
			return
		}
		fmt.Fprint(w, "login")
	}))
	defer srv.Close()
	c, _ := New(srv.URL + "/portal")
	if _, err := c.ScheduleReader().Week(context.Background(), time.Now()); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expiry: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ScheduleReader().Week(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestLiveSchedule(t *testing.T) {
	c := liveLogin(t)
	reader := c.ScheduleReader()
	for _, date := range []string{time.Now().In(japan).Format("2006-01-02"), "2026-11-03", "2026-12-31"} {
		d, _ := time.ParseInLocation("2006-01-02", date, japan)
		w, err := reader.Week(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		if date < w.StartDate || date > w.EndDate {
			t.Fatalf("wrong date: %s, %s", w.StartDate, w.EndDate)
		}
		t.Logf("%s〜%s: %d events", w.StartDate, w.EndDate, len(w.Events))
		for _, e := range w.Events {
			if !e.Start.Before(e.End) || e.Title == "" {
				t.Fatal("invalid event")
			}
		}
	}
}
