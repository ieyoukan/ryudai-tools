package university

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTimetablePDFCells(t *testing.T) {
	rows, err := ParseTimetablePDF(fixture(t, "fusion2026.pdf"), Document{Title: "工学融合科目/選択科目"}, Source{URL: "https://example.test/source.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d courses", len(rows))
	}
	byCode := map[string]TimetableCourse{}
	for _, r := range rows {
		byCode[r.LectureCode] = r
	}
	r := byCode["990100001"]
	if r.Title != "試験流体演習" || r.NumberingCode != "機械983" || r.Capacity == nil || *r.Capacity != 20 || r.Credits == nil || *r.Credits != 2 || r.DayPeriod != "火4" {
		t.Fatalf("wrong columns: %+v", r)
	}
	if !strings.Contains(r.Notes, "基礎演習を受講済み") || !strings.Contains(r.Notes, "未受講者は履修不可") {
		t.Fatal("multi-line restrictions lost:", r.Notes)
	}
	if strings.Contains(byCode["990100002"].Notes, "基礎演習を受講済み") {
		t.Fatal("notes leaked across rows")
	}
	r = byCode["990100003"]
	if r.Page != 2 || r.DayPeriod != "火4～5" || r.Period != "第2クォータ" {
		t.Fatalf("multi-period/quarter: %+v", r)
	}
	rows, err = ParseTimetablePDF(fixture(t, "course2026.pdf"), Document{Title: "知能情報コース"}, Source{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.LectureCode == "990100004" {
			found = true
			if r.Capacity == nil || *r.Capacity != 75 || !strings.Contains(r.Notes, "対象年度の学生のみ") {
				t.Fatalf("cohort restrictions: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("course timetable not parsed")
	}
}

func TestAutumnPDFWithPositionedCharacters(t *testing.T) {
	for _, file := range []string{"course_autumn2026.pdf", "fusion_intro_autumn2026.pdf"} {
		rows, err := ParseTimetablePDF(fixture(t, file), Document{Title: file}, Source{})
		if err != nil || len(rows) == 0 {
			t.Fatalf("%s: %v", file, err)
		}
		if file == "course_autumn2026.pdf" {
			found := false
			for _, r := range rows {
				if r.LectureCode == "990100005" {
					found = true
					if !strings.Contains(r.Notes, "同時に履修") || r.Capacity == nil || *r.Capacity != 90 {
						t.Fatalf("co-requisite lost: %+v", r)
					}
				}
			}
			if !found {
				t.Fatal("engineering design course missing")
			}
		}
	}
}

func TestOfficialLinkDiscovery(t *testing.T) {
	index := `<div id="content"><a href="?page_id=1">【前学期】</a></div><aside><a href="?page_id=1">≪学部生用≫令和8（2026）年度【前学期】授業時間配当表</a><a href="?page_id=2">≪大学院生用≫令和8（2026）年度【前学期】時間割配当表</a></aside>`
	c := New()
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(index)), Request: r}, nil
	})
	u, _, e := c.yearPage(context.Background(), TimetableURL, 2026, "前学期")
	if e != nil || u != "https://rais.skr.u-ryukyu.ac.jp/dc/?page_id=1" {
		t.Fatalf("%s %v", u, e)
	}
	if _, _, e = c.yearPage(context.Background(), TimetableURL, 2027, "前学期"); e == nil {
		t.Fatal("must not substitute another year")
	}
	ds, e := documents([]byte(`<div id="content"><div class="su-spoiler-title">工学部</div><a href="../wordpress/fusion.pdf">工学融合</a><div class="su-spoiler-title">農学部</div><a href="../wordpress/agri.pdf">共通</a></div>`), TimetableURL)
	if e != nil || len(ds) != 2 || ds[0].Faculty != "工学部" || ds[1].Faculty != "農学部" || ds[0].URL != "https://rais.skr.u-ryukyu.ac.jp/wordpress/fusion.pdf" {
		t.Fatalf("%+v %v", ds, e)
	}
}

func TestCacheKeepsSourceOnRefreshFailure(t *testing.T) {
	c := New()
	calls := 0
	c.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			return nil, fmt.Errorf("offline")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("published")), Request: r}, nil
	})
	ctx := context.Background()
	_, first, err := c.fetch(ctx, CalendarURL)
	if err != nil {
		t.Fatal(err)
	}
	old := c.cache[CalendarURL]
	old.retryAt = time.Now().Add(-time.Second)
	c.cache[CalendarURL] = old
	b, src, err := c.fetch(ctx, CalendarURL)
	if err != nil || string(b) != "published" || !src.Stale || src.Warning == "" || !src.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("%s %+v %v", b, src, err)
	}
	c.fetch(ctx, CalendarURL)
	if calls != 2 {
		t.Fatal("failed refresh is retried too often")
	}
	if _, _, e := c.fetch(ctx, "https://example.org/source"); e == nil {
		t.Fatal("arbitrary host allowed")
	}
	if _, e := officialURL(CalendarURL, "http://www.u-ryukyu.ac.jp:80/foo"); e == nil {
		t.Fatal("unapproved port allowed")
	}
}

func TestFusionRuleKeepsConditionsAndSource(t *testing.T) {
	for _, tc := range []struct {
		text  string
		intro bool
	}{
		{"※7 1つのコースの工学融合科目から4単位以上を履修する。", false},
		{"工学融合科目群のうち，１つのコースから選択必修を含む４単位以上を履修する。", true},
	} {
		p := Passage{Document: "2024年度 知能情報コース", Page: 11, Text: tc.text, Source: Source{URL: "official.pdf"}}
		r := fusionRuleFromPassage(p)
		if r == nil || r.MinimumCredits != 4 || !r.SameCourse || r.RequiresIntro != tc.intro || r.Evidence.Page != 11 || r.Evidence.Source.URL != "official.pdf" {
			t.Fatalf("%+v", r)
		}
	}
	if r := fusionRuleFromPassage(Passage{Text: "1つのコースから4単位以上"}); r != nil {
		t.Fatal("unrelated rule classified as engineering fusion")
	}
}
