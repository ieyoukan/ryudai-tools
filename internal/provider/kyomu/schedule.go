package kyomu

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const schedulePath = "StudentApp/Top.aspx"
const weekGrid = "ctl00_phContents_ucScheduleWeek_gv"

var japan = time.FixedZone("Asia/Tokyo", 9*60*60)
var scheduleDate = regexp.MustCompile(`^(\d{1,2})[／/](\d{1,2})[（(]([月火水木金土日])[）)]`)
var clockText = regexp.MustCompile(`^\d{2}:\d{2}$`)

// ScheduleEvent は曜日の繰り返しではなく、ポータルの指定日に実在する予定。
type ScheduleEvent struct {
	ID          string    `json:"id"`
	Date        string    `json:"date"`
	Title       string    `json:"title"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Period      string    `json:"period,omitempty"`
	LectureCode string    `json:"lecture_code,omitempty"`
	Year        int       `json:"year,omitempty"`
	Instructors string    `json:"instructors,omitempty"`
	Room        string    `json:"room,omitempty"`
	Theme       string    `json:"theme,omitempty"`
	Note        string    `json:"note,omitempty"`
	Status      string    `json:"status,omitempty"`
	Cancelled   bool      `json:"cancelled"`
}

type ScheduleWeek struct {
	StartDate string          `json:"start_date"`
	EndDate   string          `json:"end_date"` // inclusive
	Events    []ScheduleEvent `json:"events"`
	Source    string          `json:"source"`
	FetchedAt time.Time       `json:"fetched_at"`
}

// ScheduleReader は同じ応答の ViewState を次の日付ジャンプに再利用する。
// 同一セッション内では直列に使い、教務への書き込みは行わない。
type ScheduleReader struct {
	c *Client
	p *page
}

func (c *Client) ScheduleReader() *ScheduleReader {
	r := &ScheduleReader{c: c}
	if c.loginSchedule != nil && time.Since(c.loginScheduleAt) < 30*time.Second {
		r.p = c.loginSchedule
	}
	c.loginSchedule = nil
	return r
}

// LoginSchedule はログインのリダイレクトで取得済みの週を追加通信なしで返す。
func (c *Client) LoginSchedule() (*ScheduleWeek, error) {
	if c.loginSchedule == nil {
		return nil, fmt.Errorf("ログイン応答に週間時間割がありません")
	}
	w, err := parseScheduleWeek(c.loginSchedule, time.Now().In(japan))
	if err != nil {
		return nil, err
	}
	w.Source = c.abs(schedulePath)
	return w, nil
}

func (r *ScheduleReader) request(ctx context.Context, vals url.Values) (*page, error) {
	target, method := r.c.abs(schedulePath), http.MethodGet
	if vals != nil {
		target, method = r.p.action(), http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	if vals != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", target)
	}
	resp, err := r.c.wc.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	p, err := readPage(resp)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(p.url), "login.aspx") {
		return nil, ErrSessionExpired
	}
	return p, nil
}

func (r *ScheduleReader) Week(ctx context.Context, date time.Time) (*ScheduleWeek, error) {
	var err error
	if r.p == nil {
		r.p, err = r.request(ctx, nil)
		if err != nil {
			return nil, err
		}
	}
	if !r.p.has(weekGrid) {
		if !r.p.has("ctl00_phContents_rbtnWeek") {
			return nil, fmt.Errorf("ポータルの週間時間割が見つかりません")
		}
		v := r.p.formValues()
		v.Set("__EVENTTARGET", "ctl00$phContents$rbtnWeek")
		v.Set("__EVENTARGUMENT", "")
		v.Set("ctl00$phContents$sch", "rbtnWeek")
		r.p, err = r.request(ctx, v)
		if err != nil {
			return nil, err
		}
	}
	// GET の週はサーバーの現在日付。要求日と月日が合う場合は追加通信不要。
	w, parseErr := parseScheduleWeek(r.p, date)
	if parseErr == nil && date.Format("2006-01-02") >= w.StartDate && date.Format("2006-01-02") <= w.EndDate && date.Year() == time.Now().In(japan).Year() {
		w.Source = r.c.abs(schedulePath)
		return w, nil
	}
	v := r.p.formValues()
	v.Set("__EVENTTARGET", "")
	v.Set("__EVENTARGUMENT", "")
	v.Set("ctl00$phContents$ucScheduleWeek$dsJump$txtDate", date.Format("2006/01/02"))
	v.Set("ctl00$phContents$ucScheduleWeek$ctl00$btnJump", "指定した日付へジャンプ")
	r.p, err = r.request(ctx, v)
	if err != nil {
		return nil, err
	}
	w, err = parseScheduleWeek(r.p, date)
	if err != nil {
		return nil, err
	}
	key := date.Format("2006-01-02")
	if key < w.StartDate || key > w.EndDate {
		return nil, fmt.Errorf("ポータルが指定日 %s の時間割を返しませんでした", key)
	}
	w.Source = r.c.abs(schedulePath)
	return w, nil
}

// tableRows は入れ子の予定テーブルの行を除外する。
func tableRows(table *html.Node) [][]*html.Node {
	var out [][]*html.Node
	walk(table, func(n *html.Node) bool {
		if n != table && n.Type == html.ElementNode && n.Data == "table" {
			return false
		}
		if n.Type == html.ElementNode && n.Data == "tr" {
			var cells []*html.Node
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
					cells = append(cells, c)
				}
			}
			out = append(out, cells)
			return false
		}
		return true
	})
	return out
}

func parseScheduleWeek(p *page, reference time.Time) (*ScheduleWeek, error) {
	table := p.ids[weekGrid]
	if table == nil {
		return nil, fmt.Errorf("週間時間割の表が見つかりません")
	}
	rows := tableRows(table)
	if len(rows) < 2 || len(rows[0]) != 8 {
		return nil, fmt.Errorf("週間時間割の見出しが変更されています")
	}
	var dates []time.Time
	for i, cell := range rows[0][1:] {
		m := scheduleDate.FindStringSubmatch(squash(nodeText(cell)))
		if m == nil {
			return nil, fmt.Errorf("時間割の日付が読めません")
		}
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		var best time.Time
		for y := reference.Year() - 1; y <= reference.Year()+1; y++ {
			d := time.Date(y, time.Month(month), day, 0, 0, 0, 0, japan)
			if d.Month() != time.Month(month) || d.Day() != day {
				continue
			}
			if best.IsZero() || absDuration(d.Sub(reference)) < absDuration(best.Sub(reference)) {
				best = d
			}
		}
		if best.IsZero() || (i > 0 && !best.Equal(dates[0].AddDate(0, 0, i))) {
			return nil, fmt.Errorf("時間割の日付が連続していません")
		}
		if m[3] != []string{"日", "月", "火", "水", "木", "金", "土"}[best.Weekday()] {
			return nil, fmt.Errorf("時間割の曜日と指定年度が一致しません")
		}
		dates = append(dates, best)
	}
	type placed struct {
		row, col, span int
		cell           *html.Node
	}
	var cells []placed
	var occupied [8]int
	baseMinute, step, firstLabelRow, firstLabelMinute := -1, 0, -1, 0
	for row, cs := range rows[1:] {
		col := 0
		for _, cell := range cs {
			for col < 8 && occupied[col] > row {
				col++
			}
			if col >= 8 {
				return nil, fmt.Errorf("時間割の列構造が変更されています")
			}
			span, _ := strconv.Atoi(attr(cell, "rowspan"))
			if span < 1 {
				span = 1
			}
			occupied[col] = row + span
			if col == 0 {
				txt := squash(nodeText(cell))
				if clockText.MatchString(txt) {
					tm, _ := time.Parse("15:04", txt)
					minute := tm.Hour()*60 + tm.Minute()
					if firstLabelRow < 0 {
						firstLabelRow, firstLabelMinute = row, minute
					} else if step == 0 && row > firstLabelRow {
						step = (minute - firstLabelMinute) / (row - firstLabelRow)
						baseMinute = firstLabelMinute - firstLabelRow*step
					}
				}
			} else {
				cells = append(cells, placed{row, col, span, cell})
			}
			col++
		}
	}
	if step <= 0 || baseMinute < 0 {
		return nil, fmt.Errorf("時間割の時刻目盛が読めません")
	}
	w := &ScheduleWeek{StartDate: dates[0].Format("2006-01-02"), EndDate: dates[6].Format("2006-01-02"), Events: []ScheduleEvent{}, FetchedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for _, cell := range cells {
		before := len(w.Events)
		walk(cell.cell, func(n *html.Node) bool {
			if n.Type != html.ElementNode || n.Data != "table" || !strings.HasSuffix(attr(n, "id"), "tblKoma") {
				return true
			}
			fields := map[string]string{}
			var link url.Values
			walk(n, func(x *html.Node) bool {
				id := attr(x, "id")
				if idx := strings.LastIndex(id, "_"); idx >= 0 {
					id = id[idx+1:]
				}
				if id != "" {
					fields[id] = squash(nodeText(x))
				}
				if x.Data == "a" && id == "hlSbjNm" {
					if u, err := url.Parse(attr(x, "href")); err == nil {
						link = u.Query()
					}
				}
				return true
			})
			title := fields["hlSbjNm"]
			if title == "" {
				title = fields["lblEventNm"]
			}
			if title == "" {
				return false
			}
			date := dates[cell.col-1]
			start := date.Add(time.Duration(baseMinute+cell.row*step) * time.Minute)
			end := start.Add(time.Duration(cell.span*step) * time.Minute)
			e := ScheduleEvent{Date: date.Format("2006-01-02"), Title: title, Start: start, End: end, Period: fields["lblPeriod"], LectureCode: link.Get("lct_cd"), Instructors: fields["lblStaffNm"], Room: fields["lblRoomNm"], Theme: fields["lblDayTheme"], Note: fields["lblNote"], Status: fields["lblOpneNm"]}
			e.Year, _ = strconv.Atoi(link.Get("lct_year"))
			for _, s := range []string{"休講", "中止", "取消"} {
				if strings.Contains(e.Status, s) {
					e.Cancelled = true
				}
			}
			e.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(e.Date+"|"+e.Start.Format("15:04")+"|"+e.LectureCode+"|"+e.Title)))[:32]
			if !seen[e.ID] {
				w.Events = append(w.Events, e)
				seen[e.ID] = true
			}
			return false
		})
		if len(w.Events) == before && squash(nodeText(cell.cell)) != "" {
			return nil, fmt.Errorf("時間割に読み取れない予定があります（ポータルの形式変更）")
		}
	}
	sort.SliceStable(w.Events, func(i, j int) bool { return w.Events[i].Start.Before(w.Events[j].Start) })
	return w, nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
