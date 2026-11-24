package university

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Event struct {
	Title     string `json:"title"`
	Start     string `json:"start"`
	End       string `json:"end,omitempty"` // inclusive; empty means the end was not published
	Kind      string `json:"kind"`
	DateLabel string `json:"date_label"`
	OpenEnded bool   `json:"open_ended,omitempty"`
}

type Calendar struct {
	Date             string   `json:"date"`
	AcademicYear     int      `json:"academic_year"`
	Timezone         string   `json:"timezone"`
	Covered          bool     `json:"covered"`
	Semester         string   `json:"semester,omitempty"`
	Quarter          int      `json:"quarter,omitempty"`
	ClassesInSession *bool    `json:"classes_in_session,omitempty"`
	EffectiveWeekday string   `json:"effective_weekday,omitempty"`
	Active           []Event  `json:"active"`
	Upcoming         []Event  `json:"upcoming"`
	Events           []Event  `json:"events,omitempty"`
	Source           Source   `json:"source"`
	Notes            []string `json:"notes"`
}

var mdRE = regexp.MustCompile(`(\d{1,2})月(\d{1,2})日`)
var academicYearRE = regexp.MustCompile(`(20\d{2})年度学年暦`)
var calendarYearRE = regexp.MustCompile(`^(20\d{2})年$`)
var makeupRE = regexp.MustCompile(`日[・･]([月火水木金土日])曜`)

func eventKind(t string) string {
	switch {
	case strings.Contains(t, "登録調整期間"):
		return "registration_adjustment"
	case strings.Contains(t, "仮登録") && strings.Contains(t, "授業科目"):
		return "preregistration"
	case strings.Contains(t, "履修中止"):
		return "withdrawal"
	case strings.Contains(t, "定期試験期間"):
		return "exams"
	case strings.Contains(t, "休業"):
		return "vacation"
	case strings.Contains(t, "授業振替日"):
		return "makeup"
	case strings.Contains(t, "休講"):
		return "no_classes"
	default:
		return "event"
	}
}

