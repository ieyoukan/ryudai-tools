package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/ieyoukan/ryudai-tools/internal/academic"
)

// MySchedule はセッション / Bearer 認証済み本人の実日付の予定を返す。
func (h *AcademicHandler) MySchedule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	q := r.URL.Query()
	days := 0
	if raw := q.Get("days"); raw != "" {
		var err error
		days, err = strconv.Atoi(raw)
		if err != nil || days < 1 {
			writeAcademicError(w, academic.ErrScheduleQuery)
			return
		}
	}
	res, err := h.Svc.MySchedule(r.Context(), user, q.Get("date"), days, q.Get("refresh") == "1")
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	if q.Get("format") == "ics" {
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="university-`+res.StartDate+`.ics"`)
		w.Write([]byte(academic.ScheduleICS(user, res)))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// MyLecture は本人の講義の授業明細スケジュール（全回の日付・時刻・教室・担当・備考）を返す。
func (h *AcademicHandler) MyLecture(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	year, err := strconv.Atoi(chi.URLParam(r, "year"))
	if err != nil {
		writeAcademicError(w, academic.ErrLectureQuery)
		return
	}
	res, err := h.Svc.MyLectureSchedule(user, year, chi.URLParam(r, "code"), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
