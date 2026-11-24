package webclass

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

// ContentInfo はコース内のコンテンツアイテム。
type ContentInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Category string `json:"category,omitempty"`
}

// MaterialCategory は本サービスが扱うコンテンツ種別（WebClass のカテゴリラベル）。
// 試験・レポート・アンケート・自習などは開くだけで「開始」扱いになったり提出画面だったりするので、
// 一覧にも出さず中身にもアクセスしない。資料の取得だけを主な機能にする。
const MaterialCategory = "資料"

// ErrNotMaterial は資料以外のコンテンツへのアクセスを拒否したことを示す。
var ErrNotMaterial = errors.New("資料以外のコンテンツ（試験・レポート・アンケートなど）は扱っていません")

// IsMaterial はコンテンツが資料かどうかを返す。
func (ci ContentInfo) IsMaterial() bool { return ci.Category == MaterialCategory }

// FileInfo はコンテンツページで見つかったダウンロード可能ファイル。
type FileInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// FetchCourseContents はコースページにアクセスして資料（MaterialCategory）のコンテンツだけを列挙する。
// 試験・レポート・アンケートなどは返さない。
func (c *Client) FetchCourseContents(courseID string) ([]ContentInfo, error) {
	resp, err := c.FollowJSRedirect(fmt.Sprintf("course.php/%s/login", courseID))
	if err != nil {
		return nil, fmt.Errorf("コース %s へのアクセス: %w", courseID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("コースページ読み取り: %w", err)
	}

	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("HTML パース: %w", err)
	}

	var materials []ContentInfo
	ids := make(map[string]bool)
	for _, ci := range parseContentSections(doc) {
		if ci.IsMaterial() {
			materials = append(materials, ci)
			ids[ci.ID] = true
		}
	}
	c.acsMu.Lock()
	c.materialCache[courseID] = ids
	if acs := extractACSToken(body); acs != "" {
		c.acsCache[courseID] = acs
	}
	c.acsMu.Unlock()
	return materials, nil
}

// requireMaterial は contentID がコースの資料であることを確かめる。
// 試験やアンケートを do_contents.php で開くと「開始」扱いになるため、中身を取得する前に必ず呼ぶ。
func (c *Client) requireMaterial(courseID, contentID string) error {
	c.acsMu.Lock()
	ids, ok := c.materialCache[courseID]
	c.acsMu.Unlock()
	if !ok {
		if _, err := c.FetchCourseContents(courseID); err != nil {
			return err
		}
		c.acsMu.Lock()
		ids = c.materialCache[courseID]
		c.acsMu.Unlock()
	}
	if !ids[contentID] {
		return ErrNotMaterial
	}
	return nil
}