func calendarEvents(data []byte, wanted int) ([]Event, error) {
	root, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	ay, year := 0, 0
	var out []Event
	seen := map[string]bool{}
	walk(root, func(n *html.Node) {
		if n.Data == "h2" {
			if m := academicYearRE.FindStringSubmatch(compact(textOf(n))); m != nil {
				ay, _ = strconv.Atoi(m[1])
				year = ay
			}
		}
		if n.Data != "tr" || ay != wanted {
			return
		}
		var cells []string
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Data == "th" || ch.Data == "td" {
				cells = append(cells, compact(textOf(ch)))
			}
		}
		if len(cells) == 1 {
			if m := calendarYearRE.FindStringSubmatch(cells[0]); m != nil {
				year, _ = strconv.Atoi(m[1])
			}
			return
		}
		if len(cells) != 2 {
			return
		}
		md := mdRE.FindAllStringSubmatch(cells[0], -1)
		if len(md) == 0 {
			return
		}
		var dates []string
		y := year
		for i, m := range md {
			month, _ := strconv.Atoi(m[1])
			day, _ := strconv.Atoi(m[2])
			if i > 0 && month < mustInt(md[i-1][1]) {
				y++
			}
			d := fmt.Sprintf("%04d-%02d-%02d", y, month, day)
			if _, e := time.Parse("2006-01-02", d); e != nil {
				return
			}
			dates = append(dates, d)
		}
		title := strings.TrimSuffix(cells[1], "(再掲)")
		add := func(start, end string, open bool) {
			key := title + start + end
			if seen[key] {
				return
			}
			seen[key] = true
			out = append(out, Event{Title: title, Start: start, End: end, Kind: eventKind(title), DateLabel: cells[0], OpenEnded: open})
		}
		if strings.ContainsAny(cells[0], "～〜") {
			if len(dates) == 1 {
				add(dates[0], "", true)
			} else {
				add(dates[0], dates[len(dates)-1], false)
			}
		} else {
			for _, d := range dates {
				add(d, d, false)
			}
		}
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("%d 年度の学年暦は掲載されていません", wanted)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out, nil
}
func mustInt(s string) int { n, _ := strconv.Atoi(s); return n }

func (c *Client) Calendar(ctx context.Context, date string, days int, all bool) (*Calendar, error) {
	if date == "" {
		date = time.Now().In(Japan).Format("2006-01-02")
	}
	d, err := time.ParseInLocation("2006-01-02", date, Japan)
	if err != nil {
		return nil, fmt.Errorf("date は YYYY-MM-DD で指定してください")
	}
	if days == 0 {
		days = 30
	}
	if days < 1 || days > 366 {
		return nil, fmt.Errorf("days は 1〜366 で指定してください")
	}
	ay := d.Year()
	if d.Month() < time.April {
		ay--
	}
	data, src, err := c.fetch(ctx, CalendarURL)
	if err != nil {
		return nil, err
	}
	events, err := calendarEvents(data, ay)
	if err != nil {
		return nil, err
	}
	res := calendarAt(events, date, days, ay)
	res.Source = src
	if all {
		res.Events = events
	}
	return res, nil
}

func calendarAt(events []Event, date string, days, ay int) *Calendar {
	r := &Calendar{Date: date, AcademicYear: ay, Timezone: "Asia/Tokyo", Covered: true, Active: []Event{}, Upcoming: []Event{}, Notes: []string{
		"全学共通の学年暦です。医学部医学科・研究科は別日程の場合があります。",
		"登録の可否は本人の履修登録画面を優先してください。試験期間に講義・補講が行われる場合もあります。",
		"クォーター科目の試験は最終授業時です。科目別の予定はシラバス・担当教員の案内を確認してください。",
		"予定は指定日の属する年度分です。翌年度の予定は翌年度の日付を指定して確認してください。",
	}}
	d, _ := time.Parse("2006-01-02", date)
	until := d.AddDate(0, 0, days).Format("2006-01-02")
	// Semester and quarter spans come from explicit start/end events, not month heuristics.
	for _, term := range []string{"前学期", "後学期"} {
		start, end := "", ""
		for _, e := range events {
			if e.Title == term+"開始" || e.Title == "学年及び"+term+"開始" {
				start = e.Start
			}
			if e.Title == term+"終了" || e.Title == "学年及び"+term+"終了" {
				end = e.Start
			}
		}
		if start != "" && end != "" && start <= date && date <= end {
			r.Semester = term
		}
	}
	for q := 1; q <= 4; q++ {
		label := fmt.Sprintf("第%dクォーター", q)
		start, end := "", ""
		for _, e := range events {
			if !strings.Contains(e.Title, label) {
				continue
			}
			if strings.Contains(e.Title, "授業開始") {
				start = e.Start
			}
			if strings.Contains(e.Title, "授業終了") {
				end = e.Start
			}
		}
		if start != "" && end != "" && start <= date && date <= end {
			r.Quarter = q
			v := true
			r.ClassesInSession = &v
		}
	}
	for _, e := range events {
		if e.Start > date && e.Start <= until {
			r.Upcoming = append(r.Upcoming, e)
		}
		// An unknown end is never treated as an infinite confirmed interval.
		if e.End == "" || e.Start > date || e.End < date {
			continue
		}
		excluded := false
		if strings.Contains(e.Title, "を除く") {
			for _, m := range mdRE.FindAllStringSubmatch(e.Title, -1) {
				if d.Month() == time.Month(mustInt(m[1])) && d.Day() == mustInt(m[2]) {
					excluded = true
				}
			}
		}
		if excluded {
			continue
		}
		r.Active = append(r.Active, e)
		if e.Kind == "vacation" || e.Kind == "no_classes" {
			v := false
			r.ClassesInSession = &v
		}
		if e.Kind == "makeup" {
			if m := makeupRE.FindStringSubmatch(e.Title); m != nil {
				r.EffectiveWeekday = m[1]
			}
		}
	}
	return r
}
