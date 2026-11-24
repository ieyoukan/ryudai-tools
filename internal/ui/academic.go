package ui

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/handler"
	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

type RegCell struct {
	Day      string
	Period   int
	Lectures []kyomu.RegisteredLecture
}

type RegRow struct {
	Period int
	Cells  []RegCell
}

type RegistrationData struct {
	BaseData        `tstype:",extends"`
	Status          *kyomu.RegistrationStatus
	RegistrationURL string
	Overview        *academic.Overview
	Days            []string
	Rows            []RegRow
	Intensive       []kyomu.RegisteredLecture
	Plan            *academic.PlanCheck
	PlanCodes       string
	PlanError       string
	Error           string
}

type RegistrationStatusData struct {
	BaseData `tstype:",extends"`
	Status   *kyomu.RegistrationStatus
	Error    string
}

func (h *Handler) ShowRegistrationStatus(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data := RegistrationStatusData{BaseData: BaseData{Username: entry.Username}}
	status, err := h.academic.RegistrationStatus(entry.Username)
	data.Status = status
	if err != nil {
		data.Error = academicErrorMessage(err)
	}
	h.render(w, "registration_status", data)
}

type CandidatesData struct {
	BaseData        `tstype:",extends"`
	Result          *academic.CandidateResult
	Slots           string
	Scopes          map[string]bool
	Filter          string
	Keywords        string // シラバス全文で探す語（空白区切り）
	Full            bool
	Error           string
	Searched        bool
	DetailsDeferred bool
	FullDetailsURL  string
}

type SyllabusSearchData struct {
	BaseData `tstype:",extends"`
	Query    kyomu.SearchParams
	Options  *kyomu.SearchOptions
	Result   *academic.SearchResult
	Error    string
}

type SyllabusDetailData struct {
	BaseData `tstype:",extends"`
	Syllabus *academic.LectureDetails
	Error    string
}

// academicErrorMessage は教務システム関連のエラーを画面向けの文にする。
func academicErrorMessage(err error) string {
	switch {
	case errors.Is(err, academic.ErrNoCredentials):
		return "教務システムのセッションが切れています。一度ログアウトして再ログインしてください。"
	case errors.Is(err, kyomu.ErrLoginFailed):
		return "教務システムへのログインに失敗しました。パスワードを変更した場合は Ryudai Tools に再ログインしてください。"
	}
	return "教務システムからの取得に失敗しました: " + err.Error()
}

// ShowRegistration は GET /registration（履修登録の確認）を処理する。
// ?plan=講義コード を付けると追加したい講義のコマ被り・単位上限もチェックする。
func (h *Handler) ShowRegistration(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data := RegistrationData{BaseData: BaseData{Username: entry.Username}}
	data.RegistrationURL = strings.TrimRight(h.academic.BaseURL(), "/") + "/StudentApp/Regist/RegistList.aspx"
	status, err := h.academic.RegistrationStatus(entry.Username)
	data.Status = status
	if err != nil {
		data.Error = academicErrorMessage(err)
		h.render(w, "registration", data)
		return
	}
	if status.State != "open" {
		h.render(w, "registration", data)
		return
	}
	ov, err := h.academic.Overview(entry.Username)
	if err != nil {
		data.Error = academicErrorMessage(err)
		h.render(w, "registration", data)
		return
	}
	data.Overview = ov
	data.Days, data.Rows, data.Intensive = buildRegistrationGrid(ov.Registration)

	var codes []string
	for _, v := range r.URL.Query()["plan"] {
		for _, c := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '、' }) {
			if c = strings.TrimSpace(c); c != "" {
				codes = append(codes, c)
			}
		}
	}
	if len(codes) > 0 {
		data.PlanCodes = strings.Join(codes, " ")
		if len(codes) > 20 {
			data.PlanError = "一度にチェックできるのは 20 件までです"
		} else if plan, err := h.academic.CheckPlanContext(r.Context(), entry.Username, codes); err != nil {
			data.PlanError = academicErrorMessage(err)
		} else {
			data.Plan = plan
		}
	}
	h.render(w, "registration", data)
}

