package academic

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

var ErrScheduleQuery = errors.New("date は YYYY-MM-DD、days は 1〜31 で指定してください")
var scheduleZone = time.FixedZone("Asia/Tokyo", 9*60*60)

type ScheduleItem struct {
	kyomu.ScheduleEvent `tstype:",extends"`
	GoogleCalendarURL   string    `json:"google_calendar_url,omitempty"`
	Location            *Location `json:"location,omitempty"` // 教室の建物と地図。特定できなければ無し
}

type ScheduleDay struct {
	Date      string         `json:"date"`
	Events    []ScheduleItem `json:"events"`
	FetchedAt time.Time      `json:"fetched_at"`
}

type Schedule struct {
	StartDate string        `json:"start_date"`
	EndDate   string        `json:"end_date"`
	Timezone  string        `json:"timezone"`
	Days      []ScheduleDay `json:"days"`
	Source    string        `json:"source"`
	FetchedAt time.Time     `json:"fetched_at"`
	Cached    bool          `json:"cached"`
}

func ScheduleRange(date string, days int) (time.Time, int, error) {
	if date == "" {
		date = time.Now().In(scheduleZone).Format("2006-01-02")
	}
	if days == 0 {
		days = 1
	}
	d, err := time.ParseInLocation("2006-01-02", date, scheduleZone)
	if err != nil || date != d.Format("2006-01-02") || days < 1 || days > 31 || d.Year() < 2000 || d.Year() > 2100 {
		return time.Time{}, 0, ErrScheduleQuery
	}
	return d, days, nil
}

func scheduleMonday(d time.Time) time.Time { return d.AddDate(0, 0, -(int(d.Weekday())+6)%7) }

func (s *Service) cacheLoginSchedule(user string, c *kyomu.Client) {
	if w, err := c.LoginSchedule(); err == nil {
		s.scheduleCache.set(user+"\x00"+w.StartDate, w)
	}
}

