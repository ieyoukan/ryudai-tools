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
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

type RequiredSubject struct {
	NumberingCode string  `json:"numbering_code"`
	Title         string  `json:"title"`
	Credits       float64 `json:"credits"`
	Selection     string  `json:"selection"` // required / selective_required
	Category      string  `json:"category"`
	Document      string  `json:"document"`
	Page          int     `json:"page"`
	Source        Source  `json:"source"`
}

type CourseRequirements struct {
	Course   string            `json:"course"`
	Subjects []RequiredSubject `json:"subjects"`
	Rules    []Passage         `json:"rules"` // graduation conditions, including selective requirements
}

type RequiredCatalog struct {
	EnterYear int                  `json:"enter_year"`
	Faculty   string               `json:"faculty"`
	Scope     string               `json:"scope"`
	Courses   []CourseRequirements `json:"courses"`
	Sources   []Source             `json:"sources"`
	Warnings  []string             `json:"warnings"`
}

var subjectNumberRE = regexp.MustCompile(`^(工共|機械|エネ|電気|電情|社基|建築|知能)\d{3}$`)
var bareSubjectNumberRE = regexp.MustCompile(`^\d{3}$`)

func CourseMatches(title, course string) bool {
	if course == "" {
		return false
	}
	canonical := func(s string) string {
		return strings.ReplaceAll(compact(s), "エネルギー機械工学", "エネルギー環境工学")
	}
	return strings.Contains(canonical(title), canonical(course))
}

// AffiliationKey は所属（コース、無ければ学科）から、便覧の資料名と照らし合わせる名前を返す。
// 「コース」「学科」「プログラム」を外すので、「亜熱帯生物資源科学科（生物機能開発学、食品機能科学、発酵・生命科学コース）」の
// ように複数のコースを 1 つの資料にまとめた資料名でも、「食品機能科学コース」の学生の資料として見つかる。
func AffiliationKey(course, department string) string {
	for _, s := range []string{course, department} {
		s = compact(s)
		for _, suffix := range []string{"コース", "プログラム", "学科", "専攻"} {
			s = strings.TrimSuffix(s, suffix)
		}
		if utf8.RuneCountInString(s) >= 2 {
			return s
		}
	}
	return ""
}

