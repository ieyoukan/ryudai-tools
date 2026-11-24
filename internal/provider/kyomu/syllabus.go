package kyomu

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	syllabusSearchPath = "Public/Syllabus/SearchMain.aspx"
	syllabusDetailPath = "Public/Syllabus/DetailMain.aspx"
	fieldPrefix        = "ctl00$phContents$"
	gridID             = "ctl00_phContents_ucGrid_grv"
)

// ErrSyllabusNotFound は講義コードに対応するシラバスが無いことを示す。
var ErrSyllabusNotFound = errors.New("シラバスが見つかりません")

// SearchParams はシラバス検索条件。選択式の項目はコード値でも表示名でも指定できる
// （例: Faculty に "25" でも "工学部" でも可、Term に "2" でも "後期" でも可）。
type SearchParams struct {
	Year          int    `json:"year,omitempty"`           // 開講年度（0 なら今年度）
	Term          string `json:"term,omitempty"`           // 前学期/後学期/通年
	Faculty       string `json:"faculty,omitempty"`        // 開講学部等
	Department    string `json:"department,omitempty"`     // 開講学科
	Division      string `json:"division,omitempty"`       // 開講コース
	Day           string `json:"day,omitempty"`            // 曜日（月〜土, 集中, 未定, その他）
	Period        string `json:"period,omitempty"`         // 時限（1〜8）
	NumberingCode string `json:"numbering_code,omitempty"` // 科目番号
	LectureCode   string `json:"lecture_code,omitempty"`   // 講義コード
	Subject       string `json:"subject,omitempty"`        // 科目名（部分一致）
	Instructor    string `json:"instructor,omitempty"`     // 教員名
	LectureType   string `json:"lecture_type,omitempty"`   // 講義/演習又は実験/実習/TA有り/実務経験講師
	Keyword       string `json:"keyword,omitempty"`        // 全文検索
}

// IsEmpty は絞り込み条件（年度以外）が一つも無いかを返す。
func (p SearchParams) IsEmpty() bool {
	return p.Term == "" && p.Faculty == "" && p.Department == "" && p.Division == "" &&
		p.Day == "" && p.Period == "" && p.NumberingCode == "" && p.LectureCode == "" &&
		p.Subject == "" && p.Instructor == "" && p.LectureType == "" && p.Keyword == ""
}

// SyllabusSummary は検索結果 1 行分。
type SyllabusSummary struct {
	Year          int    `json:"year"`
	LectureCode   string `json:"lecture_code"`
	NumberingCode string `json:"numbering_code,omitempty"`
	Language      string `json:"language,omitempty"` // 日 / 英
	Title         string `json:"title"`
	Instructors   string `json:"instructors"`
	Term          string `json:"term"`
	DayPeriod     string `json:"day_period"`
	Slots         []Slot `json:"slots,omitempty"`
	URL           string `json:"url"`
}

// Slot は曜日・時限の 1 コマ。
type Slot struct {
	Day    string `json:"day"`            // 月〜土
	Period int    `json:"period"`         // 1〜8
	Half   string `json:"half,omitempty"` // "前半" / "後半"（1コマを分割した科目）
}

