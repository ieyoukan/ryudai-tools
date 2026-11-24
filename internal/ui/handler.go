package ui

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

const sessionCookie = "ryudai_tools_session"

type Handler struct {
	cfg           *config.Config
	store         session.Store
	db            *db.DB
	academic      *academic.Service
	webClassLocks sync.Map // username -> *sync.Mutex
}

func NewHandler(cfg *config.Config, store session.Store, database *db.DB, svc *academic.Service) *Handler {
	if svc == nil {
		svc = academic.New(cfg.KyomuBaseURL, nil)
	}
	return &Handler{cfg: cfg, store: store, db: database, academic: svc}
}

func (h *Handler) sessionFromCookie(r *http.Request) *session.Entry {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	entry, ok := h.store.Get(c.Value)
	if !ok {
		return nil
	}
	h.renewWebClassCookie(entry)
	return entry
}

// renewWebClassCookie は WCAC の期限が近いとき、.env の本人に限り
// 再ログインして Cookie を更新する。WebClass の keep-alive は Cookie を
// 再発行しないため、この方式でのみ7日間の期限を更新できる。
func (h *Handler) renewWebClassCookie(entry *session.Entry) {
	mu := h.webClassLock(entry.Username)
	mu.Lock()
	defer mu.Unlock()
	current, ok := h.store.Get(entry.SessionID)
	if !ok {
		return
	}
	entry.Cookies = current.Cookies
	expiresAt, hasExpiry := session.WebClassCookieExpiresAt(entry.Cookies)
	if !hasExpiry || time.Until(expiresAt) > session.WebClassRenewBefore {
		return
	}
	if _, ok := h.envPassword(entry.Username); !ok {
		return
	}
	if err := h.loginWebClass(entry); err != nil {
		log.Printf("WebClass Cookie 更新失敗: %v", err)
	}
}

// errNoPassword は .env の本人以外で、再ログインできないことを示す（公開サーバーはパスワードを保存しない）。
var errNoPassword = errors.New("WebClass のセッションが切れました。ログアウトして再ログインしてください")

// envPassword は .env の WEBCLASS_USERNAME 本人のパスワードを返す。他のユーザーは false。
// 自分でデプロイして .env に自分のアカウントを書いた人だけが自動再ログインを使える。
func (h *Handler) envPassword(username string) (string, bool) {
	if h.cfg.WebClassUsername == "" || h.cfg.WebClassPassword == "" || !strings.EqualFold(username, h.cfg.WebClassUsername) {
		return "", false
	}
	return h.cfg.WebClassPassword, true
}