// MySchedule は実日付の予定だけを返す。空の正常応答と取得失敗を区別し、曜日から補完しない。
func (s *Service) MySchedule(ctx context.Context, user, date string, days int, refresh bool) (*Schedule, error) {
	start, days, err := ScheduleRange(date, days)
	if err != nil {
		return nil, err
	}
	if user == "" {
		return nil, ErrNoCredentials
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	us := s.userSession(user)
	us.scheduleMu.Lock()
	defer us.scheduleMu.Unlock()
	end := start.AddDate(0, 0, days-1)
	weeks := map[string]*kyomu.ScheduleWeek{}
	var missing []time.Time
	for d := scheduleMonday(start); !d.After(end); d = d.AddDate(0, 0, 7) {
		key := d.Format("2006-01-02")
		if w, ok := s.scheduleCache.get(user + "\x00" + key); ok && !refresh {
			weeks[key] = w
		} else {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		err = s.withUser(user, func(c *kyomu.Client) error {
			reader := c.ScheduleReader()
			for _, d := range missing {
				if err := ctx.Err(); err != nil {
					return err
				}
				key := d.Format("2006-01-02")
				// withUser の再ログインで今週が取得済みなら、月表示でも再取得しない。
				if w, ok := s.scheduleCache.get(user + "\x00" + key); ok && !refresh {
					weeks[key] = w
					continue
				}
				w, err := reader.Week(ctx, d)
				if err != nil {
					return err
				}
				// ポータルの週の境界が変わった場合に、未確認日を空きとしない。
				if w.StartDate != key || w.EndDate != d.AddDate(0, 0, 6).Format("2006-01-02") {
					return fmt.Errorf("ポータルの週の日付が要求と一致しません")
				}
				weeks[key] = w
				s.scheduleCache.set(user+"\x00"+key, w)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	res := &Schedule{StartDate: start.Format("2006-01-02"), EndDate: end.Format("2006-01-02"), Timezone: "Asia/Tokyo", Days: []ScheduleDay{}, Cached: len(missing) == 0}
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		w := weeks[scheduleMonday(d).Format("2006-01-02")]
		day := ScheduleDay{Date: d.Format("2006-01-02"), Events: []ScheduleItem{}, FetchedAt: w.FetchedAt}
		for _, e := range w.Events {
			if e.Date == day.Date {
				e.Room = NormalizeRoom(e.Room) // 共創技術ﾗﾎﾞ → 共創技術ラボ、工1－322 → 工1-322
				item := ScheduleItem{ScheduleEvent: e, Location: LocateRoom(e.Room)}
				if !e.Cancelled {
					item.GoogleCalendarURL = googleCalendarURL(e, w.Source)
				}
				day.Events = append(day.Events, item)
			}
		}
		res.Days = append(res.Days, day)
		res.Source = w.Source
		if res.FetchedAt.IsZero() || w.FetchedAt.Before(res.FetchedAt) {
			res.FetchedAt = w.FetchedAt
		}
	}
	return res, nil
}

// eventPlace はカレンダーの場所欄。建物が分かれば「工4-211（工学部4号館）」にする。
func eventPlace(e kyomu.ScheduleEvent) string {
	if l := LocateRoom(e.Room); l != nil && !l.Online {
		return e.Room + "（" + l.Building + "）"
	}
	return e.Room
}

func scheduleDescription(e kyomu.ScheduleEvent, source string) string {
	var parts []string
	for _, v := range []string{e.Period, e.Instructors, e.Theme, e.Note, e.Status, source} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "\n")
}

func googleCalendarURL(e kyomu.ScheduleEvent, source string) string {
	q := url.Values{"action": {"TEMPLATE"}, "text": {e.Title}, "dates": {e.Start.UTC().Format("20060102T150405Z") + "/" + e.End.UTC().Format("20060102T150405Z")}, "ctz": {"Asia/Tokyo"}, "location": {eventPlace(e)}, "details": {scheduleDescription(e, source)}}
	return "https://calendar.google.com/calendar/render?" + q.Encode()
}

// ScheduleICS は繰り返しを使わず、確認できた日付の予定だけを書き出す。
func ScheduleICS(user string, s *Schedule) string {
	var b strings.Builder
	line := func(v string) {
		width := 0
		for _, r := range v {
			n := len(string(r))
			if width+n > 75 {
				b.WriteString("\r\n ")
				width = 1
			}
			b.WriteRune(r)
			width += n
		}
		b.WriteString("\r\n")
	}
	escape := func(v string) string {
		v = strings.ReplaceAll(v, "\r\n", "\n")
		v = strings.ReplaceAll(v, "\r", "\n")
		return strings.NewReplacer("\\", "\\\\", "\n", "\\n", ";", "\\;", ",", "\\,").Replace(v)
	}
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Ryudai Tools//University Schedule//JA")
	line("CALSCALE:GREGORIAN")
	line("X-WR-CALNAME:大学の予定")
	line("X-WR-TIMEZONE:Asia/Tokyo")
	for _, d := range s.Days {
		for _, item := range d.Events {
			e := item.ScheduleEvent
			line("BEGIN:VEVENT")
			line(fmt.Sprintf("UID:%x@ryudai-tools", sha256.Sum256([]byte(user+"\x00"+e.ID))))
			line("DTSTAMP:" + d.FetchedAt.UTC().Format("20060102T150405Z"))
			line("DTSTART:" + e.Start.UTC().Format("20060102T150405Z"))
			line("DTEND:" + e.End.UTC().Format("20060102T150405Z"))
			line("SUMMARY:" + escape(e.Title))
			line("LOCATION:" + escape(eventPlace(e)))
			line("DESCRIPTION:" + escape(scheduleDescription(e, s.Source)))
			if e.Cancelled {
				line("STATUS:CANCELLED")
			} else {
				line("STATUS:CONFIRMED")
			}
			line("END:VEVENT")
		}
	}
	line("END:VCALENDAR")
	return b.String()
}