// Book は教科書・参考書 1 冊。
type Book struct {
	Title     string `json:"title"`
	Author    string `json:"author,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Year      string `json:"year,omitempty"`
	ISBN      string `json:"isbn,omitempty"`
	Note      string `json:"note,omitempty"`
}

// SyllabusPlanSession は授業計画に明記された回番号と内容。日付は推測しない。
type SyllabusPlanSession struct {
	No      int    `json:"no"`
	Content string `json:"content"`
}

// Syllabus はシラバス詳細。
type Syllabus struct {
	Year               int      `json:"year"`
	LectureCode        string   `json:"lecture_code"`
	NumberingCode      string   `json:"numbering_code,omitempty"`
	Title              string   `json:"title"`
	Instructors        string   `json:"instructors"`
	Credits            float64  `json:"credits"`
	Term               string   `json:"term"`
	DayPeriod          string   `json:"day_period"`
	Slots              []Slot   `json:"slots,omitempty"`
	Faculty            string   `json:"faculty"`
	Room               string   `json:"room,omitempty"`
	Quota              string   `json:"quota,omitempty"`               // 登録人数（定員）
	RegistrationMethod string   `json:"registration_method,omitempty"` // 抽選対象 など
	TeachingFormat     string   `json:"teaching_format,omitempty"`     // 対面授業 / 遠隔 など
	MainCourse         bool     `json:"main_course"`                   // 主要授業科目
	LectureTypes       []string `json:"lecture_types,omitempty"`
	ActiveLearning     string   `json:"active_learning,omitempty"`
	Outline            string   `json:"outline,omitempty"`      // 授業内容と方法
	URGCC              string   `json:"urgcc,omitempty"`        // URGCC 学習教育目標
	Goals              string   `json:"goals,omitempty"`        // 達成目標
	Grading            string   `json:"grading,omitempty"`      // 評価基準と評価方法
	Requirements       string   `json:"requirements,omitempty"` // 履修条件
	Plan               string   `json:"plan,omitempty"`         // 授業計画
	Preparation        string   `json:"preparation,omitempty"`  // 事前学習
	Review             string   `json:"review,omitempty"`       // 事後学習
	Textbooks          []Book   `json:"textbooks,omitempty"`
	TextbookNote       string   `json:"textbook_note,omitempty"`
	References         []Book   `json:"references,omitempty"`
	ReferenceNote      string   `json:"reference_note,omitempty"`
	Language           string   `json:"language,omitempty"`
	Message            string   `json:"message,omitempty"`
	OfficeHours        string   `json:"office_hours,omitempty"`
	Website            string   `json:"website,omitempty"`
	URL                string   `json:"url"`

	PlanSessions []SyllabusPlanSession `json:"plan_sessions,omitempty"`
}

// SearchOptions は検索フォームの選択肢一覧。
type SearchOptions struct {
	Years        []Option `json:"years"`
	Terms        []Option `json:"terms"`
	Faculties    []Option `json:"faculties"`
	Departments  []Option `json:"departments,omitempty"` // Faculty 指定時のみ
	Days         []Option `json:"days"`
	Periods      []Option `json:"periods"`
	LectureTypes []Option `json:"lecture_types"`
}

// AcademicYear は日付から年度（4月始まり）を返す。
func AcademicYear(t time.Time) int {
	t = t.In(time.FixedZone("Asia/Tokyo", 9*60*60))
	if t.Month() < time.April {
		return t.Year() - 1
	}
	return t.Year()
}

// SyllabusURL はシラバス詳細ページの URL を返す。
func SyllabusURL(baseURL string, year int, lectureCode string) string {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return fmt.Sprintf("%s/%s?lct_year=%d&lct_cd=%s&je_cd=1",
		strings.TrimRight(baseURL, "/"), syllabusDetailPath, year, url.QueryEscape(lectureCode))
}

// SyllabusOptions は検索フォームの選択肢を返す。faculty を渡すと学科一覧も返す。
func (c *Client) SyllabusOptions(faculty string) (*SearchOptions, error) {
	p, err := c.get(syllabusSearchPath)
	if err != nil {
		return nil, err
	}
	opts := &SearchOptions{
		Years:        realOptions(p.selectOptions(fieldPrefix + "ddl_year")),
		Terms:        realOptions(p.selectOptions(fieldPrefix + "ddl_term")),
		Faculties:    realOptions(p.selectOptions(fieldPrefix + "ddl_fac")),
		Days:         realOptions(p.selectOptions(fieldPrefix + "ddl_day")),
		Periods:      realOptions(p.selectOptions(fieldPrefix + "ddl_time")),
		LectureTypes: realOptions(p.selectOptions(fieldPrefix + "ddl_syl_lct_type")),
	}
	if faculty != "" {
		fac, err := resolveOption(opts.Faculties, faculty, nil)
		if err != nil {
			return nil, fmt.Errorf("学部: %w", err)
		}
		p, err = c.postback(p, fieldPrefix+"ddl_fac", "", map[string]string{fieldPrefix + "ddl_fac": fac})
		if err != nil {
			return nil, err
		}
		opts.Departments = realOptions(p.selectOptions(fieldPrefix + "ddl_dpt"))
	}
	return opts, nil
}

// SearchSyllabus は公開シラバスを検索する（ログイン不要）。結果は全件を返す。
func (c *Client) SearchSyllabus(sp SearchParams) ([]SyllabusSummary, error) {
	p, err := c.get(syllabusSearchPath)
	if err != nil {
		return nil, err
	}
	if sp.Year == 0 {
		sp.Year = AcademicYear(time.Now())
	}
	set := map[string]string{fieldPrefix + "ddl_year": strconv.Itoa(sp.Year)}

	// 学部 → 学科 → コースは AutoPostBack で連動するので順に確定させる。
	cascade := []struct{ field, input, label string }{
		{"ddl_fac", sp.Faculty, "学部"},
		{"ddl_dpt", sp.Department, "学科"},
		{"ddl_div", sp.Division, "コース"},
	}
	for i, cf := range cascade {
		if cf.input == "" {
			break
		}
		v, err := resolveOption(p.selectOptions(fieldPrefix+cf.field), cf.input, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", cf.label, err)
		}
		set[fieldPrefix+cf.field] = v
		if i+1 < len(cascade) && cascade[i+1].input != "" {
			p, err = c.postback(p, fieldPrefix+cf.field, "", set)
			if err != nil {
				return nil, err
			}
		}
	}

	selects := []struct{ field, input, label string }{
		{"ddl_term", sp.Term, "開講学期"},
		{"ddl_day", sp.Day, "曜日"},
		{"ddl_time", sp.Period, "時限"},
		{"ddl_syl_lct_type", sp.LectureType, "授業の形態"},
	}
	for _, sf := range selects {
		if sf.input == "" {
			continue
		}
		v, err := resolveOption(p.selectOptions(fieldPrefix+sf.field), sf.input, optionAliases)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sf.label, err)
		}
		set[fieldPrefix+sf.field] = v
	}
	set[fieldPrefix+"txt_numCd_Search"] = sp.NumberingCode
	set[fieldPrefix+"txt_lctCd_Search"] = sp.LectureCode
	set[fieldPrefix+"txt_sbj_Search"] = sp.Subject
	set[fieldPrefix+"txt_staff_Search"] = sp.Instructor
	set[fieldPrefix+"txt_all_Search"] = sp.Keyword

	btnName, btnValue := p.submitButton("_btnSearch")
	if btnName == "" {
		return nil, errors.New("検索ボタンが見つかりません（画面構成が変わった可能性があります）")
	}
	p, err = c.submit(p, btnName, btnValue, set)
	if err != nil {
		return nil, err
	}
	if errMsg := p.searchError(); errMsg != "" {
		return nil, errors.New(errMsg)
	}
	// 複数ページに分かれていれば表示件数を「全件」にして取り直す。
	if p.hasGridPager() {
		p, err = c.postback(p, fieldPrefix+"ucGrid$ddlLines", "", map[string]string{fieldPrefix + "ucGrid$ddlLines": "0"})
		if err != nil {
			return nil, err
		}
	}
	return parseSearchResults(p, c.wc.BaseURL, sp.Year), nil
}

// FetchSyllabus は講義コードからシラバス詳細を取得する（ログイン不要）。
func (c *Client) FetchSyllabus(year int, lectureCode string) (*Syllabus, error) {
	lectureCode = strings.TrimSpace(lectureCode)
	if lectureCode == "" {
		return nil, errors.New("講義コードが空です")
	}
	if year == 0 {
		year = AcademicYear(time.Now())
	}
	src := SyllabusURL(c.wc.BaseURL, year, lectureCode)
	p, err := c.get(src)
	if err != nil {
		return nil, err
	}
	s, err := parseSyllabus(p)
	if err != nil {
		return nil, err
	}
	s.URL = src
	return s, nil
}

// submitButton は id が suffix で終わる submit ボタンの name と value を返す。
func (p *page) submitButton(suffix string) (string, string) {
	for id, n := range p.ids {
		if n.Data == "input" && strings.HasSuffix(id, suffix) {
			return attr(n, "name"), attr(n, "value")
		}
	}
	return "", ""
}

func (p *page) hasGridPager() bool {
	found := false
	walk(p.doc, func(n *html.Node) bool {
		if found {
			return false
		}
		if n.Type == html.ElementNode && n.Data == "a" && strings.Contains(attr(n, "href"), "ucGrid$grv','Page$") {
			found = true
		}
		return true
	})
	return found
}

// searchError は検索画面の入力エラーメッセージ（あれば）を返す。
func (p *page) searchError() string {
	for id, n := range p.ids {
		if strings.HasSuffix(id, "_aError") && attr(n, "title") != "" {
			return attr(n, "title")
		}
	}
	return ""
}

func parseSearchResults(p *page, baseURL string, year int) []SyllabusSummary {
	table, ok := p.ids[gridID]
	if !ok {
		return []SyllabusSummary{}
	}
	col, rows := gridRows(table)
	cell := func(r []*html.Node, name string) string { return gridCell(col, r, name) }
	out := []SyllabusSummary{}
	seen := map[string]bool{} // 全文検索では同じ講義が複数行に出ることがある
	for _, r := range rows {
		code := cell(r, "講義コード")
		if code == "" || len(r) < 5 {
			continue // ページャ行など
		}
		s := SyllabusSummary{
			LectureCode:   code,
			NumberingCode: cell(r, "科目番号"),
			Language:      cell(r, "日英"),
			Title:         cell(r, "科目名"),
			Instructors:   cell(r, "担当教員"),
			Term:          cell(r, "開講期"),
			DayPeriod:     cell(r, "曜日・時限"),
		}
		if i, ok := col["科目名"]; ok && i < len(r) {
			if q := linkQuery(r[i]); q != nil {
				s.Year, _ = strconv.Atoi(q.Get("lct_year"))
			}
		}
		if s.Year == 0 {
			s.Year = year
		}
		s.Slots = ParseSlots(s.DayPeriod)
		s.URL = SyllabusURL(baseURL, s.Year, s.LectureCode)
		if seen[s.LectureCode] {
			continue
		}
		seen[s.LectureCode] = true
		out = append(out, s)
	}
	return out
}

const detailPrefix = "ctl00_phContents_Detail_"

var bookFieldRe = regexp.MustCompile(`^` + detailPrefix + `ItemSyllabus(TextBook|ReferenceBook)_ItemBook\w*?_(\d+)_txt(\w+)_lbl$`)

func parseSyllabus(p *page) (*Syllabus, error) {
	v := func(key string) string { return p.text(detailPrefix + "dcl_" + key + "_lblCategory") }
	one := func(key string) string { return squash(v(key)) }

	code := one("syl_lct_cd")
	if code == "" {
		return nil, ErrSyllabusNotFound
	}
	s := &Syllabus{
		LectureCode:        code,
		NumberingCode:      one("num_cd_double"),
		Title:              one("lct_name_double"),
		Instructors:        one("syl_staff_name_double"),
		Term:               one("term_name"),
		DayPeriod:          one("day_period"),
		Faculty:            one("fac_name"),
		Room:               one("room_name"),
		Quota:              one("quota"),
		RegistrationMethod: one("regist_note"),
		TeachingFormat:     one("teaching_format"),
		MainCourse:         strings.Contains(one("main_mark"), "○"),
		ActiveLearning:     one("active_learning"),
		Outline:            v("outline"),
		URGCC:              one("urgcc"),
		Goals:              v("target"),
		Grading:            v("grading"),
		Requirements:       v("requirements"),
		Plan:               v("plan"),
		Preparation:        v("outside_info"),
		Review:             v("self_learning"),
		TextbookNote:       v("t_note"),
		ReferenceNote:      v("r_note"),
		Language:           one("use_language_cd"),
		Message:            v("message"),
		OfficeHours:        v("office_hours"),
		Website:            one("url"),
	}
	s.Year, _ = strconv.Atoi(one("lct_year"))
	s.Credits, _ = strconv.ParseFloat(strings.TrimSuffix(one("credits"), "単位"), 64)
	s.Slots = ParseSlots(s.DayPeriod)
	s.PlanSessions = parsePlanSessions(s.Plan)
	for _, t := range strings.Split(one("lct_Type"), "、") {
		if t = strings.TrimSpace(t); t != "" {
			s.LectureTypes = append(s.LectureTypes, t)
		}
	}

	type bookKey struct {
		kind string
		idx  int
	}
	books := map[bookKey]*Book{}
	for id, n := range p.ids {
		m := bookFieldRe.FindStringSubmatch(id)
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[2])
		k := bookKey{m[1], idx}
		b := books[k]
		if b == nil {
			b = &Book{}
			books[k] = b
		}
		val := squash(nodeText(n))
		switch m[3] {
		case "BookName":
			b.Title = val
		case "Author":
			b.Author = val
		case "Publisher":
			b.Publisher = val
		case "Year":
			b.Year = val
		case "Isbn":
			b.ISBN = val
		case "Series":
			b.Note = val
		}
	}
	keys := make([]bookKey, 0, len(books))
	for k := range books {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].idx < keys[j].idx })
	for _, k := range keys {
		b := books[k]
		if b.Title == "" {
			continue
		}
		if k.kind == "TextBook" {
			s.Textbooks = append(s.Textbooks, *b)
		} else {
			s.References = append(s.References, *b)
		}
	}
	return s, nil
}

var planSessionRE = regexp.MustCompile(`^第[\s　]*([0-9０-９]{1,3})[\s　]*回[\s　]*[:：.．、)）]?[\s　]*(.*)$`)
var planRangeRE = regexp.MustCompile(`^(?:[～〜~－—–-]|から|[・、]\s*第)`)

func parsePlanSessions(plan string) []SyllabusPlanSession {
	var entries []SyllabusPlanSession
	current := -1
	counts := map[int]int{}
	for _, line := range strings.Split(plan, "\n") {
		line = strings.TrimSpace(line)
		if m := planSessionRE.FindStringSubmatch(line); m != nil {
			current = -1
			// 複数回をまとめた見出しは、単一の授業回に割り当てない。原文で確認できる。
			if planRangeRE.MatchString(m[2]) {
				continue
			}
			n, _ := strconv.Atoi(normalizeLabel(m[1]))
			if n < 1 || n > 100 {
				continue
			}
			counts[n]++
			entries = append(entries, SyllabusPlanSession{No: n, Content: m[2]})
			current = len(entries) - 1
		} else if current >= 0 && line != "" {
			entries[current].Content = strings.TrimSpace(entries[current].Content + "\n" + line)
		}
	}
	var out []SyllabusPlanSession
	for _, e := range entries {
		// 重複する回番号は対応が曖昧なので除外する。
		if counts[e.No] == 1 && e.Content != "" {
			out = append(out, e)
		}
	}
	return out
}

var slotRe = regexp.MustCompile(`([月火水木金土日])\s*([1-8１-８])(?:\s*[〜~～-]\s*([1-8１-８]))?\s*(前半|後半)?`)

// ParseSlots は "月3" "月2,木3" "月3〜4前半" のような曜日時限表記をコマに分解する。
// "集中" "未定" など曜日時限を持たないものは nil。
func ParseSlots(dayPeriod string) []Slot {
	var out []Slot
	for _, m := range slotRe.FindAllStringSubmatch(dayPeriod, -1) {
		from := digit(m[2])
		to := from
		if m[3] != "" {
			to = digit(m[3])
		}
		for p := from; p <= to; p++ {
			out = append(out, Slot{Day: m[1], Period: p, Half: m[4]})
		}
	}
	return out
}

func digit(s string) int {
	r := []rune(s)[0]
	if r >= '１' && r <= '９' {
		return int(r-'１') + 1
	}
	return int(r - '0')
}

// realOptions は "-----" や "NULL" のようなプレースホルダ選択肢を除く。
func realOptions(opts []Option) []Option {
	out := make([]Option, 0, len(opts))
	for _, o := range opts {
		if o.Value == "NULL" || strings.Trim(o.Label, "-") == "" {
			continue
		}
		out = append(out, o)
	}
	return out
}

// optionAliases は表記ゆれを選択肢の表示名に寄せる。
var optionAliases = map[string]string{
	"前期": "前学期", "後期": "後学期", "1学期": "前学期", "2学期": "後学期",
	"月": "月曜日", "火": "火曜日", "水": "水曜日", "木": "木曜日", "金": "金曜日", "土": "土曜日",
}

// resolveOption は入力をコード値・表示名・部分一致の順に照合して選択肢の値を返す。
func resolveOption(opts []Option, input string, aliases map[string]string) (string, error) {
	in := normalizeLabel(input)
	if a, ok := aliases[in]; ok {
		in = a
	}
	opts = realOptions(opts)
	for _, o := range opts {
		if o.Value == input || o.Value == in {
			return o.Value, nil
		}
	}
	for _, o := range opts {
		if normalizeLabel(o.Label) == in {
			return o.Value, nil
		}
	}
	for _, o := range opts {
		if strings.HasPrefix(normalizeLabel(o.Label), in) {
			return o.Value, nil
		}
	}
	for _, o := range opts {
		if strings.Contains(normalizeLabel(o.Label), in) {
			return o.Value, nil
		}
	}
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.Label
	}
	return "", fmt.Errorf("%q に一致する選択肢がありません（候補: %s）", input, strings.Join(labels, ", "))
}

// normalizeLabel は全角数字・空白・「時限」表記などを揃えて比較しやすくする。
func normalizeLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r == ' ' || r == '　':
			return -1
		}
		return r
	}, s)
	s = strings.TrimSuffix(s, "時限")
	s = strings.TrimSuffix(s, "限")
	return s
}
