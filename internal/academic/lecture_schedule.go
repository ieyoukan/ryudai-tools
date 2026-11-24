package academic

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
)

var ErrLectureQuery = errors.New("年度と講義コードを正しく指定してください")
var lectureCodePattern = regexp.MustCompile(`^[0-9A-Za-z]{1,20}$`)

// LectureSchedule は教務の授業明細スケジュールに、対応する WebClass コースの ID を添えたもの。
type LectureSchedule struct {
	kyomu.LectureSchedule `tstype:",extends"`
	// WebClassCourseID は WebClass の慣例（年度下2桁 + "S" + 講義コード）から作った ID。
	// WebClass に実在するかは時間割の一覧で確認する。
	WebClassCourseID string `json:"webclass_course_id"`
	// Locations は授業回に出てくる教室表記 → 建物と地図（特定できた教室のみ）。
	Locations map[string]*Location `json:"locations"`
	Cached    bool                 `json:"cached"`
}

func withLocations(ls *kyomu.LectureSchedule, cached bool) *LectureSchedule {
	res := &LectureSchedule{LectureSchedule: *ls, WebClassCourseID: WebClassCourseID(ls.Year, ls.LectureCode), Locations: map[string]*Location{}, Cached: cached}
	// キャッシュの中身を書き換えないよう、授業回はコピーしてから教室表記をそろえる。
	res.Sessions = append([]kyomu.LectureSession(nil), ls.Sessions...)
	for i := range res.Sessions {
		res.Sessions[i].Room = NormalizeRoom(res.Sessions[i].Room)
	}
	for _, s := range res.Sessions {
		if _, done := res.Locations[s.Room]; !done && s.Room != "" {
			if l := LocateRoom(s.Room); l != nil {
				res.Locations[s.Room] = l
			}
		}
	}
	return res
}

// WebClassCourseID は 2026 年度の講義 123456789 を 26S123456789 にする。
func WebClassCourseID(year int, code string) string {
	return fmt.Sprintf("%02dS%s", year%100, code)
}

// MyLectureSchedule は本人のセッションで講義の全授業回（日付・時刻・教室・担当・備考）を返す。
func (s *Service) MyLectureSchedule(user string, year int, code string, refresh bool) (*LectureSchedule, error) {
	if year < 2000 || year > 2100 || !lectureCodePattern.MatchString(code) {
		return nil, ErrLectureQuery
	}
	if user == "" {
		return nil, ErrNoCredentials
	}
	key := user + "\x00" + strconv.Itoa(year) + "\x00" + code
	if ls, ok := s.lectureCache.get(key); ok && !refresh {
		return withLocations(ls, true), nil
	}
	var ls *kyomu.LectureSchedule
	err := s.withUser(user, func(c *kyomu.Client) error {
		var err error
		ls, err = c.LectureSchedule(year, code)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.lectureCache.set(key, ls)
	return withLocations(ls, false), nil
}
