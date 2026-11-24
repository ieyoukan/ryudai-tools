package university

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

type TimetableCourse struct {
	LectureCode   string   `json:"lecture_code"`
	NumberingCode string   `json:"numbering_code,omitempty"`
	Title         string   `json:"title"`
	Credits       *float64 `json:"credits,omitempty"`
	Selection     string   `json:"selection,omitempty"`
	Capacity      *int     `json:"capacity,omitempty"` // published capacity, not current registrations
	TargetYear    string   `json:"target_year,omitempty"`
	DayPeriod     string   `json:"day_period,omitempty"`
	Room          string   `json:"room,omitempty"`
	Period        string   `json:"period,omitempty"`
	Notes         string   `json:"notes,omitempty"`
	Document      string   `json:"document"`
	Page          int      `json:"page"`
	Source        Source   `json:"source"`
}

type Timetable struct {
	Year        int               `json:"year"`
	Term        string            `json:"term"`
	Documents   []Document        `json:"documents"`
	Courses     []TimetableCourse `json:"courses"`
	Sources     []Source          `json:"sources"`
	Corrections []Passage         `json:"corrections,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
}

type Passage struct {
	Document string `json:"document"`
	Page     int    `json:"page"`
	Text     string `json:"text"`
	Source   Source `json:"source"`
}
type StudyRules struct {
	EnterYear  int         `json:"enter_year"`
	Faculty    string      `json:"faculty"`
	Course     string      `json:"course,omitempty"`
	Documents  []Document  `json:"documents"`
	Passages   []Passage   `json:"passages"`
	FusionRule *FusionRule `json:"fusion_rule,omitempty"`
	ThesisRule *ThesisRule `json:"thesis_rule,omitempty"`
	Sources    []Source    `json:"sources"`
	Warnings   []string    `json:"warnings,omitempty"`
}
type FusionRule struct {
	MinimumCredits float64 `json:"minimum_credits"`
	SameCourse     bool    `json:"same_course"`
	RequiresIntro  bool    `json:"requires_intro"`
	Evidence       Passage `json:"evidence"`
}

// ThesisRule は卒業研究（卒業設計）を登録する＝研究室で卒研に着手するための条件。
// コースごとに書き方が違うので、読み取れた種類の条件だけを埋める。読み取れない条件があり得るため、
// 判定結果と一緒に必ず Evidence（便覧の注記の原文）を見せること。
//
//   - 知能情報（R6）: 6個学期在学なら取得単位数 100 以上 → CreditsBySemesters、専門必修の全単位 → RequiresMajorRequired
//   - 社会基盤デザイン（R6）: 今後修得しなければならない単位数が卒研を含めて 20 以下 → MaxRemainingCredits、
//     卒研以外の専門必修の未修得が 4 科目以下 → MaxMissingMajorRequired、特定科目の修得 → RequiredCourses
//   - 建築学（R6）: 今後取得しなければならない単位数が卒設・卒研を含めて 24 以下 → MaxRemainingCredits
type ThesisRule struct {
	// CreditsBySemesters は在学学期数 → 必要な取得単位数。
	CreditsBySemesters map[int]float64 `json:"credits_by_semesters,omitempty"`
	// MaxRemainingCredits は「卒業要件を満たすために今後修得しなければならない単位数（卒研を含む）」の上限。0 は条件なし。
	MaxRemainingCredits float64 `json:"max_remaining_credits,omitempty"`
	// RequiresMajorRequired は「専門必修科目の全ての単位を取得」が条件に含まれるか（原則とされている場合も true）。
	RequiresMajorRequired bool `json:"requires_major_required,omitempty"`
	// MaxMissingMajorRequired は卒研以外の専門必修の未修得科目数の上限。nil は条件なし。
	MaxMissingMajorRequired *int `json:"max_missing_major_required,omitempty"`
	// RequiredCourses は修得していなければならない科目。内側はいずれか 1 つでよい（「A，もしくは，B」）。
	RequiredCourses [][]string `json:"required_courses,omitempty"`
	Evidence        Passage    `json:"evidence"`
}

// HasConditions は自動で判定できる条件を 1 つ以上読み取れたかを返す。
func (r *ThesisRule) HasConditions() bool {
	return r != nil && (len(r.CreditsBySemesters) > 0 || r.MaxRemainingCredits > 0 || r.RequiresMajorRequired ||
		r.MaxMissingMajorRequired != nil || len(r.RequiredCourses) > 0)
}

func CurrentYear() int {
	now := time.Now().In(Japan)
	y := now.Year()
	if now.Month() < time.April {
		y--
	}
	return y
}
func NormalizeTerm(t string) string {
	switch t {
	case "1", "前期", "前学期":
		return "前学期"
	case "2", "後期", "後学期":
		return "後学期"
	}
	return t
}
func DefaultTerm() string {
	m := time.Now().In(Japan).Month()
	if m >= time.April && m < time.October {
		return "前学期"
	}
	return "後学期"
}

func (c *Client) Timetable(ctx context.Context, year int, term, faculty, course string, codes []string) (*Timetable, error) {
	if year == 0 {
		year = CurrentYear()
	}
	term = NormalizeTerm(term)
	if term == "" {
		term = DefaultTerm()
	}
	if term != "前学期" && term != "後学期" {
		return nil, fmt.Errorf("term は前学期または後学期を指定してください")
	}
	if faculty == "" {
		faculty = "工学部"
	}
	page, indexSrc, err := c.yearPage(ctx, TimetableURL, year, term)
	if err != nil {
		return nil, err
	}
	data, src, err := c.fetch(ctx, page)
	if err != nil {
		return nil, err
	}
	docs, err := documents(data, page)
	if err != nil {
		return nil, err
	}
	r := &Timetable{Year: year, Term: term, Documents: []Document{}, Courses: []TimetableCourse{}, Sources: []Source{indexSrc, src}}
	want := map[string]bool{}
	for _, code := range codes {
		want[code] = true
	}
	for _, d := range docs {
		if d.Kind != "pdf" {
			continue
		}
		if d.Faculty != faculty {
			continue
		}
		// Include common and fusion subjects with a student's course timetable.
		if course != "" && !CourseMatches(d.Title, course) && !strings.Contains(d.Title, "共通") && !strings.Contains(d.Title, "融合") && !strings.Contains(d.Title, "訂正") {
			continue
		}
		if course != "" && strings.Contains(d.Title, "～H28") {
			continue
		}
		r.Documents = append(r.Documents, d)
		if strings.Contains(d.Title, "訂正") {
			b, ds, e := c.fetch(ctx, d.URL)
			if e != nil {
				r.Warnings = append(r.Warnings, "時間割訂正を取得できません: "+d.URL)
				continue
			}
			pages, e := c.documentPages(b, d)
			if e != nil {
				r.Warnings = append(r.Warnings, "時間割訂正をPDFとして確認できません: "+d.URL)
				continue
			}
			r.Sources = append(r.Sources, ds)
			for i, t := range pages {
				if strings.TrimSpace(t) != "" {
					r.Corrections = append(r.Corrections, Passage{Document: d.Title, Page: i + 1, Text: t, Source: ds})
				}
			}
			continue
		}
		b, ds, e := c.fetch(ctx, d.URL)
		if e != nil {
			r.Warnings = append(r.Warnings, d.Title+": "+e.Error())
			continue
		}
		rows, e := c.timetableRows(b, d, ds)
		if e != nil {
			r.Warnings = append(r.Warnings, d.Title+": "+e.Error())
			continue
		}
		r.Sources = append(r.Sources, ds)
		for _, row := range rows {
			if len(want) == 0 || want[row.LectureCode] {
				r.Courses = append(r.Courses, row)
			}
		}
	}
	if len(r.Documents) == 0 {
		return nil, fmt.Errorf("%s %s の時間割が見つかりません", faculty, course)
	}
	for _, src := range r.Sources {
		if src.Stale {
			r.Warnings = append(r.Warnings, src.Warning+"（"+src.URL+"）")
		}
	}
	if len(r.Corrections) > 0 {
		r.Warnings = append(r.Warnings, "訂正表は corrections に別途収録しています。科目一覧への自動反映はしていないため、訂正表の記載を優先して確認してください。")
	}
	return r, nil
}

// PDF strings emitted at identical coordinates are concatenated in drawing order.
func pdfTexts(ts []pdf.Text) []pdf.Text {
	var out []pdf.Text
	for _, t := range ts {
		t.S = strings.ReplaceAll(t.S, "�", "")
		if t.S == "" {
			continue
		}
		if len(out) > 0 {
			p := &out[len(out)-1]
			if math.Abs(p.Y-t.Y) < 0.8 && (math.Abs(p.X-t.X) < 0.8 || p.W > 0 && math.Abs(p.X+p.W-t.X) < 1) {
				p.S += t.S
				p.W = math.Max(p.W, t.X+t.W-p.X)
				continue
			}
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if math.Abs(out[i].Y-out[j].Y) < 1 {
			return out[i].X < out[j].X
		}
		return out[i].Y > out[j].Y
	})
	return out
}
func renderTexts(ts []pdf.Text) string {
	var b strings.Builder
	lastY := math.Inf(1)
	for _, t := range ts {
		if b.Len() > 0 {
			if math.Abs(t.Y-lastY) > 2 {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t.S)
		lastY = t.Y
	}
	return normalize(strings.TrimSpace(b.String()))
}

var lectureCodeRE = regexp.MustCompile(`^\d{9}$`)

// ParseTimetablePDF uses the actual grid rectangles, so notes above/below a
// course's baseline remain attached to that course rather than adjacent rows.
func ParseTimetablePDF(data []byte, doc Document, src Source) (out []TimetableCourse, err error) {
	defer func() {
		if v := recover(); v != nil {
			out = nil
			err = fmt.Errorf("時間割 PDF を読めません: %v", v)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if r.NumPage() > 100 {
		return nil, fmt.Errorf("時間割 PDF のページ数が多すぎます")
	}
	for p := 1; p <= r.NumPage(); p++ {
		content := r.Page(p).Content()
		texts := pdfTexts(content.Text)
		var xs []float64
		// Every column has a thin vertical border in the official Excel PDF.
		for _, rect := range content.Rect {
			if rect.Max.X-rect.Min.X < 2 && rect.Max.Y-rect.Min.Y > 12 {
				xs = append(xs, (rect.Min.X+rect.Max.X)/2)
			}
		}
		sort.Float64s(xs)
		var edges []float64
		for _, x := range xs {
			if len(edges) == 0 || x-edges[len(edges)-1] > 2 {
				edges = append(edges, x)
			}
		}
		if len(edges) < 10 {
			continue
		}
		for _, t := range texts {
			code := compact(t.S)
			if !lectureCodeRE.MatchString(code) {
				continue
			}
			col := -1
			for i := 0; i < len(edges)-1; i++ {
				if edges[i] <= t.X && t.X < edges[i+1] {
					col = i
					break
				}
			}
			if col != 0 {
				continue
			} // ignore lecture codes mentioned in notes
			top, bottom := math.Inf(1), math.Inf(-1)
			for _, rect := range content.Rect {
				if rect.Max.Y-rect.Min.Y > 2 || rect.Min.X > t.X || rect.Max.X < t.X {
					continue
				}
				y := (rect.Min.Y + rect.Max.Y) / 2
				if y > t.Y && y < top {
					top = y
				}
				if y < t.Y && y > bottom {
					bottom = y
				}
			}
			if math.IsInf(top, 0) || math.IsInf(bottom, 0) {
				continue
			}
			cells := make([][]pdf.Text, len(edges)-1)
			for _, v := range texts {
				if v.Y <= bottom || v.Y >= top {
					continue
				}
				for i := range cells {
					if v.X >= edges[i] && v.X < edges[i+1] {
						cells[i] = append(cells[i], v)
						break
					}
				}
			}
			// Find columns by their header text, retaining layout variations.
			head := make([][]pdf.Text, len(cells))
			for _, v := range texts {
				if v.Y <= top {
					continue
				}
				for i := range head {
					if v.X >= edges[i] && v.X < edges[i+1] {
						head[i] = append(head[i], v)
						break
					}
				}
			}
			get := func(label string) string {
				for i, h := range head {
					if strings.Contains(compact(renderTexts(h)), label) {
						return renderTexts(cells[i])
					}
				}
				return ""
			}
			row := TimetableCourse{LectureCode: code, NumberingCode: compact(get("科目番号")), Title: compact(get("科目名")), Selection: compact(get("必修")), TargetYear: compact(get("受講年次")), DayPeriod: compact(get("曜日")) + compact(get("時限")), Room: get("教室"), Period: compact(get("期間")), Notes: compact(get("備考")), Document: doc.Title, Page: p, Source: src}
			if v, e := strconv.ParseFloat(compact(get("単位")), 64); e == nil {
				row.Credits = &v
			}
			if v, e := strconv.Atoi(compact(get("登録人員"))); e == nil {
				row.Capacity = &v
			}
			if row.Title != "" {
				out = append(out, row)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("講義コードと時間割表の列を読み取れません")
	}
	return out, nil
}

func pdfPages(data []byte) (pages []string, err error) {
	defer func() {
		if v := recover(); v != nil {
			pages = nil
			err = fmt.Errorf("便覧 PDF を読めません: %v", v)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if r.NumPage() > 100 {
		return nil, fmt.Errorf("分冊 PDF を指定してください（100 ページまで）")
	}
	for p := 1; p <= r.NumPage(); p++ {
		pages = append(pages, pdfPageText(r, p))
	}
	return pages, nil
}

// pdfPageText は 1 ページ分の本文を返す。壊れたページ（ライブラリが panic する）は空にして、ほかのページは読む。
func pdfPageText(r *pdf.Reader, p int) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	return renderTexts(pdfTexts(r.Page(p).Content().Text))
}

var sameCourseRE = regexp.MustCompile(`(?:1つ|一つ|１つ|同一)の?コース.{0,40}?(\d+(?:\.\d+)?)単位以上`)

var (
	// 「※8 …卒業研究Iを登録するためには」「※9 卒業研究を履修するためには」「※8 卒業設計又は卒業研究を履修登録するためには」
	thesisNoteRE       = regexp.MustCompile(`※\d+[^※]{0,80}?卒業(?:設計又は卒業研究|研究(?:I|Ⅰ|１)?)を(?:履修登録|登録|履修)するためには[^※]*`)
	thesisCreditsRE    = regexp.MustCompile(`(\d+)個学期在学の学生については[，,、]?(?:取得|修得)単位数が(\d+)(?:単位)?以上`)
	thesisRemainingRE  = regexp.MustCompile(`今後(?:取得|修得)しなければならない単位数が[，,、]?[^，,。]{0,30}?合計(\d+)単位以下`)
	thesisRequiredRE   = regexp.MustCompile(`専門必修科目の全ての単位を(?:取得|修得)`)
	thesisMissingRE    = regexp.MustCompile(`専門必修の未修得科目数が(\d+)科目以下`)
	thesisCourseRE     = regexp.MustCompile(`・([^・。]{2,80}?)の単位を(?:取得|修得)していること`)
	thesisCourseSplitR = regexp.MustCompile(`[，,、]?(?:もしくは|又は|または)[，,、]?`)
	// 条件の中身は書かず「卒研着手条件をみたすこと」とだけある場合（理学部物理系 R6 など）
	thesisMentionRE = regexp.MustCompile(`[^。]{0,80}(?:卒研|卒業研究の?|卒業論文の?)着手(?:条件|要件)[^。]{0,80}`)
)

// thesisRuleFromPassage は便覧のページから卒業研究の登録条件の注記を探し、読み取れる条件を取り出す。
// 注記が無ければ nil。注記はあるが条件を読み取れない場合は、Evidence だけを持つ rule を返す。
func thesisRuleFromPassage(p Passage) *ThesisRule {
	text := compact(p.Text)
	note := thesisNoteRE.FindString(text)
	if note == "" {
		if m := thesisMentionRE.FindString(text); m != "" {
			p.Text = m
			return &ThesisRule{Evidence: p}
		}
		return nil
	}
	rule := &ThesisRule{}
	for _, m := range thesisCreditsRE.FindAllStringSubmatch(note, -1) {
		sem, _ := strconv.Atoi(m[1])
		credits, _ := strconv.ParseFloat(m[2], 64)
		if rule.CreditsBySemesters == nil {
			rule.CreditsBySemesters = map[int]float64{}
		}
		rule.CreditsBySemesters[sem] = credits
	}
	if m := thesisRemainingRE.FindStringSubmatch(note); m != nil {
		rule.MaxRemainingCredits, _ = strconv.ParseFloat(m[1], 64)
	}
	rule.RequiresMajorRequired = thesisRequiredRE.MatchString(note)
	if m := thesisMissingRE.FindStringSubmatch(note); m != nil {
		n, _ := strconv.Atoi(m[1])
		rule.MaxMissingMajorRequired = &n
	}
	for _, m := range thesisCourseRE.FindAllStringSubmatch(note, -1) {
		var anyOf []string
		for _, c := range thesisCourseSplitR.Split(m[1], -1) {
			if c = strings.Trim(c, "，,、「」"); c != "" {
				anyOf = append(anyOf, c)
			}
		}
		if len(anyOf) > 0 {
			rule.RequiredCourses = append(rule.RequiredCourses, anyOf)
		}
	}
	// 注記の後ろに表や別の説明がくっついていることがあるので、条件の文の終わりで切る
	if i := strings.LastIndex(note, "こと。"); i >= 0 {
		note = note[:i+len("こと。")]
	}
	p.Text = note
	rule.Evidence = p
	return rule
}

func fusionRuleFromPassage(p Passage) *FusionRule {
	text := compact(p.Text)
	for _, loc := range sameCourseRE.FindAllStringSubmatchIndex(text, -1) {
		before := []rune(text[:loc[0]])
		if len(before) > 40 {
			before = before[len(before)-40:]
		}
		match := text[loc[0]:loc[1]]
		if !strings.Contains(string(before)+match, "工学融合") {
			continue
		}
		n, _ := strconv.ParseFloat(text[loc[2]:loc[3]], 64)
		p.Text = match
		return &FusionRule{MinimumCredits: n, SameCourse: true, RequiresIntro: strings.Contains(match, "選択必修"), Evidence: p}
	}
	return nil
}

func (c *Client) StudyRules(ctx context.Context, enterYear int, faculty, course, keyword string) (*StudyRules, error) {
	if enterYear < 2000 || enterYear > CurrentYear()+1 {
		return nil, fmt.Errorf("enter_year に適用便覧の年度を指定してください")
	}
	if faculty == "" {
		faculty = "工学部"
	}
	page, indexSrc, err := c.yearPage(ctx, HandbookURL, enterYear, "")
	if err != nil {
		return nil, err
	}
	data, pageSrc, err := c.fetch(ctx, page)
	if err != nil {
		return nil, err
	}
	docs, err := documents(data, page)
	if err != nil {
		return nil, err
	}
	r := &StudyRules{EnterYear: enterYear, Faculty: faculty, Course: course, Documents: []Document{}, Passages: []Passage{}, Sources: []Source{indexSrc, pageSrc}, Warnings: []string{"入学年度に対応する便覧です。編入・転コース等で適用便覧が異なる場合はその年度を指定してください。"}}
	courseRead, truncated := false, false
	for _, d := range docs {
		if d.Kind != "pdf" || d.Faculty != faculty {
			continue
		}
		if course != "" && !CourseMatches(d.Title, course) && !strings.Contains(d.Title, "規程") && !strings.Contains(d.Title, "正誤") {
			continue
		}
		r.Documents = append(r.Documents, d)
		if len(r.Documents) > 12 {
			r.Warnings = append(r.Warnings, "course を指定するとコース別資料を読めます")
			break
		}
		b, src, e := c.fetch(ctx, d.URL)
		if e != nil {
			r.Warnings = append(r.Warnings, d.Title+": "+e.Error())
			continue
		}
		pages, e := c.documentPages(b, d)
		if e != nil {
			r.Warnings = append(r.Warnings, d.Title+": "+e.Error())
			continue
		}
		r.Sources = append(r.Sources, src)
		if course != "" && CourseMatches(d.Title, course) {
			courseRead = true
		}
		for i, t := range pages {
			ct := compact(t)
			// Only derive a rule from the requested course's own curriculum.
			if course != "" && CourseMatches(d.Title, course) {
				p := Passage{Document: d.Title, Page: i + 1, Text: t, Source: src}
				if rule := fusionRuleFromPassage(p); rule != nil {
					r.FusionRule = rule
				}
				if rule := thesisRuleFromPassage(p); rule != nil && r.ThesisRule == nil {
					r.ThesisRule = rule
				}
			}
			if keyword != "" {
				if !strings.Contains(ct, compact(keyword)) {
					continue
				}
			} else if !strings.Contains(ct, "卒業") && !strings.Contains(ct, "履修") && !strings.Contains(ct, "修得") {
				continue
			}
			// Keep each page intact (tables and notes), with pagination and source.
			if len(r.Passages) < 20 {
				r.Passages = append(r.Passages, Passage{Document: d.Title, Page: i + 1, Text: t, Source: src})
			} else {
				truncated = true
			}
		}
	}
	if len(r.Documents) == 0 {
		return nil, fmt.Errorf("%d 年度 %s %s の便覧資料が見つかりません", enterYear, faculty, course)
	}
	if course != "" && !courseRead {
		r.Warnings = append(r.Warnings, "指定コースの便覧本文は未取得です。返された学部規程だけでコース別の要件を満たすとは判断できません。")
	}
	if truncated {
		r.Warnings = append(r.Warnings, "本文は条件に合う先頭20ページまでです。keyword で絞り込んでください")
	}
	return r, nil
}