func buildRegistrationGrid(reg *kyomu.Registration) ([]string, []RegRow, []kyomu.RegisteredLecture) {
	days := []string{"月", "火", "水", "木", "金"}
	maxPeriod := 5
	var intensive []kyomu.RegisteredLecture
	grid := map[string][]kyomu.RegisteredLecture{}
	for _, l := range reg.Lectures {
		if l.Day == "集中等" {
			intensive = append(intensive, l)
			continue
		}
		if l.Day == "土" && len(days) == 5 {
			days = append(days, "土")
		}
		if l.Period > maxPeriod {
			maxPeriod = l.Period
		}
		k := l.Day + strconv.Itoa(l.Period)
		grid[k] = append(grid[k], l)
	}
	rows := make([]RegRow, 0, maxPeriod)
	for p := 1; p <= maxPeriod; p++ {
		row := RegRow{Period: p}
		for _, d := range days {
			row.Cells = append(row.Cells, RegCell{Day: d, Period: p, Lectures: grid[d+strconv.Itoa(p)]})
		}
		rows = append(rows, row)
	}
	return days, rows, intensive
}

// ShowCandidates は GET /registration/candidates（コマに入れられる講義探し）を処理する。
// ?go=1 で検索する。履修条件・教室などの補足（シラバス）は時間がかかるので、まず省いた一覧を返し、
// 画面は同じ条件に &details=full を付けてもう一度読み、補足つきの一覧に差し替える。
func (h *Handler) ShowCandidates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	data := CandidatesData{
		BaseData: BaseData{Username: entry.Username},
		Slots:    strings.TrimSpace(q.Get("slots")),
		Filter:   strings.TrimSpace(q.Get("filter")),
		Keywords: strings.TrimSpace(q.Get("keywords")),
		Full:     q.Get("full") == "1",
		Scopes:   map[string]bool{},
	}
	for _, s := range q["scope"] {
		data.Scopes[s] = true
	}
	if len(data.Scopes) == 0 {
		data.Scopes["general"], data.Scopes["own"] = true, true
	}
	if q.Get("go") == "" {
		h.render(w, "candidates", data)
		return
	}
	data.Searched = true
	cq := academic.CandidateQuery{Keyword: data.Filter, IncludeFull: data.Full, DeferDetails: q.Get("details") != "full"}
	fullQuery := r.URL.Query()
	fullQuery.Set("details", "full")
	data.FullDetailsURL = "/registration/candidates?" + fullQuery.Encode()
	for _, kw := range strings.FieldsFunc(data.Keywords, func(r rune) bool { return r == ',' || r == ' ' || r == '　' || r == '、' }) {
		cq.SyllabusKeywords = append(cq.SyllabusKeywords, kw)
	}
	// キーワードで絞ったときは件数が少ないので、内容が分かるようシラバスの要点も付ける
	if len(cq.SyllabusKeywords) > 0 {
		cq.WithSyllabus, cq.Limit = true, 40
	}
	for _, s := range strings.FieldsFunc(data.Slots, func(r rune) bool { return r == ',' || r == ' ' || r == '、' }) {
		slots := kyomu.ParseSlots(s)
		if len(slots) == 0 {
			data.Error = "コマは「月2 木3」のように入力してください: " + s
			h.render(w, "candidates", data)
			return
		}
		cq.Slots = append(cq.Slots, slots...)
	}
	for _, sc := range []kyomu.CandidateScope{kyomu.ScopeGeneral, kyomu.ScopeOwn, kyomu.ScopeOther} {
		if data.Scopes[string(sc)] {
			cq.Scopes = append(cq.Scopes, sc)
		}
	}
	res, err := h.academic.FindCandidatesContext(r.Context(), entry.Username, cq)
	if err != nil {
		data.Error = academicErrorMessage(err)
	} else {
		data.Result = res
		data.DetailsDeferred = cq.DeferDetails && len(res.Candidates) > 0
	}
	h.render(w, "candidates", data)
}