// ParseRequiredSubjects reads only the course's classification table. A circle
// in a neighbouring column/row or an example study plan never becomes a requirement.
func ParseRequiredSubjects(data []byte, doc Document, src Source) (out []RequiredSubject, err error) {
	defer func() {
		if v := recover(); v != nil {
			out, err = nil, fmt.Errorf("必修科目表のPDFを読めません: %v", v)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if r.NumPage() > 100 {
		return nil, fmt.Errorf("分冊便覧は100ページまでです")
	}
	seen := map[string]RequiredSubject{}
	for page := 1; page <= r.NumPage(); page++ {
		content := r.Page(page).Content()
		texts := pdfTexts(content.Text)
		t := compact(renderTexts(texts))
		if !strings.Contains(t, "授業科目分類表") || !strings.Contains(t, "必修") {
			continue
		}
		var xs []float64
		for _, rect := range content.Rect {
			if rect.Max.X-rect.Min.X < 2 && rect.Max.Y-rect.Min.Y > 100 {
				xs = append(xs, rect.Min.X)
			}
		}
		sort.Float64s(xs)
		var edges []float64
		for _, x := range xs {
			if len(edges) == 0 || x-edges[len(edges)-1] > 2 {
				edges = append(edges, x)
			}
		}
		if len(edges) != 11 { // two tables, each classification/number/title/credits/selection
			return nil, fmt.Errorf("必修科目表の列配置を確認できません（%s %dページ）", doc.Title, page)
		}
		for _, col := range []int{1, 6} {
			expectedMarks, matchedMarks := 0, 0
			for _, v := range texts {
				if v.X >= edges[col+3] && v.X < edges[col+4] {
					switch compact(v.S) {
					case "○", "◯", "〇", "選必", "選択必修":
						expectedMarks++
					}
				}
			}
			var numberLines []pdf.Text
			for _, v := range texts {
				if v.X < edges[col] || v.X >= edges[col+1] {
					continue
				}
				if len(numberLines) > 0 && math.Abs(numberLines[len(numberLines)-1].Y-v.Y) < 1 {
					numberLines[len(numberLines)-1].S += v.S
				} else {
					numberLines = append(numberLines, v)
				}
			}
			// Some PDF fonts omit the prefix for an otherwise legible number.
			// Restore it only when the nearest numbered subjects on BOTH sides
			// use the same prefix; never cross a classification boundary.
			for i := range numberLines {
				bare := compact(numberLines[i].S)
				if !bareSubjectNumberRE.MatchString(bare) {
					continue
				}
				before, after := "", ""
				for j := i - 1; j >= 0; j-- {
					if m := subjectNumberRE.FindStringSubmatch(compact(numberLines[j].S)); m != nil {
						before = m[1]
						break
					}
				}
				for j := i + 1; j < len(numberLines); j++ {
					if m := subjectNumberRE.FindStringSubmatch(compact(numberLines[j].S)); m != nil {
						after = m[1]
						break
					}
				}
				if before != "" && before == after {
					numberLines[i].S = before + bare
				}
			}
			var codes []pdf.Text
			for _, v := range numberLines {
				if subjectNumberRE.MatchString(compact(v.S)) {
					codes = append(codes, v)
				}
			}
			for _, code := range codes {
				top, bottom := code.Y+20, code.Y-20
				// Even an unreadable optional subject's number line is a row
				// boundary. Otherwise its name/credits can leak into a required row.
				for _, line := range numberLines {
					if line.Y > code.Y+1 {
						top = math.Min(top, (line.Y+code.Y)/2)
					}
					if line.Y < code.Y-1 {
						bottom = math.Max(bottom, (line.Y+code.Y)/2)
					}
				}
				// Credits remain present on rows with no printed subject number
				// (e.g. grouped optional special lectures), and delimit those rows.
				for _, v := range texts {
					if v.X < edges[col+2] || v.X >= edges[col+3] {
						continue
					}
					if _, e := strconv.ParseFloat(compact(v.S), 64); e != nil {
						continue
					}
					if v.Y > code.Y+1 {
						top = math.Min(top, (v.Y+code.Y)/2)
					}
					if v.Y < code.Y-1 {
						bottom = math.Max(bottom, (v.Y+code.Y)/2)
					}
				}
				cell := func(offset int) string {
					var values []pdf.Text
					for _, v := range texts {
						if v.X >= edges[col+offset] && v.X < edges[col+offset+1] && v.Y < top && v.Y > bottom {
							values = append(values, v)
						}
					}
					return compact(renderTexts(values))
				}
				mark := cell(3)
				selection := ""
				switch mark {
				case "○", "◯", "〇":
					selection = "required"
				case "選必", "選択必修":
					selection = "selective_required"
				case "":
					continue
				default:
					return nil, fmt.Errorf("必修表示 %q を確認できません（%s %dページ %s）", mark, doc.Title, page, compact(code.S))
				}
				number, title := compact(code.S), cell(1)
				matchedMarks++
				credits, e := strconv.ParseFloat(cell(2), 64)
				if e != nil || credits <= 0 || title == "" {
					return nil, fmt.Errorf("必修科目の名称・単位を確認できません: %s", number)
				}
				category := "コース専門科目"
				if strings.HasPrefix(number, "工共") {
					category = "工学共通科目"
				}
				row := RequiredSubject{NumberingCode: number, Title: title, Credits: credits, Selection: selection, Category: category, Document: doc.Title, Page: page, Source: src}
				if old, ok := seen[number]; ok {
					if old.Title != title || old.Credits != credits || old.Selection != selection {
						return nil, fmt.Errorf("必修科目表の記載が競合しています: %s", number)
					}
					continue
				}
				seen[number] = row
				out = append(out, row)
			}
			if matchedMarks != expectedMarks {
				return nil, fmt.Errorf("必修表示の一部を科目番号と対応付けられません（%s %dページ）", doc.Title, page)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("必修科目表から科目を取得できません: %s", doc.Title)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NumberingCode < out[j].NumberingCode })
	return out, nil
}

func (c *Client) RequiredCourses(ctx context.Context, year int, faculty, course string) (*RequiredCatalog, error) {
	if year < 2000 || year > CurrentYear()+1 {
		return nil, fmt.Errorf("enter_year に適用便覧年度を指定してください")
	}
	if faculty == "" {
		faculty = "工学部"
	}
	if faculty != "工学部" {
		return nil, fmt.Errorf("必修科目表の構造化は現在工学部に対応しています。他学部は get_study_rules で便覧を確認してください")
	}
	page, indexSource, err := c.yearPage(ctx, HandbookURL, year, "")
	if err != nil {
		return nil, err
	}
	data, pageSource, err := c.fetch(ctx, page)
	if err != nil {
		return nil, err
	}
	docs, err := documents(data, page)
	if err != nil {
		return nil, err
	}
	out := &RequiredCatalog{EnterYear: year, Faculty: faculty, Scope: "専門教育の授業科目分類表（工学共通・コース専門科目）", Courses: []CourseRequirements{}, Sources: []Source{indexSource, pageSource}, Warnings: []string{
		"○を必修、選必を選択必修として取得します。選択必修の候補すべてを必修とは判定しません。選択の組合せ・必要単位は rules の卒業要件を確認してください。",
		"共通教育・専門基礎教育の外国語・先修科目等はこの一覧の対象外です。卒業要件の区分別単位と便覧本文も合わせて確認してください。",
	}}
	for _, d := range docs {
		if d.Faculty != faculty || d.Kind != "pdf" {
			continue
		}
		if strings.Contains(d.Title, "正誤") {
			out.Warnings = append(out.Warnings, "便覧の正誤表も確認してください: "+d.URL)
		}
		if !strings.HasSuffix(d.Title, "コース") || course != "" && !CourseMatches(d.Title, course) {
			continue
		}
		b, source, e := c.fetch(ctx, d.URL)
		if e != nil {
			out.Warnings = append(out.Warnings, d.Title+": "+e.Error())
			continue
		}
		out.Sources = append(out.Sources, source)
		subjects, e := ParseRequiredSubjects(b, d, source)
		if e != nil {
			out.Warnings = append(out.Warnings, e.Error())
			continue
		}
		entry := CourseRequirements{Course: d.Title, Subjects: subjects, Rules: []Passage{}}
		pages, e := pdfPages(b)
		if e != nil {
			out.Warnings = append(out.Warnings, d.Title+": "+e.Error())
		} else {
			for i, text := range pages {
				ct := compact(text)
				if strings.Contains(ct, "卒業要件") && !strings.Contains(ct, "・卒業要件") && len([]rune(ct)) > 300 {
					entry.Rules = append(entry.Rules, Passage{Document: d.Title, Page: i + 1, Text: text, Source: source})
				}
			}
		}
		out.Courses = append(out.Courses, entry)
	}
	for _, src := range out.Sources {
		if src.Stale {
			out.Warnings = append(out.Warnings, src.Warning+"（"+src.URL+"）")
		}
	}
	if len(out.Courses) == 0 {
		return nil, fmt.Errorf("%d年度 %s %s の必修科目を確認できません: %s", year, faculty, course, strings.Join(out.Warnings, "; "))
	}
	return out, nil
}
