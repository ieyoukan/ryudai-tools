package webclass

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

// jst は日本標準時（UTC+9）。
var jst = time.FixedZone("JST", 9*60*60)

// AttendanceItem は出席確認アイテムの情報。
type AttendanceItem struct {
	Name        string
	Status      string    // "出席" | "遅刻" | "欠席" | ""
	WindowOpen  time.Time // 出席扱い開始
	WindowClose time.Time // 出席扱い終了（0 値は無制限）
}

// IsOpenAt は指定時刻に出席ウィンドウが開いているか返す。
func (a AttendanceItem) IsOpenAt(t time.Time) bool {
	if a.WindowOpen.IsZero() || t.Before(a.WindowOpen) {
		return false
	}
	return a.WindowClose.IsZero() || !t.After(a.WindowClose)
}

var (
	acsTokenPattern          = regexp.MustCompile(`[?&]acs_=([a-f0-9]+)`)
	attendanceContentIDRegex = regexp.MustCompile(`set_contents_id=([a-f0-9]+)`)
	attendanceWindowRegex    = regexp.MustCompile(`出席扱い\s*:\s*(\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2})\s*-\s*(\d{2}:\d{2})`)
)

// extractACSToken はページ HTML から acs_ トークンを取り出す。
func extractACSToken(body []byte) string {
	if m := acsTokenPattern.FindSubmatch(body); len(m) >= 2 {
		return string(m[1])
	}
	return ""
}

// FetchAttendanceList はコースの出席確認一覧を取得する。
func (c *Client) FetchAttendanceList(courseID string) ([]AttendanceItem, error) {
	resp, err := c.FollowJSRedirect(fmt.Sprintf("course.php/%s/login", courseID))
	if err != nil {
		return nil, fmt.Errorf("コースログイン: %w", err)
	}
	courseBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("course.php/%s/attendance", courseID)
	if acs := extractACSToken(courseBody); acs != "" {
		path += "?acs_=" + acs
	}

	resp2, err := c.FetchPage(path)
	if err != nil {
		return nil, fmt.Errorf("出席一覧取得: %w", err)
	}
	defer resp2.Body.Close()
	body, err := io.ReadAll(resp2.Body)
	if err != nil {
		return nil, err
	}

	return parseAttendanceTable(body), nil
}

func parseAttendanceTable(body []byte) []AttendanceItem {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var items []AttendanceItem
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if item, ok := parseAttendanceRow(n); ok {
				items = append(items, item)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return items
}

func parseAttendanceRow(row *html.Node) (AttendanceItem, bool) {
	cells := webclient.RowCells(row)
	if len(cells) < 6 {
		return AttendanceItem{}, false
	}

	var linkHref string
	var walkLink func(*html.Node)
	walkLink = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if h := webclient.AttrMap(n)["href"]; h != "" {
				linkHref = h
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walkLink(child)
		}
	}
	walkLink(cells[0])

	if !attendanceContentIDRegex.MatchString(linkHref) {
		return AttendanceItem{}, false
	}

	name := cleanCourseName(webclient.TextContent(cells[0]))
	status := strings.TrimSpace(webclient.TextContent(cells[1]))

	timeText := webclient.TextContent(cells[5])
	item := AttendanceItem{
		Name:   name,
		Status: status,
	}

	if wm := attendanceWindowRegex.FindStringSubmatch(timeText); len(wm) >= 3 {
		if t, err := time.ParseInLocation("2006/01/02 15:04", wm[1], jst); err == nil {
			item.WindowOpen = t
		}
		if wm[2] != "" && !item.WindowOpen.IsZero() {
			dateStr := strings.SplitN(wm[1], " ", 2)[0]
			if t, err := time.ParseInLocation("2006/01/02 15:04", dateStr+" "+wm[2], jst); err == nil {
				item.WindowClose = t
			}
		}
	}

	return item, true
}
