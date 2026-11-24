package webclass

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/ieyoukan/daigaku-hack/internal/webclient"
)

// navTextURLRe は txtbk_show_chapter.php の js-config JSON から text_url を抽出する。
var navTextURLRe = regexp.MustCompile(`"text_url"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// TextURL は txtbk_show_chapter.php の JSON から本文 URL を絶対 URL で返す。
func (c *Client) TextURL(body []byte, base string) string {
	m := navTextURLRe.FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return c.ResolveURL(strings.ReplaceAll(string(m[1]), `\/`, "/"), base)
}

// StartShowInfo は show_info.php の「開始」フォームを送信し、レスポンスを返す。
// フォーム内の全 hidden フィールドと next=開始 を送る。
// finalURL は HTTP リダイレクト後の URL（多くは JS リダイレクトページなので JSRedirect で追う）。
func (c *Client) StartShowInfo(body []byte, pageURL string) (respBody []byte, ct, finalURL string, err error) {
	action := c.FormActionURL(body, pageURL)
	if action == "" {
		return nil, "", "", fmt.Errorf("show_info.php のフォームが見つかりません")
	}
	vals := url.Values{"next": {"開始"}}
	for name, value := range webclient.HiddenInputsFromHTML(body) {
		vals.Set(name, value)
	}
	return c.PostFormAbsFull(action, vals)
}