// parseContentSections は DOM から data-contents-id を持つ <section> 要素を走査する。
func parseContentSections(doc *xhtml.Node) []ContentInfo {
	seen := make(map[string]bool)
	var contents []ContentInfo

	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "section" {
			attrs := webclient.AttrMap(n)
			id := attrs["data-contents-id"]
			if id != "" && !seen[id] {
				seen[id] = true
				// data-contents-name は &amp;lt;pdf&amp;gt; のように二重エンティティになっている
				name := html.UnescapeString(html.UnescapeString(attrs["data-contents-name"]))
				category := findCategoryLabel(n)
				contents = append(contents, ContentInfo{
					ID:       id,
					Name:     name,
					Category: category,
					URL:      fmt.Sprintf("do_contents.php?reset_status=1&set_contents_id=%s", id),
				})
				// content section の内部は再帰しない（入れ子 section はフォルダ）
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return contents
}

// findCategoryLabel は <section> 内の cl-contentsList_categoryLabel を探す。
func findCategoryLabel(section *xhtml.Node) string {
	var label string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if label != "" {
			return
		}
		if n.Type == xhtml.ElementNode && n.Data == "div" {
			if strings.Contains(webclient.AttrMap(n)["class"], "cl-contentsList_categoryLabel") {
				label = strings.TrimSpace(webclient.TextContent(n))
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(section)
	return label
}

// --- コンテンツファイル取得 ---
// フロー: do_contents.php → (show_frame.php → show_info.php 自動 POST →)
//         txtbk_frame.php → txtbk_show_chapter.php
// 目次 (TOC) の章・節タイトルをファイル名に使い、file_down.php の添付資料も列挙する。

var (
	// txtbk のファイル収集用フレーム skip 方針（本文・目次以外を除外）。
	chapterSkipFrameRe = regexp.MustCompile(`show_header\.php|show_foot|title_simple\.php|dqstn_`)
	tocRowRe           = regexp.MustCompile(`(?is)<TR[^>]*\bdata-page='(\d+)'[^>]*>(.*?)</TR>`)
	tocSpanRe          = regexp.MustCompile(`(?is)<SPAN[^>]*>(.*?)</SPAN>`)
	tocChapterNumRe    = regexp.MustCompile(`^第\d+[章節]$`)
	fileDownLinkRe     = regexp.MustCompile(`file_down\.php\?[^"'\s<>]+`)
	fileNameParamRe    = regexp.MustCompile(`file_name=([^&"']+)`)
	fileParamPattern   = regexp.MustCompile(`[?&]file=([^&"]+)`)
	contentsURLPattern = regexp.MustCompile(`contents_url=([^&"']+)`)
	jsConfigRe         = regexp.MustCompile(`(?s)<script[^>]+class="js-config"[^>]*>\s*(\{.*?\})\s*</script>`)
	htmlTagRe          = regexp.MustCompile(`(?s)<[^>]*>`)
)

type chapterJSONConfig struct {
	TextURL  string            `json:"text_url"`
	TextURLs map[string]string `json:"text_urls"`
}

// FetchContentFiles はコンテンツページをたどってダウンロード可能ファイルを列挙する。
// courseID はコースセッション確立（acs_ トークン取得）に必要。
// 資料以外は ErrNotMaterial を返す。資料でもファイルが無ければ (nil, nil) を返す。
func (c *Client) FetchContentFiles(courseID, contentID string) ([]FileInfo, error) {
	if err := c.requireMaterial(courseID, contentID); err != nil {
		return nil, err
	}
	acs := c.CourseACSToken(courseID)
	body, err := c.fetchChapterPage(contentID, acs)
	if err != nil || body == nil {
		return nil, err
	}
	return c.parseChapterFiles(body), nil
}

// fetchChapterPage は do_contents.php から txtbk_show_chapter.php まで
// リダイレクト・FRAMESET・show_info.php 開始確認を自動でたどる。
// 教科書タイプでない場合は (nil, nil) を返す。
func (c *Client) fetchChapterPage(contentID, acs string) ([]byte, error) {
	rawURL := c.BaseURL + "/do_contents.php?reset_status=1&set_contents_id=" + contentID
	if acs != "" {
		rawURL += "&acs_=" + acs
	}
	postedInfo := false

	for hop := 0; hop < 10; hop++ {
		body, err := c.FetchBodyAbs(rawURL)
		if err != nil {
			return nil, err
		}

		if strings.Contains(rawURL, "txtbk_show_chapter.php") {
			return body, nil
		}

		// FRAMESET → 目次ページに至るフレームへ
		if frames := c.FrameSrcs(body); frames != nil {
			next := pickChapterFrame(frames)
			if next == "" {
				return nil, nil
			}
			rawURL = c.ResolveURL(next, rawURL)
			continue
		}

		// show_info.php → 「開始」を自動 POST
		if strings.Contains(rawURL, "show_info.php") && !postedInfo {
			postedInfo = true
			respBody, _, finalURL, err := c.StartShowInfo(body, rawURL)
			if err != nil {
				return nil, nil
			}
			next := c.JSRedirect(respBody, finalURL)
			if next == "" {
				next = finalURL
			}
			rawURL = next
			continue
		}

		// JS リダイレクト
		if next := c.JSRedirect(body, rawURL); next != "" {
			if strings.Contains(next, "logout.php") {
				return nil, nil
			}
			rawURL = next
			continue
		}

		return nil, nil // 教科書タイプ以外
	}
	return nil, nil
}

// fetchContentBody は do_contents.php から教材本文のHTMLを取得する。
// txtbk_frame.php では目次ではなく本文フレームだけを追跡する。
func (c *Client) fetchContentBody(contentID, acs string) ([]byte, error) {
	rawURL := c.BaseURL + "/do_contents.php?reset_status=1&set_contents_id=" + contentID
	if acs != "" {
		rawURL += "&acs_=" + acs
	}
	postedInfo := false

	for hop := 0; hop < 12; hop++ {
		body, err := c.FetchBodyAbs(rawURL)
		if err != nil {
			return nil, err
		}

		// txtbk_frame.php → 本文フレームを選択（目次はスキップ）
		if strings.Contains(rawURL, "txtbk_frame.php") {
			if next := pickContentFrame(c.FrameSrcs(body), rawURL, c.ResolveURL); next != "" {
				rawURL = next
				continue
			}
			// フレームがない場合、直接 loadit.php iframe を探す
			if pdfURL := extractPDFViewerURL(body, rawURL, c.ResolveURL); pdfURL != "" {
				pdfBody, err := c.FetchBodyAbs(pdfURL)
				if err != nil {
					return nil, err
				}
				return pdfBody, nil
			}
			return nil, nil
		}

		// フレーム内のページで loadit.php iframe をチェック
		if pdfURL := extractPDFViewerURL(body, rawURL, c.ResolveURL); pdfURL != "" {
			pdfBody, err := c.FetchBodyAbs(pdfURL)
			if err != nil {
				return nil, err
			}
			return pdfBody, nil
		}

		// FRAMESET
		if frames := c.FrameSrcs(body); frames != nil {
			next := pickChapterFrame(frames)
			if next == "" {
				return nil, nil
			}
			rawURL = c.ResolveURL(next, rawURL)
			continue
		}

		// show_info.php → 「開始」POST
		if strings.Contains(rawURL, "show_info.php") && !postedInfo {
			postedInfo = true
			respBody, _, finalURL, err := c.StartShowInfo(body, rawURL)
			if err != nil {
				return nil, nil
			}
			next := c.JSRedirect(respBody, finalURL)
			if next == "" {
				next = finalURL
			}
			rawURL = next
			continue
		}

		// JS リダイレクト
		if next := c.JSRedirect(body, rawURL); next != "" {
			if strings.Contains(next, "logout.php") {
				return nil, nil
			}
			rawURL = next
			continue
		}

		return nil, nil
	}
	return nil, nil
}

// pickContentFrame は txtbk_frame.php の FRAMESET から目次以外の本文フレームを選ぶ。
func pickContentFrame(frames []string, base string, resolve func(string, string) string) string {
	for _, src := range frames {
		src = strings.ReplaceAll(src, "&amp;", "&")
		// 目次フレームはスキップ、それ以外を本文とみなす
		if !strings.Contains(src, "txtbk_show_chapter") &&
			!strings.Contains(src, "show_header") &&
			!strings.Contains(src, "show_foot") &&
			!strings.Contains(src, "title_simple") {
			return resolve(src, base)
		}
	}
	return ""
}

// pdfViewerIframeRe は txtbk_frame.php 内の PDF ビューア iframe を検出する。
var pdfViewerIframeRe = regexp.MustCompile(`<iframe[^>]+src=["']([^"']*loadit\.php\?action=pdfViewer[^"']*)["']`)

// extractPDFViewerURL は txtbk_frame.php の HTML から PDF ビューアの URL を抽出する。
func extractPDFViewerURL(body []byte, base string, resolve func(string, string) string) string {
	m := pdfViewerIframeRe.FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return resolve(strings.ReplaceAll(string(m[1]), "&amp;", "&"), base)
}

// pickChapterFrame は FRAMESET のフレーム一覧から目次ページに至るものを選ぶ。
func pickChapterFrame(frames []string) string {
	for _, pat := range []string{"txtbk_show_chapter.php", "show_info.php"} {
		for _, f := range frames {
			if strings.Contains(f, pat) {
				return f
			}
		}
	}
	for _, f := range frames {
		if !chapterSkipFrameRe.MatchString(f) {
			return f
		}
	}
	return ""
}

// resolveFileDownloadURL は file_down.php 等のリンクが HTML ダウンロードページを返した場合、
// そのページ内から実ファイルの URL を抽出して返す。HTML でなければ元の URL を返す。
func (c *Client) resolveFileDownloadURL(fileURL string) string {
	resp, err := c.FetchURL(fileURL)
	if err != nil {
		return fileURL
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "text/html") {
		return fileURL
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fileURL
	}

	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return fileURL
	}

	var realURL string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if realURL != "" {
			return
		}
		if n.Type == xhtml.ElementNode && n.Data == "a" {
			attrs := webclient.AttrMap(n)
			href := attrs["href"]
			if strings.Contains(href, "download.php") {
				realURL = c.ResolveURL(strings.ReplaceAll(href, "&amp;", "&"), fileURL)
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if realURL != "" {
		return realURL
	}
	return fileURL
}

// parseChapterFiles は txtbk_show_chapter.php の HTML から
// 目次タイトル付きのファイル一覧を組み立てる。
func (c *Client) parseChapterFiles(body []byte) []FileInfo {
	host := c.BaseHost()

	// ページ番号 → text_url の JSON config
	var cfg chapterJSONConfig
	if m := jsConfigRe.FindSubmatch(body); len(m) > 1 {
		_ = json.Unmarshal(m[1], &cfg)
	}

	// 教材ファイルの基底 URL (/webclass/data/course/...)
	contentsBaseURL := ""
	if m := contentsURLPattern.FindSubmatch(body); len(m) > 1 {
		if u, err := url.QueryUnescape(string(m[1])); err == nil {
			contentsBaseURL = u
		}
	}

	var files []FileInfo
	seen := make(map[string]bool)
	// addedFileParams は追加済みの file= 値を記録し、
	// text_url と file_down.php、および file_down.php 同士の重複を防ぐ。
	addedFileParams := make(map[string]bool)

	// recordFileParam は file= 値を正規化（URLデコード）して記録する。
	recordFileParam := func(raw string) {
		decoded, err := url.QueryUnescape(raw)
		if err != nil {
			decoded = raw
		}
		addedFileParams[decoded] = true
	}

	// isFileParamAdded は file= 値が既に追加済みかを返す。
	isFileParamAdded := func(raw string) bool {
		decoded, err := url.QueryUnescape(raw)
		if err != nil {
			decoded = raw
		}
		return addedFileParams[decoded]
	}

	addFile := func(name, fileURL string) {
		if fileURL == "" || seen[fileURL] {
			return
		}
		seen[fileURL] = true
		files = append(files, FileInfo{Name: name, URL: fileURL})
	}

	// 本文ページの教材ファイル（text_url の file= パラメータ）。
	// 追加に成功したら true を返す。
	addTextFile := func(page, title string) bool {
		textURL, ok := cfg.TextURLs[page]
		if !ok {
			return false
		}
		fm := fileParamPattern.FindStringSubmatch(textURL)
		if len(fm) < 2 || fm[1] == "" {
			return false
		}
		filePath, err := url.QueryUnescape(fm[1])
		if err != nil || filePath == "" {
			return false
		}
		recordFileParam(fm[1])

		name := title
		if name == "" {
			name = filePath[strings.LastIndex(filePath, "/")+1:]
		} else if idx := strings.LastIndex(filePath, "."); idx >= 0 {
			name += filePath[idx:] // タイトル + 拡張子
		}
		addFile(name, host+contentsBaseURL+filePath)
		return true
	}

	rows := tocRowRe.FindAllSubmatch(body, -1)
	for _, row := range rows {
		page := string(row[1])
		rowHTML := string(row[2])
		title := tocRowTitle(rowHTML)

		hasText := addTextFile(page, title)

		// 本文教材がある場合、同じセクションの添付は重複コンテンツなのでスキップ。
		// 本文教材がないセクション（動画のみ等）では添付をそのまま収集する。
		if !hasText {
			for _, link := range fileDownLinkRe.FindAllString(rowHTML, -1) {
				link = strings.ReplaceAll(link, "&amp;", "&")

				if fm := fileParamPattern.FindStringSubmatch(link); len(fm) >= 2 {
					if isFileParamAdded(fm[1]) {
						continue
					}
					recordFileParam(fm[1])
				}

				name := ""
				if nm := fileNameParamRe.FindStringSubmatch(link); len(nm) > 1 {
					name, _ = url.QueryUnescape(nm[1])
				}
				if name == "" {
					name = "添付資料"
				}
				fileURL := c.BaseURL + "/" + link
				fileURL = c.resolveFileDownloadURL(fileURL)
				addFile(name, fileURL)
			}
		}
	}

	// TOC が無い場合のフォールバック: JSON の text_urls を直接なめる
	if len(rows) == 0 {
		for page := 1; page <= len(cfg.TextURLs); page++ {
			addTextFile(fmt.Sprintf("%d", page), "")
		}
	}

	return files
}

// tocRowTitle は TOC 行 HTML から章・節タイトルを取り出す。
// 「第N章」「第N節」のような番号ラベルはスキップする。
func tocRowTitle(rowHTML string) string {
	for _, m := range tocSpanRe.FindAllStringSubmatch(rowHTML, -1) {
		text := htmlTagRe.ReplaceAllString(m[1], "")
		text = strings.TrimSpace(html.UnescapeString(text))
		if text == "" || tocChapterNumRe.MatchString(text) {
			continue
		}
		return text
	}
	return ""
}

// extractPageText は txtbk_show_text.php の HTML から本文テキストを抽出する。
