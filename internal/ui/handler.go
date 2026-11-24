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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/lms"
	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

const sessionCookie = "ryudai_tools_session"

type Handler struct {
	cfg           *config.Config
	store         session.Store
	db            *db.DB
	academic      *academic.Service
	// lms は WebClass のサービス層。利用者ごとの順番待ち・再ログイン・時間の上限・エラーの文言を受け持つ。
	lms *lms.Service
}

func NewHandler(cfg *config.Config, store session.Store, database *db.DB, svc *academic.Service) *Handler {
	if svc == nil {
		svc = academic.New(cfg.KyomuBaseURL, nil)
	}
	h := &Handler{cfg: cfg, store: store, db: database, academic: svc}
	h.lms = lms.New(cfg.WebClassBaseURL, store, h.envPassword)
	return h
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
	h.lms.RenewIfExpiring(r.Context(), entry)
	return entry
}

// envPassword は .env の WEBCLASS_USERNAME 本人のパスワードを返す。他のユーザーは false。
// 自分でデプロイして .env に自分のアカウントを書いた人だけが自動再ログインを使える。
func (h *Handler) envPassword(username string) (string, bool) {
	if h.cfg.WebClassUsername == "" || h.cfg.WebClassPassword == "" || !strings.EqualFold(username, h.cfg.WebClassUsername) {
		return "", false
	}
	return h.cfg.WebClassPassword, true
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
	next := safeNext(r.FormValue("next"))
	fail := func(msg string) {
		h.render(w, "login", LoginData{Error: msg, Next: next, ForOAuth: strings.HasPrefix(next, "/oauth/")})
	}

	// 大学のメールアドレスは @ より前を ID にし、それ以外のメールアドレスは大学のシステムへ送る前に止める。
	password := r.FormValue("password")
	username, cookies, err := h.lms.Login(r.Context(), r.FormValue("username"), password)
	switch {
	case errors.Is(err, webclass.ErrEmailAsUsername):
		fail(err.Error())
		return
	case err != nil && (r.FormValue("username") == "" || password == ""):
		fail("ユーザー名とパスワードを入力してください")
		return
	case err != nil:
		fail("ユーザー名またはパスワードが違います")
		return
	}

	// 教務システムにも同じパスワードでログインし、Cookie だけを保存する。
	// 失敗しても WebClass の機能は使えるので、ログイン自体は続ける。
	if err := h.academic.StartSession(username, password); err != nil {
		log.Printf("教務システムへのログイン失敗 (%s): %v", username, err)
	}

	sessionID := h.store.Create(username, cookies)
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
	courses, err := h.lms.Courses(r.Context(), entry)
	if err != nil {
		h.render(w, "courses", CoursesData{BaseData: base, Error: lms.Describe("コース一覧", err)})
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

	// 資料は試験・レポート・アンケートを除いたもの。ファイルはページ描画をブロックしないよう、
	// 遷移後に /files.json から取得する（CourseFilesJSON）。
	cc, err := h.lms.CourseContents(r.Context(), entry, courseID)
	if err != nil {
		h.render(w, "contents", ContentsData{BaseData: base, CourseID: courseID, CourseName: courseName, WebClassURL: webClassCourseURL(courseID), Error: lms.Describe("資料の一覧", err)})
		return
	}
	data := ContentsData{
		BaseData:    base,
		CourseID:    courseID,
		CourseName:  courseName,
		Contents:    cc.Materials,
		WebClassURL: webClassCourseURL(courseID),
	}
	if cc.AttendanceErr != nil {
		data.AttendanceError = lms.Describe("出席確認", cc.AttendanceErr)
	} else {
		now := time.Now()
		rows := make([]AttendanceRow, len(cc.Attendance))
		for i, it := range cc.Attendance {
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
	data := FilesData{
		BaseData:    BaseData{Username: entry.Username},
		CourseID:    courseID,
		CourseName:  r.URL.Query().Get("course"),
		ContentID:   contentID,
		ContentName: contentName,
		WebClassURL: webClassCourseURL(courseID),
	}
	files, err := h.lms.ContentFiles(r.Context(), entry, courseID, contentID)
	if err != nil {
		data.Error = lms.Describe("ファイルの一覧", err)
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

	client, release, err := h.lms.Open(r.Context(), entry)
	defer release()
	if err != nil {
		http.Error(w, lms.Describe("WebClass への接続", err), http.StatusBadGateway)
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

	client, release, err := h.lms.Open(r.Context(), entry)
	defer release()
	if err != nil {
		http.Error(w, lms.Describe("WebClass への接続", err), http.StatusBadGateway)
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
	client, release, err := h.lms.Open(r.Context(), entry)
	defer release()
	if err != nil {
		http.Error(w, lms.Describe("WebClass への接続", err), http.StatusBadGateway)
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

	client, release, err := h.lms.Open(r.Context(), entry)
	defer release()
	if err != nil {
		http.Error(w, lms.Describe("WebClass への接続", err), http.StatusBadGateway)
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
	data := MessagesData{BaseData: BaseData{Username: entry.Username}}
	msgs, unread, err := h.lms.Inbox(r.Context(), entry)
	if err != nil {
		data.Error = lms.Describe("メッセージ", err)
	}
	data.Messages, data.UnreadCnt = msgs, unread
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