// ShowSyllabusSearch は GET /syllabus（ログイン不要）を処理する。
func (h *Handler) ShowSyllabusSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	data := SyllabusSearchData{Query: handler.SearchParamsFromQuery(r.URL.Query())}
	if entry := h.sessionFromCookie(r); entry != nil {
		data.Username = entry.Username
	}
	if opts, err := h.academic.SyllabusOptions(""); err == nil {
		data.Options = opts
	}
	if !data.Query.IsEmpty() {
		res, err := h.academic.SearchLectures(data.Username, data.Query, 50, true)
		if err != nil {
			data.Error = err.Error()
		} else {
			data.Result = res
		}
	}
	h.render(w, "syllabus", data)
}

// ShowSyllabusDetail は GET /syllabus/{year}/{code}（ログイン不要）を処理する。
func (h *Handler) ShowSyllabusDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	data := SyllabusDetailData{}
	if entry := h.sessionFromCookie(r); entry != nil {
		data.Username = entry.Username
	}
	year, _ := strconv.Atoi(chi.URLParam(r, "year"))
	user := data.Username
	// 講義画面では公開シラバスだけを読み、登録人数などの追加照会を省く。
	if r.URL.Query().Get("availability") == "0" {
		user = ""
	}
	s, err := h.academic.Lecture(user, year, chi.URLParam(r, "code"))
	if err != nil {
		data.Error = err.Error()
		if errors.Is(err, kyomu.ErrSyllabusNotFound) {
			w.WriteHeader(http.StatusNotFound)
		}
	}
	data.Syllabus = s
	h.render(w, "syllabus_detail", data)
}

// HandleCreateAPIToken は POST /settings/api-tokens を処理する。
// 発行したトークンは保存せず、この応答でだけ表示する（リダイレクトしない）。
func (h *Handler) HandleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if h.db == nil {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "MCP クライアント"
	}
	if len([]rune(name)) > 40 {
		name = string([]rune(name)[:40])
	}
	token, err := h.db.CreateAPIToken(entry.Username, name)
	if err != nil {
		http.Error(w, "トークン発行に失敗しました", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.renderSettings(w, r, entry.Username, token)
}

// HandleDeleteAPIToken は POST /settings/api-tokens/delete を処理する。
func (h *Handler) HandleDeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if h.db != nil {
		if err := h.db.DeleteAPIToken(entry.Username, r.FormValue("id")); err != nil && !errors.Is(err, db.ErrTokenNotFound) {
			http.Error(w, "トークン削除に失敗しました", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/settings#mcp", http.StatusSeeOther)
}

// HandleRevokeOAuthGrant は POST /settings/oauth-grants/revoke（ChatGPT などとの連携解除）を処理する。
func (h *Handler) HandleRevokeOAuthGrant(w http.ResponseWriter, r *http.Request) {
	entry := h.sessionFromCookie(r)
	if entry == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if h.db != nil {
		if err := h.db.RevokeOAuthGrant(entry.Username, r.FormValue("id")); err != nil && !errors.Is(err, db.ErrOAuthNotFound) {
			http.Error(w, "連携の解除に失敗しました", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/settings#mcp", http.StatusSeeOther)
}

// mcpEndpoint は MCP の URL を返す。PUBLIC_BASE_URL があればそれを、無ければブラウザから見えているホスト名を使う。
func (h *Handler) mcpEndpoint(r *http.Request) string {
	if h.cfg.PublicBaseURL != "" {
		return h.cfg.PublicBaseURL + "/mcp"
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = fh
	}
	return scheme + "://" + host + "/mcp"
}
