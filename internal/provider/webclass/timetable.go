package webclass

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

var dayNames = []string{"月", "火", "水", "木", "金", "土"}

// periodNumPattern は "1" "1限" "第1限" などから数字を抽出する。
var periodNumPattern = regexp.MustCompile(`(\d+)`)

// courseURLPattern はコースページへのリンクから ID を抽出する。
var courseURLPattern = regexp.MustCompile(`course\.php/([^/]+)/login`)

// FetchTimetable はトップページのスケジュールテーブルからコース一覧を取得する。
func (c *Client) FetchTimetable() ([]CourseInfo, error) {
	resp, err := c.FollowJSRedirect("")
	if err != nil {
		return nil, fmt.Errorf("トップページ取得: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("トップページ読み取り: %w", err)
	}
	if err := checkLoggedIn(body); err != nil {
		return nil, err
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("HTML パース: %w", err)
	}

	courses := parseScheduleTable(doc)
	if len(courses) == 0 {
		courses = scanCourseLinks(doc)
	}
	return courses, nil
}

// parseScheduleTable は class="schedule-table" のテーブルを解析してコース一覧を返す。
func parseScheduleTable(doc *html.Node) []CourseInfo {
	table := webclient.FindTableByClass(doc, "schedule-table")
	if table == nil {
		return nil
	}

	rows := webclient.TableRows(table)
	if len(rows) < 2 {
		return nil
	}

	dayColumns := parseDayColumns(rows[0])
	if len(dayColumns) == 0 {
		return nil
	}

	var courses []CourseInfo
	for _, row := range rows[1:] {
		cells := webclient.RowCells(row)
		if len(cells) == 0 {
			continue
		}

		periodText := strings.TrimSpace(webclient.TextContent(cells[0]))
		m := periodNumPattern.FindString(periodText)
		if m == "" {
			continue
		}
		period, _ := strconv.Atoi(m)

		for _, dc := range dayColumns {
			if dc.index >= len(cells) {
				continue
			}
			for _, course := range extractCoursesFromCell(cells[dc.index]) {
				course.Day = dc.day
				course.Period = period
				courses = append(courses, *course)
			}
		}
	}
	return courses
}

type dayColumn struct {
	index int
	day   string
}

// parseDayColumns はヘッダー行を解析して列インデックス→曜日のマッピングを返す。
func parseDayColumns(headerRow *html.Node) []dayColumn {
	cells := webclient.RowCells(headerRow)
	var result []dayColumn
	for i, cell := range cells {
		text := strings.TrimSpace(webclient.TextContent(cell))
		for _, day := range dayNames {
			if strings.Contains(text, day) {
				result = append(result, dayColumn{index: i, day: day})
				break
			}
		}
	}
	return result
}

// newMsgPattern は「新着メッセージ(N)」を除去するために使用する。
var newMsgPattern = regexp.MustCompile(`新着メッセージ\(\d+\)`)

// cleanCourseName は course 名から余分なテキストを除去して整形する。
func cleanCourseName(raw string) string {
	name := newMsgPattern.ReplaceAllString(raw, "")
	name = strings.TrimPrefix(strings.TrimSpace(name), "» ")
	return strings.TrimSpace(name)
}

// extractCoursesFromCell は1セル内の全コースリンクを返す（複数コース対応）。
func extractCoursesFromCell(cell *html.Node) []*CourseInfo {
	var courses []*CourseInfo
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := webclient.AttrMap(n)["href"]
			if m := courseURLPattern.FindStringSubmatch(href); len(m) > 1 {
				name := cleanCourseName(webclient.TextContent(n))
				if name != "" {
					courses = append(courses, &CourseInfo{ID: m[1], Name: name})
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(cell)
	return courses
}

// scanCourseLinks は schedule-table が無い場合のフォールバック（全リンク走査）。
func scanCourseLinks(doc *html.Node) []CourseInfo {
	seen := make(map[string]bool)
	var courses []CourseInfo
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := webclient.AttrMap(n)["href"]
			if m := courseURLPattern.FindStringSubmatch(href); len(m) > 1 {
				id := m[1]
				if !seen[id] {
					seen[id] = true
					courses = append(courses, CourseInfo{ID: id, Name: cleanCourseName(webclient.TextContent(n))})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return courses
}
