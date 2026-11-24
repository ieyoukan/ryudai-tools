package kyomu

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func lectureFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/lecture_list.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseLectureSchedule(t *testing.T) {
	p, _ := parseHTML("https://example.test/portal/StudentApp/Lct/LectureList.aspx", lectureFixture(t))
	ls, err := parseLectureSchedule(p)
	if err != nil {
		t.Fatal(err)
	}
	if ls.Year != 2026 || ls.LectureCode != "TEST40" || ls.NumberCode != "試験326" || ls.Title != "試験科目40" || ls.Instructors != "試験 教員" || ls.DayPeriod != "木4" || ls.Term != "後期" || ls.Credits != "2" {
		t.Fatalf("header: %+v", ls)
	}
	if len(ls.Sessions) != 3 {
		t.Fatalf("sessions: %+v", ls.Sessions)
	}
	s := ls.Sessions[0]
	if s.No != 1 || s.Date != "2026-10-01" || s.Period != "4限" || s.StartTime != "14:40" || s.EndTime != "16:10" || s.Room != "試験棟1-322" || s.Cancelled {
		t.Errorf("first: %+v", s)
	}
	if !ls.Sessions[1].Cancelled || ls.Sessions[1].Note != "休講" {
		t.Errorf("cancelled: %+v", ls.Sessions[1])
	}
	last := ls.Sessions[2]
	if last.Date != "2027-01-07" || last.EndTime != "17:50" || last.Note != "教室変更" || last.Instructors != "試験 教員、試験 助手" || last.Cancelled {
		t.Errorf("last: %+v", last)
	}
}

func TestParseLectureScheduleRejectsOtherPages(t *testing.T) {
	p, _ := parseHTML("https://example.test/x", "<html><body>エラー</body></html>")
	if _, err := parseLectureSchedule(p); err == nil {
		t.Fatal("missing header must be an error")
	}
	body := strings.Replace(lectureFixture(t), "<th scope=\"col\">教室名</th>", "<th scope=\"col\">場所</th>", 1)
	p, _ = parseHTML("https://example.test/x", body)
	if _, err := parseLectureSchedule(p); err == nil {
		t.Fatal("changed columns must be an error, not a silently wrong room")
	}
}

func TestLectureScheduleRequestsLecturePage(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path + "?" + r.URL.RawQuery
		w.Write([]byte(lectureFixture(t)))
	}))
	defer srv.Close()
	c, err := New(srv.URL + "/portal")
	if err != nil {
		t.Fatal(err)
	}
	ls, err := c.LectureSchedule(2026, "TEST40")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/portal/StudentApp/Lct/LectureList.aspx?lct_cd=TEST40&lct_year=2026" || !strings.HasSuffix(ls.Source, "lct_cd=TEST40&lct_year=2026") {
		t.Fatalf("request %q source %q", got, ls.Source)
	}
	if _, err := c.LectureSchedule(2026, "OTHER"); err == nil {
		t.Fatal("a page for another lecture must be rejected")
	}
}