func (h *Handler) webClassLock(username string) *sync.Mutex {
	mu, _ := h.webClassLocks.LoadOrStore(username, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// loginWebClass は .env の本人のときだけ、そのパスワードで WebClass に再ログインする。
func (h *Handler) loginWebClass(entry *session.Entry) error {
	password, ok := h.envPassword(entry.Username)
	if !ok {
		return errNoPassword
	}
	client, err := webclass.New(h.cfg.WebClassBaseURL)
	if err != nil {
		return err
	}
	client.HTTPClient.Timeout = 30 * time.Second
	cookies, err := client.Login(h.cfg.WebClassUsername, password)
	if err != nil {
		return fmt.Errorf("WebClass の再ログインに失敗しました: %w", err)
	}
	if !h.store.UpdateCookies(entry.SessionID, cookies) {
		return fmt.Errorf("ログインセッションが無効になりました")
	}
	entry.Cookies = cookies
	return nil
}

// WebClassClient recovers a rejected upstream session independently of cookie expiry.
func (h *Handler) WebClassClient(entry *session.Entry) (*webclass.Client, error) {
	client, err := webclass.NewFromCookies(h.cfg.WebClassBaseURL, entry.Cookies)
	if err != nil {
		return nil, err
	}
	failedCookies := entry.Cookies
	client.SetSessionRecovery(func() ([]*http.Cookie, error) {
		mu := h.webClassLock(entry.Username)
		mu.Lock()
		defer mu.Unlock()
		current, ok := h.store.Get(entry.SessionID)
		if !ok {
			return nil, fmt.Errorf("ログインセッションが無効になりました")
		}
		// Another request may already have recovered this session.
		if sameCookieValues(current.Cookies, failedCookies) {
			if err := h.loginWebClass(current); err != nil {
				return nil, err
			}
		}
		return current.Cookies, nil
	})
	return client, nil
}

func sameCookieValues(a, b []*http.Cookie) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

func cookieExpiryLabel(cookies []*http.Cookie) string {
	if expiresAt, ok := session.WebClassCookieExpiresAt(cookies); ok {
		return expiresAt.Format(time.RFC3339)
	}
	return "不明"
}

// ShowLogin は GET /login を処理する。
func (h *Handler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"))
	if h.sessionFromCookie(r) != nil {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	h.render(w, "login", LoginData{Next: next, ForOAuth: strings.HasPrefix(next, "/oauth/")})
}

// safeNext はログイン後の戻り先として自サイト内のパスだけを許す（オープンリダイレクト防止）。
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/courses"
	}
	return next
}

// CurrentUser はブラウザのログインセッションのユーザー名を返す（未ログインなら ""）。OAuth の認可画面で使う。
func (h *Handler) CurrentUser(r *http.Request) string {
	if entry := h.sessionFromCookie(r); entry != nil {
		return entry.Username
	}
	return ""
}

// HandleLogin は POST /login を処理する。
// WebClass の ID・パスワードでログインする。パスワードは保存せず、各システムの Cookie だけを保存する。
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	wcPassword := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	fail := func(msg string) {
		h.render(w, "login", LoginData{Error: msg, Next: next, ForOAuth: strings.HasPrefix(next, "/oauth/")})
	}

	// 大学のメールアドレスは @ より前を ID にし、それ以外のメールアドレスは大学のシステムへ送る前に止める。
	wcUsername, err := webclass.NormalizeUsername(r.FormValue("username"))
	if err != nil {
		fail(err.Error())
		return
	}
	if wcUsername == "" || wcPassword == "" {
		fail("ユーザー名とパスワードを入力してください")
		return
	}

	// WebClass にログイン
	client, err := webclass.New(h.cfg.WebClassBaseURL)
	if err != nil {
		fail("クライアント作成失敗")
		return
	}
	cookies, err := client.Login(wcUsername, wcPassword)
	if err != nil {
		fail("ユーザー名またはパスワードが違います")
		return
	}

	// 教務システムにも同じパスワードでログインし、Cookie だけを保存する。
	// 失敗しても WebClass の機能は使えるので、ログイン自体は続ける。
	if err := h.academic.StartSession(wcUsername, wcPassword); err != nil {
		log.Printf("教務システムへのログイン失敗 (%s): %v", wcUsername, err)
	}

	sessionID := h.store.Create(wcUsername, cookies)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(session.IdleTimeout.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// ShowRegister はログインページにリダイレクトする（登録はログインと統合済み）。
func (h *Handler) ShowRegister(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// HandleRegister はログインページにリダイレクトする。
func (h *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// HandleLogout は POST /logout を処理する。
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if entry, ok := h.store.Get(c.Value); ok {
			h.academic.Forget(entry.Username)
		}
		h.store.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ShowCourses は GET /courses を処理する。
func (h *Handler) ShowCourses(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	base := BaseData{Username: entry.Username}

	client, err := h.WebClassClient(entry)
	if err != nil {
		h.render(w, "courses", CoursesData{BaseData: base, Error: "クライアント作成に失敗しました"})
		return
	}

	courses, err := client.FetchTimetable()
	if err != nil {
		h.render(w, "courses", CoursesData{BaseData: base, Error: "コース一覧の取得に失敗しました: " + err.Error()})
		return
	}

	h.render(w, "courses", CoursesData{
		BaseData: base,
		Days:     timetableDays,
		Rows:     buildTimetableRows(courses),
	})
}

// ShowContents は GET /courses/{courseID} を処理する。
func (h *Handler) ShowContents(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	courseID := chi.URLParam(r, "courseID")
	courseName := r.URL.Query().Get("name")
	if courseName == "" {
		courseName = courseID
	}

	base := BaseData{Username: entry.Username}

	client, err := h.WebClassClient(entry)
	if err != nil {
		h.render(w, "contents", ContentsData{BaseData: base, CourseName: courseName, Error: "クライアント作成に失敗しました"})
		return
	}

	contents, err := client.FetchCourseContents(courseID)
	if err != nil {
		h.render(w, "contents", ContentsData{BaseData: base, CourseName: courseName, Error: "コンテンツ一覧の取得に失敗しました: " + err.Error()})
		return
	}

	// contents は資料だけ（試験・レポート・アンケートは provider 側で除外済み）。
	// ファイルはページ描画をブロックしないよう、遷移後に /files.json から
	// 非同期・逐次で取得する（下の CourseFilesJSON）。ここでは一覧のみ用意する。
	data := ContentsData{
		BaseData:    base,
		CourseID:    courseID,
		CourseName:  courseName,
		Contents:    contents,
		WebClassURL: webClassCourseURL(courseID),
	}

	now := time.Now()
	attItems, err := client.FetchAttendanceList(courseID)
	if err != nil {
		data.AttendanceError = err.Error()
	} else {
		rows := make([]AttendanceRow, len(attItems))
		for i, it := range attItems {
			rows[i] = AttendanceRow{Name: it.Name, Status: it.Status, WindowOpen: it.WindowOpen, WindowClose: it.WindowClose, IsOpen: it.IsOpenAt(now)}
		}
		data.Attendance = rows
	}

	h.render(w, "contents", data)
}

// ShowFiles は GET /courses/{courseID}/contents/{contentID} を処理する。
func (h *Handler) ShowFiles(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	courseID := chi.URLParam(r, "courseID")
	contentID := chi.URLParam(r, "contentID")
	contentName := r.URL.Query().Get("name")
	if contentName == "" {
		contentName = contentID
	}
	courseName := r.URL.Query().Get("course")

	base := BaseData{Username: entry.Username}

	client, err := h.WebClassClient(entry)
	if err != nil {
		h.render(w, "files", FilesData{BaseData: base, ContentName: contentName, Error: "クライアント作成に失敗しました"})
		return
	}

	data := FilesData{
		BaseData:    base,
		CourseID:    courseID,
		CourseName:  courseName,
		ContentID:   contentID,
		ContentName: contentName,
		WebClassURL: webClassCourseURL(courseID),
	}
	files, err := client.FetchContentFiles(courseID, contentID)
	if errors.Is(err, webclass.ErrNotMaterial) {
		data.Error = err.Error() + "。WebClass で直接開いてください。"
		h.render(w, "files", data)
		return
	}
	if err != nil {
		data.Error = "ファイル一覧の取得に失敗しました: " + err.Error()
		h.render(w, "files", data)
		return
	}
	data.Files = files
	h.render(w, "files", data)
}

const wcHost = "https://webclass.cc.u-ryukyu.ac.jp"
const wcBase = wcHost + "/webclass/"

// webClassCourseURL は WebClass のコースページの URL。資料以外（試験・レポートなど）はここから直接開いてもらう。
func webClassCourseURL(courseID string) string {
	return wcBase + "course.php/" + url.PathEscape(courseID) + "/login"
}

// isWebClassURL は rawURL が WebClass のホストを指す https URL かどうかを返す。
func isWebClassURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	base, _ := url.Parse(wcHost)
	return u.Scheme == base.Scheme && u.Host == base.Host && u.User == nil
}

// Download は GET /download?url=... を処理する。WebClass のファイルをプロキシする。
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "url パラメータが必要です", http.StatusBadRequest)
		return
	}
	// SSRF 防止: WebClass の URL のみ許可（前方一致だと webclass.cc.u-ryukyu.ac.jp.example.com も通るのでホストで比べる）
	if !isWebClassURL(rawURL) {
		http.Error(w, "不正な URL です", http.StatusBadRequest)
		return
	}

	client, err := h.WebClassClient(entry)
	if err != nil {
		http.Error(w, "クライアント作成に失敗しました", http.StatusInternalServerError)
		return
	}

	resp, err := client.FetchURL(rawURL)
	if err != nil {
		http.Error(w, "ファイルのダウンロードに失敗しました", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	// HTML は資料ファイルではない（試験・提出画面やログイン画面など）。
	// このサーバーのオリジンで WebClass の HTML を表示するとスクリプトも動くので、返さない。
	if strings.Contains(strings.ToLower(ct), "text/html") {
		w.Header().Del("Content-Type")
		w.Header().Del("Content-Length")
		http.Error(w, "この URL は資料ファイルではありません。WebClass で直接開いてください。", http.StatusUnsupportedMediaType)
		return
	}

	// ファイル名の優先順位: name クエリ > WebClass の Content-Disposition > URL のパス末尾
	if name := r.URL.Query().Get("name"); name != "" {
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(name)))
	} else if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	} else if u, err := url.Parse(rawURL); err == nil {
		if name := path.Base(u.Path); name != "" && name != "." {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		}
	}

	io.Copy(w, resp.Body)
}

