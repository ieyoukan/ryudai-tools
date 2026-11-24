package kyomu

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const lectureListPath = "StudentApp/Lct/LectureList.aspx"
const lectureGrid = "ctl00_phContents_ucLctList_gv"
const lectureHeader = "ctl00_phContents_ucLctList_ucLctHeader_"

var sessionDate = regexp.MustCompile(`^(\d{4})/(\d{1,2})/(\d{1,2})`)
var sessionTime = regexp.MustCompile(`^(\d{1,2}):(\d{2})\s*[～~〜\-－]\s*(\d{1,2}):(\d{2})$`)

// LectureSession は教務の「授業明細スケジュール」の1回分。
type LectureSession struct {
	No          int    `json:"no"`
	Date        string `json:"date"`                 // YYYY-MM-DD
	Period      string `json:"period,omitempty"`     // 例: 4限
	StartTime   string `json:"start_time,omitempty"` // HH:MM（日本時間）
	EndTime     string `json:"end_time,omitempty"`
	Room        string `json:"room,omitempty"`
	Instructors string `json:"instructors,omitempty"`
	Note        string `json:"note,omitempty"`
	Cancelled   bool   `json:"cancelled"`
}

// LectureSchedule は講義1件の教務ページ（授業明細スケジュール）。
type LectureSchedule struct {
	Year        int              `json:"year"`
	LectureCode string           `json:"lecture_code"`
	NumberCode  string           `json:"number_code,omitempty"` // 科目番号（例: 知能326）
	Title       string           `json:"title"`
	Instructors string           `json:"instructors,omitempty"` // 責任教員
	DayPeriod   string           `json:"day_period,omitempty"`  // 例: 木4
	Term        string           `json:"term,omitempty"`
	Credits     string           `json:"credits,omitempty"`
	Sessions    []LectureSession `json:"sessions"`
	Source      string           `json:"source"`
	FetchedAt   time.Time        `json:"fetched_at"`
}

// LectureSchedule は講義の全授業回（日付・時刻・教室・担当・備考）を教務から読む。読み取りのみ。
func (c *Client) LectureSchedule(year int, code string) (*LectureSchedule, error) {
	path := lectureListPath + "?" + url.Values{"lct_year": {strconv.Itoa(year)}, "lct_cd": {code}}.Encode()
	p, err := c.get(path)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(p.url), "login.aspx") {
		return nil, ErrSessionExpired
	}
	ls, err := parseLectureSchedule(p)
	if err != nil {
		return nil, err
	}
	if ls.LectureCode != code {
		return nil, fmt.Errorf("教務システムが講義 %s の授業明細を返しませんでした", code)
	}
	ls.Source = c.abs(path)
	return ls, nil
}

func parseLectureSchedule(p *page) (*LectureSchedule, error) {
	field := func(name string) string { return squash(p.text(lectureHeader + name)) }
	ls := &LectureSchedule{
		LectureCode: field("lblLctCd"),
		NumberCode:  field("lblNumCd"),
		Title:       field("linkSbjNm"),
		Instructors: field("lblStaffNm"),
		DayPeriod:   field("lblDayPeriodShort"),
		Term:        field("lblTermNm"),
		Credits:     field("lblCredits"),
		Sessions:    []LectureSession{},
		FetchedAt:   time.Now().UTC(),
	}
	ls.Year, _ = strconv.Atoi(field("lblLctYear"))
	if ls.LectureCode == "" || ls.Year == 0 {
		return nil, fmt.Errorf("教務システムの授業明細が見つかりません（講義コードが違うか、ページの形式が変わりました）")
	}
	if ls.Title == "" {
		ls.Title = field("lblSbjNm")
	}
	table := p.ids[lectureGrid]
	if table == nil {
		// 授業日程が未登録の講義は表そのものが出ない。
		return ls, nil
	}
	rows := tableRows(table)
	if len(rows) == 0 || len(rows[0]) < 7 || squash(nodeText(rows[0][1])) != "日付" || squash(nodeText(rows[0][3])) != "時刻" || squash(nodeText(rows[0][4])) != "教室名" {
		return nil, fmt.Errorf("授業明細スケジュールの見出しが変更されています")
	}
	for _, cells := range rows[1:] {
		if len(cells) < 7 {
			continue
		}
		cell := func(i int) string { return squash(nodeText(cells[i])) }
		m := sessionDate.FindStringSubmatch(cell(1))
		if m == nil {
			return nil, fmt.Errorf("授業明細の日付が読めません: %q", cell(1))
		}
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		s := LectureSession{
			Date:        time.Date(y, time.Month(mo), d, 0, 0, 0, 0, japan).Format("2006-01-02"),
			Period:      cell(2),
			Room:        normalizeRoom(cell(4)),
			Instructors: joinLines(nodeText(cells[5]), "、"),
			Note:        joinLines(nodeText(cells[6]), "\n"),
		}
		s.No, _ = strconv.Atoi(cell(0))
		if t := sessionTime.FindStringSubmatch(cell(3)); t != nil {
			n := func(i int) int { v, _ := strconv.Atoi(t[i]); return v }
			s.StartTime = fmt.Sprintf("%02d:%02d", n(1), n(2))
			s.EndTime = fmt.Sprintf("%02d:%02d", n(3), n(4))
		}
		for _, w := range []string{"休講", "中止", "取消"} {
			if strings.Contains(s.Note, w) {
				s.Cancelled = true
			}
		}
		ls.Sessions = append(ls.Sessions, s)
	}
	return ls, nil
}

func joinLines(s, sep string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = squash(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, sep)
}

// normalizeRoom は全角ハイフンを週間時間割と同じ半角にそろえる（工1－322 → 工1-322）。
func normalizeRoom(s string) string {
	return strings.NewReplacer("－", "-", "−", "-").Replace(s)
}
