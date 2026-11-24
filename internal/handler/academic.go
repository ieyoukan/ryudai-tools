package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
	"github.com/ieyoukan/ryudai-tools/internal/provider/university"
)

// AcademicHandler は教務システム（シラバス・履修）の JSON API。
//
//	GET  /api/syllabus/search?subject=...&faculty=...&limit=20&details=true   公開
//	GET  /api/syllabus/options?faculty=工学部                                 公開
//	GET  /api/syllabus/{year}/{code}                                        公開
//	GET  /api/me/registration                                               要認証
//	GET  /api/me/candidates?slots=月2,木3&scopes=general,own&keywords=発音,会話&with_syllabus=1&offset=0&limit=30  要認証
//	GET  /api/me/graduation?include_grades=1                                要認証
//	POST /api/me/plan  {"lecture_codes": ["..."]}                           要認証
type AcademicHandler struct {
	Svc *academic.Service
	// User はリクエストのユーザー名を返す（未認証なら ""）。
	User func(*http.Request) string
	// SessionUser はログインセッションだけからユーザー名を返す（書き込みの API 用。API トークンは使えない）。
	SessionUser func(*http.Request) string
	// Plans を設定すると、履修計画の書き込みの API を使える。
	Plans PlanWriter
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeAcademicError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, academic.ErrEmptyQuery), errors.Is(err, academic.ErrScheduleQuery), errors.Is(err, academic.ErrLectureQuery):
		status = http.StatusBadRequest
	case errors.Is(err, kyomu.ErrSyllabusNotFound):
		status = http.StatusNotFound
	case errors.Is(err, academic.ErrNoCredentials), errors.Is(err, kyomu.ErrLoginFailed), errors.Is(err, kyomu.ErrSessionExpired):
		status = http.StatusUnauthorized
	case strings.Contains(err.Error(), "一致する選択肢がありません"):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, errorResponse{Error: err.Error()})
}

// SearchParamsFromQuery は URL クエリからシラバス検索条件を読む（UI と共用）。
func SearchParamsFromQuery(q map[string][]string) kyomu.SearchParams {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	year, _ := strconv.Atoi(get("year"))
	return kyomu.SearchParams{
		Year:          year,
		Term:          get("term"),
		Faculty:       get("faculty"),
		Department:    get("department"),
		Division:      get("division"),
		Day:           get("day"),
		Period:        get("period"),
		NumberingCode: get("numbering_code"),
		LectureCode:   get("lecture_code"),
		Subject:       get("subject"),
		Instructor:    get("instructor"),
		LectureType:   get("lecture_type"),
		Keyword:       get("keyword"),
	}
}

func (h *AcademicHandler) SearchSyllabus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	details := q.Get("details") != "false" && q.Get("details") != "0"
	res, err := h.Svc.SearchLectures(h.optionalUser(r), SearchParamsFromQuery(q), limit, details)
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AcademicHandler) SyllabusOptions(w http.ResponseWriter, r *http.Request) {
	res, err := h.Svc.SyllabusOptions(r.URL.Query().Get("faculty"))
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AcademicHandler) Syllabus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	year, err := strconv.Atoi(chi.URLParam(r, "year"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "year は数字で指定してください"})
		return
	}
	res, err := h.Svc.Lecture(h.optionalUser(r), year, chi.URLParam(r, "code"))
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AcademicHandler) requireUser(w http.ResponseWriter, r *http.Request) string {
	user := h.User(r)
	if user == "" {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "認証が必要です（Authorization: Bearer <トークン> または X-Session-ID）"})
	}
	return user
}

func (h *AcademicHandler) MyRegistration(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	res, err := h.Svc.Overview(user)
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AcademicHandler) MyCandidates(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	q := r.URL.Query()
	cq := academic.CandidateQuery{
		DeferDetails:     q.Get("details") == "basic",
		Keyword:          q.Get("filter"),
		IncludeFull:      q.Get("full") == "1" || q.Get("full") == "true",
		SyllabusKeywords: splitList(q.Get("keywords")),
		WithSyllabus:     q.Get("with_syllabus") == "1" || q.Get("with_syllabus") == "true",
	}
	cq.Offset, _ = strconv.Atoi(q.Get("offset"))
	cq.Limit, _ = strconv.Atoi(q.Get("limit"))
	if cq.Limit <= 0 || cq.Limit > 100 {
		cq.Limit = 100
	}
	for _, s := range splitList(q.Get("slots")) {
		slots := kyomu.ParseSlots(s)
		if len(slots) == 0 {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "slots は 月2,木3 のように指定してください"})
			return
		}
		cq.Slots = append(cq.Slots, slots...)
	}
	for _, s := range splitList(q.Get("scopes")) {
		cq.Scopes = append(cq.Scopes, kyomu.CandidateScope(s))
	}
	res, err := h.Svc.FindCandidatesContext(r.Context(), user, cq)
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// MyGraduation は GET /api/me/graduation?include_grades=1（成績と卒業要件の見通し）。
func (h *AcademicHandler) MyGraduation(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	inc := r.URL.Query().Get("include_grades")
	res, err := h.Svc.GraduationStatusContext(r.Context(), user, inc == "1" || inc == "true")
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AcademicHandler) MyPlan(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	var body struct {
		LectureCodes []string `json:"lecture_codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.LectureCodes) == 0 || len(body.LectureCodes) > 20 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: `{"lecture_codes": ["..."]}（1〜20 件）を送ってください`})
		return
	}
	res, err := h.Svc.CheckPlanContext(r.Context(), user, body.LectureCodes)
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '、' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (h *AcademicHandler) optionalUser(r *http.Request) string {
	if h.User == nil {
		return ""
	}
	return h.User(r)
}

func (h *AcademicHandler) RequiredCourses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	q := r.URL.Query()
	year, err := requiredEnterYear(q.Get("enter_year"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	user := h.optionalUser(r)
	if user == "" && year == 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "未認証の場合は enter_year に適用便覧年度を指定してください"})
		return
	}
	res, err := h.Svc.RequiredCourses(r.Context(), user, year, q.Get("faculty"), q.Get("course"), q.Get("all_courses") == "1" || q.Get("all_courses") == "true")
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *AcademicHandler) MyRequiredCourses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	year, err := requiredEnterYear(r.URL.Query().Get("enter_year"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	res, err := h.Svc.RequiredCourseStatus(r.Context(), user, year)
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func requiredEnterYear(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	year, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || year < 2000 || year > university.CurrentYear()+1 {
		return 0, fmt.Errorf("enter_year に有効な適用便覧年度を指定してください")
	}
	return year, nil
}
func (h *AcademicHandler) Calendar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	res, err := h.Svc.University.Calendar(r.Context(), q.Get("date"), days, q.Get("all") == "1")
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
func (h *AcademicHandler) Timetable(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year, _ := strconv.Atoi(q.Get("year"))
	res, err := h.Svc.University.Timetable(r.Context(), year, q.Get("term"), q.Get("faculty"), q.Get("course"), splitList(q.Get("lecture_codes")))
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
func (h *AcademicHandler) StudyRules(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	q := r.URL.Query()
	year, _ := strconv.Atoi(q.Get("enter_year"))
	res, err := h.Svc.StudyRules(r.Context(), h.optionalUser(r), year, q.Get("faculty"), q.Get("course"), q.Get("keyword"))
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
