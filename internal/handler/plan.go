package handler

import (
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/daigaku-hack/internal/academic"
	"github.com/ieyoukan/daigaku-hack/internal/db"
)

// PlanWriter は履修計画と、利用者が選んだ今期の登録科目の区分を書き換える（db.DB が実装）。
type PlanWriter interface {
	AddPlanItem(userID string, it db.PlanItem) error
	DeletePlanItem(userID string, id int64) error
	DeletePlan(userID string) error
	SaveLectureCategory(userID string, c db.LectureCategory) error
	DeleteLectureCategories(userID string) error
}

// 卒業までの履修計画の API（画面 /plan から使う）。
//
//	GET    /api/me/plan/suggestions?term=2027前期     計画に足す候補の科目（要認証）
//	POST   /api/me/plan/items  {"year_term","title","section","category","credits"}  科目を追加（要ログインセッション）
//	DELETE /api/me/plan/items/{id}                    1 科目を削除（要ログインセッション）
//	DELETE /api/me/plan/items                         計画をすべて削除（要ログインセッション）
//	PUT    /api/me/lecture-categories {"categories": {"講義コード": "区分名"}}  今期の登録科目の区分を保存（要ログインセッション）
//
// API トークンは読み取り専用なので、書き込みはログインセッションだけで受け付ける。

// PlanSuggestions は GET /api/me/plan/suggestions を処理する。
func (h *AcademicHandler) PlanSuggestions(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == "" {
		return
	}
	term := r.URL.Query().Get("term")
	if !academic.ValidPlanTerm(term) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "term は 2027前期 のように指定してください"})
		return
	}
	res, err := h.Svc.PlanSuggestions(r.Context(), user, term)
	if err != nil {
		writeAcademicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// requireSessionWrite は書き込みの API の共通チェック。ログインセッションのユーザー名を返す（だめなら応答を書いて ""）。
// JSON の本文を送る API は Content-Type: application/json を必須にして、他サイトのフォームから送らせない。
func (h *AcademicHandler) requireSessionWrite(w http.ResponseWriter, r *http.Request, withBody bool) string {
	if h.Plans == nil || h.SessionUser == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "履修計画を保存するデータベースを利用できません"})
		return ""
	}
	user := h.SessionUser(r)
	if user == "" {
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "ログインしてください（API トークンでは書き込めません）"})
		return ""
	}
	if withBody {
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{Error: "Content-Type: application/json で送ってください"})
			return ""
		}
	}
	return user
}

// AddPlanItem は POST /api/me/plan/items を処理する。
func (h *AcademicHandler) AddPlanItem(w http.ResponseWriter, r *http.Request) {
	user := h.requireSessionWrite(w, r, true)
	if user == "" {
		return
	}
	var it db.PlanItem
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&it); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: academic.ErrInvalidPlanItem.Error()})
		return
	}
	it, err := academic.NormalizePlanItem(it)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if err := h.Plans.AddPlanItem(user, it); err != nil {
		log.Printf("履修計画の保存失敗 (%s): %v", user, err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "計画を保存できませんでした"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeletePlanItem は DELETE /api/me/plan/items/{id} を処理する。
func (h *AcademicHandler) DeletePlanItem(w http.ResponseWriter, r *http.Request) {
	user := h.requireSessionWrite(w, r, false)
	if user == "" {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "id が正しくありません"})
		return
	}
	if err := h.Plans.DeletePlanItem(user, id); err != nil {
		log.Printf("履修計画の削除失敗 (%s): %v", user, err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "計画から削除できませんでした"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeletePlan は DELETE /api/me/plan/items（計画と、選んだ区分をすべて削除）を処理する。
func (h *AcademicHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	user := h.requireSessionWrite(w, r, false)
	if user == "" {
		return
	}
	if err := h.Plans.DeleteLectureCategories(user); err != nil {
		log.Printf("科目区分の削除失敗 (%s): %v", user, err)
	}
	if err := h.Plans.DeletePlan(user); err != nil {
		log.Printf("履修計画の削除失敗 (%s): %v", user, err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "計画を削除できませんでした"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SaveLectureCategories は PUT /api/me/lecture-categories を処理する。
func (h *AcademicHandler) SaveLectureCategories(w http.ResponseWriter, r *http.Request) {
	user := h.requireSessionWrite(w, r, true)
	if user == "" {
		return
	}
	var body struct {
		Categories map[string]string `json:"categories"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body); err != nil || len(body.Categories) > 50 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: `{"categories": {"講義コード": "区分名"}} を送ってください`})
		return
	}
	for code, category := range body.Categories {
		if !academic.ValidLectureCategory(code, category) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "講義コードか区分名が正しくありません: " + code})
			return
		}
	}
	for code, category := range body.Categories {
		if err := h.Plans.SaveLectureCategory(user, db.LectureCategory{LectureCode: code, Category: strings.TrimSpace(category), Source: "user"}); err != nil {
			log.Printf("科目区分の保存失敗 (%s): %v", user, err)
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "区分を保存できませんでした"})
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