// DownloadCourseZip は GET /courses/{courseID}/files.zip を処理する。
// コース内の全ファイルを取得して 1 つの ZIP にまとめて返す。
func (h *Handler) DownloadCourseZip(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	courseID := chi.URLParam(r, "courseID")
	courseName := r.URL.Query().Get("name")
	if courseName == "" {
		courseName = courseID
	}

	client, err := h.WebClassClient(entry)
	if err != nil {
		http.Error(w, "クライアント作成に失敗しました", http.StatusInternalServerError)
		return
	}

	contents, err := client.FetchCourseContents(courseID)
	if err != nil {
		http.Error(w, "コンテンツ一覧の取得に失敗しました: "+err.Error(), http.StatusBadGateway)
		return
	}

	fileMap := fetchCourseFilesSeq(client, courseID, contents)

	// ファイルが1件もなければエラー
	totalFiles := 0
	for _, files := range fileMap {
		totalFiles += len(files)
	}
	if totalFiles == 0 {
		http.Error(w, "ダウンロード可能なファイルがありません", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename*=UTF-8''%s.zip`, url.PathEscape(courseName)))

	zw := zip.NewWriter(w)
	defer zw.Close()
	usedNames := make(map[string]bool)
	for _, c := range contents {
		files := fileMap[c.ID]
		if len(files) == 0 {
			continue
		}
		// コンテンツ名をサブディレクトリにして同名衝突を回避
		dirName := sanitizeZipName(c.Name)
		if dirName == "" {
			dirName = c.ID
		}
		for _, f := range files {
			resp, err := client.FetchURL(f.URL)
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
				resp.Body.Close()
				continue
			}
			name := uniqueZipName(dirName+"/"+f.Name, usedNames)
			fw, err := zw.Create(name)
			if err != nil {
				resp.Body.Close()
				continue
			}
			io.Copy(fw, resp.Body)
			resp.Body.Close()
		}
	}
}

// fetchCourseFilesSeq はコース内全コンテンツのファイルを逐次取得する。
// do_contents.php?reset_status=1 はサーバー側の「現在のコンテンツ」状態を変えるため、
// 並列化すると show_info 経由の多段フロー（ミーティング等）が互いに壊れる。
// そのため逐次で取得して確実に全ファイルを集める。
func fetchCourseFilesSeq(client *webclass.Client, courseID string, contents []webclass.ContentInfo) map[string][]webclass.FileInfo {
	out := make(map[string][]webclass.FileInfo, len(contents))
	for _, c := range contents {
		if files, _ := client.FetchContentFiles(courseID, c.ID); len(files) > 0 {
			out[c.ID] = files
		}
	}
	return out
}

// CourseFilesJSON は GET /courses/{courseID}/files.json を処理する。
// ページ遷移後にフロントから呼ばれ、全コンテンツのファイルを逐次取得して返す。
func (h *Handler) CourseFilesJSON(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	courseID := chi.URLParam(r, "courseID")
	client, err := h.WebClassClient(entry)
	if err != nil {
		http.Error(w, "クライアント作成に失敗しました", http.StatusInternalServerError)
		return
	}

	contents, err := client.FetchCourseContents(courseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	fileMap := fetchCourseFilesSeq(client, courseID, contents)

	type fileJSON struct {
		Name        string `json:"name"`
		DownloadURL string `json:"downloadUrl"`
	}
	type contentFilesJSON struct {
		ContentID string     `json:"contentId"`
		Files     []fileJSON `json:"files"`
	}

	out := make([]contentFilesJSON, 0, len(fileMap))
	for _, c := range contents {
		files := fileMap[c.ID]
		if len(files) == 0 {
			continue
		}
		fj := make([]fileJSON, len(files))
		for i, f := range files {
			fj[i] = fileJSON{
				Name: f.Name,
				DownloadURL: fmt.Sprintf("/download?url=%s&name=%s",
					url.QueryEscape(f.URL), url.QueryEscape(f.Name)),
			}
		}
		out = append(out, contentFilesJSON{ContentID: c.ID, Files: fj})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

// CourseSummary は GET /courses/{courseID}/summary で AI 用の構造化 JSON を表示する。
func (h *Handler) CourseSummary(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	courseID := chi.URLParam(r, "courseID")
	courseName := r.URL.Query().Get("name")
	if courseName == "" {
		courseName = courseID
	}

	client, err := h.WebClassClient(entry)
	if err != nil {
		http.Error(w, "クライアント作成に失敗しました", http.StatusInternalServerError)
		return
	}

	contents, err := client.FetchCourseContents(courseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	fileMap := fetchCourseFilesSeq(client, courseID, contents)

	type summaryFile struct {
		Name string `json:"name"`
	}
	type summaryContent struct {
		Name  string        `json:"name"`
		Files []summaryFile `json:"files"`
	}

	var items []summaryContent
	for _, c := range contents {
		files := fileMap[c.ID]
		if len(files) == 0 {
			continue
		}
		sf := make([]summaryFile, len(files))
		for i, f := range files {
			sf[i] = summaryFile{Name: f.Name}
		}
		items = append(items, summaryContent{Name: c.Name, Files: sf})
	}

	summary := map[string]any{
		"course":   courseName,
		"contents": items,
	}
	jsonBytes, _ := json.MarshalIndent(summary, "", "  ")
	jsonStr := string(jsonBytes)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="ja">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s - AI要約用JSON - Ryudai Tools</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#1e1e2e;color:#cdd6f4;padding:1.5rem}
.toolbar{display:flex;align-items:center;gap:1rem;margin-bottom:1rem;flex-wrap:wrap}
h1{font-size:1.1rem;color:#cdd6f4}
.copy-btn{padding:.4rem 1.2rem;background:#7c3aed;color:#fff;border:none;border-radius:5px;cursor:pointer;font-size:.85rem;white-space:nowrap}
.copy-btn:hover{opacity:.85}
.copy-btn.copied{background:#16a34a}
.hint{font-size:.8rem;color:#a6adc8}
pre{background:#181825;border:1px solid #313244;border-radius:6px;padding:1rem;overflow:auto;font-size:.82rem;line-height:1.5;white-space:pre-wrap;word-break:break-all}
</style>
</head>
<body>
<div class="toolbar">
<h1>%s</h1>
<button class="copy-btn" onclick="copyJSON()">コピー</button>
<span class="hint">このJSONをAIに貼り付けて要約・質問ができます</span>
</div>
<pre id="json">%s</pre>
<script>
function copyJSON(){
  var btn=document.querySelector(".copy-btn");
  navigator.clipboard.writeText(document.getElementById("json").textContent).then(function(){
    btn.textContent="コピーしました";
    btn.classList.add("copied");
    setTimeout(function(){btn.textContent="コピー";btn.classList.remove("copied")},2000);
  });
}
</script>
</body>
</html>`, template.HTMLEscapeString(courseName), template.HTMLEscapeString(courseName), template.HTMLEscapeString(jsonStr))
}

// uniqueZipName は ZIP 内で重複しないファイル名を返す（重複時は連番を付ける）。
func uniqueZipName(name string, used map[string]bool) string {
	if name == "" {
		name = "file"
	}
	if !used[name] {
		used[name] = true
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

// sanitizeZipName はZIPエントリ名として使えるよう危険な文字を置換する。
func sanitizeZipName(name string) string {
	repl := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	name = repl.Replace(name)
	name = strings.TrimSpace(name)
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

// --- ページのデータ構造（JSON で画面に渡す） ---

// BaseData は全ページ共通のフィールド（ログイン中のユーザー名。未ログインなら空）。
type BaseData struct {
	Username string
}

type LoginData struct {
	BaseData `tstype:",extends"`
	Error    string
	Next     string // ログイン後の戻り先
	ForOAuth bool   // ChatGPT などとの連携のためのログインか
}

type TimetableRow struct {
	Period int
	Cells  [][]*webclass.CourseInfo // 1セルに複数コースが入る場合がある
}

type CoursesData struct {
	BaseData `tstype:",extends"`
	Days     []string
	Rows     []TimetableRow
	Error    string
}

var timetableDays = []string{"月", "火", "水", "木", "金"}

// buildTimetableRows はコース一覧から時間割グリッドの行を構築する。
func buildTimetableRows(courses []webclass.CourseInfo) []TimetableRow {
	maxPeriod := 5
	grid := make(map[string]map[int][]*webclass.CourseInfo)
	for _, d := range timetableDays {
		grid[d] = make(map[int][]*webclass.CourseInfo)
	}
	for i := range courses {
		c := &courses[i]
		if c.Period > maxPeriod {
			maxPeriod = c.Period
		}
		if _, ok := grid[c.Day]; ok {
			grid[c.Day][c.Period] = append(grid[c.Day][c.Period], c)
		}
	}
	rows := make([]TimetableRow, maxPeriod)
	for p := 1; p <= maxPeriod; p++ {
		cells := make([][]*webclass.CourseInfo, len(timetableDays))
		for d, day := range timetableDays {
			cells[d] = grid[day][p] // nil なら空スライス扱いになる
		}
		rows[p-1] = TimetableRow{Period: p, Cells: cells}
	}
	return rows
}

// AttendanceRow は出席確認 1 件（状態と出席扱いの時間帯）。
type AttendanceRow struct {
	Name        string
	Status      string // 出席 / 遅刻 / 欠席 / 空（未出席）
	WindowOpen  time.Time
	WindowClose time.Time // 0 値は無制限
	IsOpen      bool
}

type ContentsData struct {
	BaseData        `tstype:",extends"`
	CourseID        string
	CourseName      string
	Contents        []webclass.ContentInfo
	WebClassURL     string
	Attendance      []AttendanceRow
	AttendanceError string
	Error           string
}

type FilesData struct {
	BaseData    `tstype:",extends"`
	CourseID    string
	CourseName  string
	ContentID   string
	ContentName string
	Files       []webclass.FileInfo
	WebClassURL string
	Error       string
}

type MessagesData struct {
	BaseData  `tstype:",extends"`
	Messages  []webclass.Message
	UnreadCnt int
	Error     string
}

type SettingsData struct {
	BaseData      `tstype:",extends"`
	SettingsError string
	APITokens     []db.APIToken
	OAuthGrants   []db.OAuthGrant
	NewToken      string // 発行直後のみ平文を表示する
	MCPURL        string
}

// ShowMessages は GET /messages で受信箱メッセージを表示する。
// 「レポートを受け取りました」は既定で除外する。
func (h *Handler) ShowMessages(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data := MessagesData{
		BaseData: BaseData{Username: entry.Username},
	}

	client, err := h.WebClassClient(entry)
	if err != nil {
		data.Error = "クライアント作成に失敗しました"
		h.render(w, "messages", data)
		return
	}

	msgs, err := client.FetchInboxMessages(entry.Username) // 自分のIDを除外
	if err != nil {
		data.Error = "メッセージ取得に失敗しました: " + err.Error()
		h.render(w, "messages", data)
		return
	}

	data.Messages = msgs

	// 未読数を API でも取得（notCourse=true でコース通知を除外）
	if cnt, err := client.FetchUnreadMessageCount(true); err == nil {
		data.UnreadCnt = cnt
	}

	h.render(w, "messages", data)
}

// ShowSettings は GET /settings を処理する。
func (h *Handler) ShowSettings(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.renderSettings(w, r, entry.Username, "")
}

// renderSettings は設定画面を描画する。newToken は API トークン発行直後のみ渡す。
func (h *Handler) renderSettings(w http.ResponseWriter, r *http.Request, username, newToken string) {
	if h.db == nil {
		http.Error(w, "設定データベースを利用できません", http.StatusServiceUnavailable)
		return
	}
	data := SettingsData{
		BaseData: BaseData{Username: username},
		NewToken: newToken,
		MCPURL:   h.mcpEndpoint(r),
	}
	switch r.URL.Query().Get("error") {
	case "save_failed":
		data.SettingsError = "設定を保存できませんでした。もう一度お試しください。"
	case "invalid_setting":
		data.SettingsError = "設定値が不正です。"
	}

	data.APITokens, _ = h.db.ListAPITokens(username)
	data.OAuthGrants, _ = h.db.ListOAuthGrants(username)

	h.render(w, "settings", data)
}

// ShowTerms は GET /terms（利用規約）を処理する。ログインしていなくても見られる。
func (h *Handler) ShowTerms(w http.ResponseWriter, r *http.Request) {
	h.render(w, "terms", h.policyData(r))
}

// ShowPrivacy は GET /privacy（プライバシーポリシー）を処理する。ログインしていなくても見られる。
func (h *Handler) ShowPrivacy(w http.ResponseWriter, r *http.Request) {
	h.render(w, "privacy", h.policyData(r))
}

func (h *Handler) policyData(r *http.Request) BaseData {
	if entry := h.sessionFromCookie(r); entry != nil {
		return BaseData{Username: entry.Username}
	}
	return BaseData{}
}
