package webclass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"

	lpdf "github.com/ledongthuc/pdf"
	xhtml "golang.org/x/net/html"

	"github.com/ieyoukan/daigaku-hack/internal/webclient"
)

// pdfCache はPDFテキスト抽出結果のメモリキャッシュ。
var pdfCache sync.Map

func extractPageText(body []byte) string {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}

	skipTags := map[string]bool{
		"script": true, "style": true, "noscript": true,
		"nav": true, "header": true, "footer": true,
	}

	var lines []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if skipTags[n.Data] {
				return
			}
			if n.Data == "br" || n.Data == "p" || n.Data == "div" ||
				n.Data == "li" || n.Data == "tr" ||
				n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "h4" {
				lines = append(lines, "")
			}
		}
		if n.Type == xhtml.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				lines = append(lines, text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	// 連続空行を1つに
	var cleaned []string
	prevEmpty := false
	for _, line := range lines {
		if line == "" {
			if !prevEmpty {
				cleaned = append(cleaned, "")
				prevEmpty = true
			}
		} else {
			cleaned = append(cleaned, line)
			prevEmpty = false
		}
	}
	return strings.Join(cleaned, "\n")
}

// extractPDFViewerText は PDF ビューアの HTML からテキストレイヤーの文字を抽出する。
// loadit.php?action=pdfViewer は各ページを画像+テキストスパンで表示する。
func extractPDFViewerText(body []byte) string {
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}

	var lines []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			// テキストレイヤー内の span だけを対象にする
			if n.Data == "span" && hasClass(n, "textLayer") {
				return // textLayer 自体は div、中の span を再帰で処理
			}
			if n.Data == "div" && hasClass(n, "textLayer") {
				// textLayer 内の全 span からテキストを収集
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == xhtml.ElementNode && c.Data == "span" {
						text := strings.TrimSpace(webclient.TextContent(c))
						if text != "" {
							lines = append(lines, text)
						}
					}
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	result := strings.Join(lines, "\n")
	if len(result) > 32000 {
		result = result[:32000] + "\n...(省略)"
	}
	return result
}

func hasClass(n *xhtml.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

// extractPDFText は PDF ファイルをダウンロードしてテキストを抽出する。
// textURL から file= と contents_url= を抽出し PDF の絶対 URL を構築する。

func (c *Client) extractPDFText(host, textURL string) string {
	u, err := url.Parse(textURL)
	if err != nil {
		return ""
	}
	q := u.Query()

	fileParam := q.Get("file")
	if fileParam == "" {
		return ""
	}
	contentsURL := q.Get("contents_url")
	contentsURL = strings.ReplaceAll(contentsURL, "&amp;", "&")

	var pdfURL string
	if strings.HasPrefix(contentsURL, "/") {
		pdfURL = host + contentsURL
	} else {
		pdfURL = host + "/" + contentsURL
	}
	if !strings.HasSuffix(pdfURL, "/") {
		pdfURL += "/"
	}
	pdfURL += strings.ReplaceAll(fileParam, "&amp;", "&")

	// キャッシュ確認
	cacheKey := fileParam
	if cached, ok := pdfCache.Load(cacheKey); ok {
		return cached.(string)
	}

	resp, err := c.FetchURL(pdfURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	tmpFile, err := os.CreateTemp("", "wc-pdf-*.pdf")
	if err != nil {
		return ""
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(body); err != nil {
		tmpFile.Close()
		return ""
	}
	tmpFile.Close()

	pdfFile, pdfReader, err := lpdf.Open(tmpFile.Name())
	if err != nil {
		return ""
	}
	defer pdfFile.Close()

	var lines []string
	for i := 1; i <= pdfReader.NumPage(); i++ {
		page := pdfReader.Page(i)
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		text = mergeFragmentedLines(text)
		if text != "" {
			lines = append(lines, text)
		}
	}
	result := strings.Join(lines, "\n")
	pdfCache.Store(cacheKey, result)
	return result
}

// FetchContentText はコンテンツの教材本文からテキストを抽出して返す。
// 資料以外は ErrNotMaterial を返す。
func (c *Client) FetchContentText(courseID, contentID string) (string, error) {
	if err := c.requireMaterial(courseID, contentID); err != nil {
		return "", err
	}
	acs := c.CourseACSToken(courseID)

	body, err := c.fetchChapterPage(contentID, acs)
	if err != nil || body == nil {
		return "", err
	}

	var cfg chapterJSONConfig
	if m := jsConfigRe.FindSubmatch(body); len(m) > 1 {
		_ = json.Unmarshal(m[1], &cfg)
	}

	if len(cfg.TextURLs) == 0 {
		pdfBody, err := c.fetchContentBody(contentID, acs)
		if err != nil || pdfBody == nil {
			return "", err
		}
		return extractPDFViewerText(pdfBody), nil
	}

	// 並列で各ページのテキストを抽出
	host := c.BaseHost()
	n := len(cfg.TextURLs)
	results := make([]string, n+1) // 1-indexed

	var wg sync.WaitGroup
	for page := 1; page <= n; page++ {
		textURL, ok := cfg.TextURLs[fmt.Sprintf("%d", page)]
		if !ok {
			continue
		}
		textURL = strings.ReplaceAll(textURL, "&amp;", "&")

		var fileParam string
		if fm := fileParamPattern.FindStringSubmatch(textURL); len(fm) >= 2 {
			fileParam = fm[1]
		}

		wg.Add(1)
		go func(p int, url, file string) {
			defer wg.Done()
			if file != "" {
				results[p] = c.extractPDFText(host, url)
			} else {
				fullURL := c.ResolveURL(url, host+"/")
				if pageBody, err := c.FetchBodyAbs(fullURL); err == nil {
					results[p] = extractPageText(pageBody)
				}
			}
		}(page, textURL, fileParam)
	}
	wg.Wait()

	var parts []string
	for _, s := range results {
		if s != "" {
			parts = append(parts, s)
		}
	}

	result := strings.Join(parts, "\n")
	return result, nil
}

// mergeFragmentedLines は文字単位で改行されたテキストを結合する。
// 短い断片は前行に結合し、適度にスペースを補う。
func mergeFragmentedLines(text string) string {
	rawLines := strings.Split(text, "\n")
	var out []string
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			out = append(out, "") // 空行は区切りとして残す
			continue
		}
		runes := []rune(line)
		// 3文字以下の断片は前行にスペース付きで結合
		if len(runes) <= 3 && len(out) > 0 && out[len(out)-1] != "" {
			out[len(out)-1] += line
		} else if len(out) > 0 && out[len(out)-1] != "" {
			// 前行との間にスペースを入れる（既に結合済みの断片を除く）
			out[len(out)-1] += " " + line
		} else {
			out = append(out, line)
		}
	}
	// 連続空行を1つに
	var cleaned []string
	prevEmpty := false
	for _, line := range out {
		if line == "" {
			if !prevEmpty {
				cleaned = append(cleaned, "")
				prevEmpty = true
			}
		} else {
			cleaned = append(cleaned, line)
			prevEmpty = false
		}
	}
	return strings.Join(cleaned, "\n")
}
