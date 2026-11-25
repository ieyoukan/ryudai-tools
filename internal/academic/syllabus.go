package academic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

// ErrEmptyQuery は検索条件が空であることを示す（全件取得は重いので禁止）。
var ErrEmptyQuery = errors.New("検索条件を 1 つ以上指定してください（科目名・学部・曜日など）")

// SyllabusDigest は検索結果 1 件に、詳細ページから単位数などを補ったもの。
// AI や一覧画面が「この講義は何単位でどう評価されるか」を 1 回で把握できる粒度にする。
type SyllabusDigest struct {
	Quota                 string               `json:"quota,omitempty"`
	Availability          *LectureAvailability `json:"availability,omitempty"`
	kyomu.SyllabusSummary `tstype:",extends"`
	Credits               float64  `json:"credits"`
	Faculty               string   `json:"faculty,omitempty"`
	Room                  string   `json:"room,omitempty"`
	RegistrationMethod    string   `json:"registration_method,omitempty"`
	TeachingFormat        string   `json:"teaching_format,omitempty"`
	LectureTypes          []string `json:"lecture_types,omitempty"`
	OutlineExcerpt        string   `json:"outline_excerpt,omitempty"`
	GradingExcerpt        string   `json:"grading_excerpt,omitempty"`
	DetailError           string   `json:"detail_error,omitempty"`
}

// SearchResult は検索結果全体。
type SearchResult struct {
	Total     int              `json:"total"`     // 条件に合う全件数
	Returned  int              `json:"returned"`  // results の件数
	Truncated bool             `json:"truncated"` // limit で切り詰めたか
	Results   []SyllabusDigest `json:"results"`
}

// SearchSyllabus はシラバスを検索する。withDetails なら先頭 limit 件に単位数などを付与する。
func (s *Service) SearchSyllabus(p kyomu.SearchParams, limit int, withDetails bool) (*SearchResult, error) {
	if p.IsEmpty() {
		return nil, ErrEmptyQuery
	}
	if p.Year == 0 {
		p.Year = kyomu.AcademicYear(time.Now())
	}
	all, err := s.searchCached(p)
	if err != nil {
		return nil, err
	}
	res := &SearchResult{Total: len(all)}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
		res.Truncated = true
	}
	res.Results = make([]SyllabusDigest, len(all))
	for i, sm := range all {
		res.Results[i] = SyllabusDigest{SyllabusSummary: sm}
	}
	if withDetails {
		s.fillDetails(res.Results)
	}
	res.Returned = len(res.Results)
	return res, nil
}

func (s *Service) searchCached(p kyomu.SearchParams) ([]kyomu.SyllabusSummary, error) {
	keyBytes, _ := json.Marshal(p)
	key := string(keyBytes)
	if v, ok := s.searchCache.get(key); ok {
		return v, nil
	}
	release := s.acquire()
	defer release()
	c, err := kyomu.New(s.baseURL)
	if err != nil {
		return nil, err
	}
	v, err := c.SearchSyllabus(p)
	if err != nil {
		return nil, err
	}
	s.searchCache.set(key, v)
	return v, nil
}

// Syllabus はシラバス詳細を返す（キャッシュつき）。year が 0 なら今年度。
func (s *Service) Syllabus(year int, code string) (*kyomu.Syllabus, error) {
	if year == 0 {
		year = kyomu.AcademicYear(time.Now())
	}
	code = strings.TrimSpace(code)
	key := fmt.Sprintf("%d/%s", year, code)
	if v, ok := s.syllabusCache.get(key); ok {
		return v, nil
	}
	var stored kyomu.Syllabus
	if s.loadPublic("syllabus/"+key, &stored) {
		s.syllabusCache.set(key, &stored)
		return &stored, nil
	}
	release := s.acquire()
	defer release()
	// 詳細ページは初回アクセスでセッションを張るので、並列取得のため毎回新しいクライアントを使う。
	c, err := kyomu.New(s.baseURL)
	if err != nil {
		return nil, err
	}
	v, err := c.FetchSyllabus(year, code)
	if err != nil {
		return nil, err
	}
	s.syllabusCache.set(key, v)
	s.savePublic("syllabus/"+key, v)
	return v, nil
}

// SyllabusOptions は検索フォームの選択肢を返す（キャッシュつき）。
func (s *Service) SyllabusOptions(faculty string) (*kyomu.SearchOptions, error) {
	if v, ok := s.optionsCache.get(faculty); ok {
		return v, nil
	}
	var stored kyomu.SearchOptions
	if s.loadPublic("syllabus-options/"+faculty, &stored) {
		s.optionsCache.set(faculty, &stored)
		return &stored, nil
	}
	release := s.acquire()
	defer release()
	c, err := kyomu.New(s.baseURL)
	if err != nil {
		return nil, err
	}
	v, err := c.SyllabusOptions(faculty)
	if err != nil {
		return nil, err
	}
	s.optionsCache.set(faculty, v)
	s.savePublic("syllabus-options/"+faculty, v)
	return v, nil
}

// fillDetails は各結果にシラバス詳細の要点を並列で埋める（同時数は upstream セマフォで制限）。
func (s *Service) fillDetails(ds []SyllabusDigest) {
	var wg sync.WaitGroup
	for i := range ds {
		wg.Add(1)
		go func(d *SyllabusDigest) {
			defer wg.Done()
			syl, err := s.Syllabus(d.Year, d.LectureCode)
			if err != nil {
				d.DetailError = err.Error()
				return
			}
			d.Quota = syl.Quota
			d.Credits = syl.Credits
			d.Faculty = syl.Faculty
			d.Room = syl.Room
			d.RegistrationMethod = syl.RegistrationMethod
			d.TeachingFormat = syl.TeachingFormat
			d.LectureTypes = syl.LectureTypes
			d.OutlineExcerpt = excerpt(syl.Outline, 160)
			d.GradingExcerpt = excerpt(syl.Grading, 120)
		}(&ds[i])
	}
	wg.Wait()
}

// excerpt は複数行を 1 行にして先頭 n 文字（ルーン）に切り詰める。
// 日本語の途中で折り返された行は空白を挟まずにつなぐ。
func excerpt(s string, n int) string {
	var sb strings.Builder
	for _, line := range strings.Split(s, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if sb.Len() > 0 {
			prev, _ := utf8.DecodeLastRuneInString(sb.String())
			next, _ := utf8.DecodeRuneInString(line)
			if prev < utf8.RuneSelf && next < utf8.RuneSelf {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(line)
	}
	s = sb.String()
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
