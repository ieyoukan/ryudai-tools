package webclass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

// Message は WebClass 受信箱の1メッセージ。
type Message struct {
	ID            string `json:"id"`
	From          string `json:"from"`
	FromID        string `json:"from_id"`
	Subject       string `json:"subject"`
	HasAttachment bool   `json:"has_attachment"`
	Date          string `json:"date"`
	IsRead        bool   `json:"is_read"`
	ViewURL       string `json:"view_url"`
}

// MessageCount は mbl.php API が返す未読件数。
type MessageCount struct {
	Count int `json:"count"`
}

// FetchInboxMessages は受信箱のメッセージ一覧を取得する。
// excludeSelfID が空でない場合、FromID が一致する自己通知（レポート受取等）を除外する。
func (c *Client) FetchInboxMessages(excludeSelfID string) ([]Message, error) {
	acs := c.getOrFetchACSToken()
	resp, err := c.FetchPage(inboxPath(acs))
	if err != nil {
		return nil, fmt.Errorf("受信箱取得: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := checkLoggedIn(body); err != nil {
		return nil, err
	}

	return parseMessageTable(body, c.BaseHost(), excludeSelfID), nil
}

// FetchUnreadMessageCount は mbl.php のメッセージカウント API を呼ぶ。
// notCourse が true だと ft[not_course]=1 を付け、コース自動通知を除外する。
func (c *Client) FetchUnreadMessageCount(notCourse bool) (int, error) {
	u := c.BaseURL + "/mbl.php/messages/api/count?ft[unread]=1"
	if notCourse {
		u += "&ft[not_course]=1"
	}

	body, err := c.FetchBodyAbs(u)
	if err != nil {
		return 0, fmt.Errorf("未読数 API: %w", err)
	}

	var mc MessageCount
	if err := json.Unmarshal(body, &mc); err != nil {
		return 0, fmt.Errorf("未読数レスポンスのパース: %w", err)
	}
	return mc.Count, nil
}

// ---------------------------------------------------------------------------
// 内部実装
// ---------------------------------------------------------------------------

func inboxPath(acs string) string {
	p := "msg_editor.php?msgappmode=inbox"
	if acs != "" {
		p += "&acs_=" + acs
	}
	return p
}

// getOrFetchACSToken は既存キャッシュまたはトップページから acs_ を取得する。
func (c *Client) getOrFetchACSToken() string {
	c.acsMu.Lock()
	for _, v := range c.acsCache {
		if v != "" {
			c.acsMu.Unlock()
			return v
		}
	}
	c.acsMu.Unlock()

	resp, err := c.FollowJSRedirect("")
	if err != nil {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return ""
	}
	return extractACSToken(body)
}

// --- 受信箱テーブルパース ---------------------------------------------------

// parseMessageTable は受信箱 HTML をパースして Message 一覧を返す。
// excludeSelfID が空でなければ、FromID が一致するメッセージを除外する。
func parseMessageTable(body []byte, baseHost string, excludeSelfID string) []Message {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}

	table := webclient.FindTableByID(doc, "MsgListTable")
	if table == nil {
		table = webclient.FindTableByClass(doc, "msg")
	}
	if table == nil {
		return nil
	}

	rows := webclient.TableRows(table)
	var messages []Message
	for _, row := range rows {
		cells := webclient.RowCells(row)
		if len(cells) < 6 {
			continue
		}

		msg := parseMessageRow(cells)
		if msg == nil {
			continue
		}
		msg.ViewURL = baseHost + "/webclass/msg_viewer.php?uomsgid=" + msg.ID

		if excludeSelfID != "" && msg.FromID == excludeSelfID {
			continue
		}
		messages = append(messages, *msg)
	}
	return messages
}

// parseMessageRow は1行（<tr> 内の <td> 配列）から Message を組み立てる。
func parseMessageRow(cells []*xhtml.Node) *Message {
	// [0] checkbox, [1] 差出人, [2] ユーザID, [3] 件名, [4] 添付, [5] 日付
	id := extractMessageID(cells[0])
	if id == "" {
		return nil
	}

	isRead := !cellHasBold(cells[1])

	msg := &Message{
		ID:            id,
		From:          strings.TrimSpace(webclient.TextContent(cells[1])),
		FromID:        strings.TrimSpace(webclient.TextContent(cells[2])),
		Subject:       cleanSubject(webclient.TextContent(cells[3])),
		HasAttachment: hasAttachment(cells[4]),
		Date:          strings.TrimSpace(webclient.TextContent(cells[5])),
		IsRead:        isRead,
	}
	return msg
}

// extractMessageID はチェックボックスからメッセージ ID を取り出す。
func extractMessageID(cell *xhtml.Node) string {
	var id string
	var find func(*xhtml.Node)
	find = func(n *xhtml.Node) {
		if id != "" {
			return
		}
		if n.Type == xhtml.ElementNode && n.Data == "input" {
			attrs := webclient.AttrMap(n)
			if attrs["type"] == "checkbox" && attrs["value"] != "" {
				id = attrs["value"]
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(cell)
	return id
}

// cellHasBold はセル内に <b> タグがあるか（≒未読か）を返す。
func cellHasBold(cell *xhtml.Node) bool {
	var found bool
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if found {
			return
		}
		if n.Type == xhtml.ElementNode && n.Data == "b" {
			found = true
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(cell)
	return found
}

// cleanSubject は件名テキストの前後の空白と未読マークの残骸を除去する。
func cleanSubject(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// hasAttachment は添付ファイルセルが空（&nbsp; のみ）でないかを返す。
func hasAttachment(cell *xhtml.Node) bool {
	html := webclient.TextContent(cell)
	html = strings.TrimSpace(html)
	if html == "" || html == "\u00a0" {
		return false
	}
	return true
}
