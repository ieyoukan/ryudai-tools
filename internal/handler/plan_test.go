package handler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/middleware"
)

func TestPlanWriteAPI(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/app.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := database.NewSessionStore()
	sid := store.Create("student", nil)
	h := &AcademicHandler{
		// API トークンで来たことにする（読み取りでは "student" として扱われる）
		User:        func(r *http.Request) string { return "student" },
		SessionUser: func(r *http.Request) string { return middleware.SessionUser(r, store) },
		Plans:       database,
	}
	r := chi.NewRouter()
	r.Post("/api/me/plan/items", h.AddPlanItem)
	r.Delete("/api/me/plan/items", h.DeletePlan)
	r.Delete("/api/me/plan/items/{id}", h.DeletePlanItem)
	r.Put("/api/me/lecture-categories", h.SaveLectureCategories)
	do := func(method, path, body, contentType string, withSession bool) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if withSession {
			req.AddCookie(&http.Cookie{Name: "ryudai_tools_session", Value: sid})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	item := `{"year_term":"2027前期","title":" 試験画像処理 ","section":"専門教育","category":"知能情報アドバンスト","credits":2}`

	if c := do("POST", "/api/me/plan/items", item, "application/json", false); c != http.StatusUnauthorized {
		t.Fatalf("API トークンだけで書き込めた: %d", c)
	}
	if c := do("POST", "/api/me/plan/items", item, "text/plain", true); c != http.StatusUnsupportedMediaType {
		t.Fatalf("JSON 以外を受け付けた: %d", c)
	}
	if c := do("POST", "/api/me/plan/items", `{"year_term":"2027春","title":"x","category":"y","credits":2}`, "application/json", true); c != http.StatusBadRequest {
		t.Fatalf("不正な学期を受け付けた: %d", c)
	}
	if c := do("POST", "/api/me/plan/items", item, "application/json; charset=utf-8", true); c != http.StatusNoContent {
		t.Fatalf("追加 = %d", c)
	}
	items, _ := database.PlanItems("student")
	if len(items) != 1 || items[0].Title != "試験画像処理" || items[0].Credits != 2 {
		t.Fatalf("items = %+v", items)
	}
	if c := do("DELETE", "/api/me/plan/items/"+itoa(items[0].ID), "", "", true); c != http.StatusNoContent {
		t.Fatalf("削除 = %d", c)
	}
	if items, _ := database.PlanItems("student"); len(items) != 0 {
		t.Fatalf("削除後 = %+v", items)
	}

	if c := do("PUT", "/api/me/lecture-categories", `{"categories":{"990327002":"知能情報アドバンスト"}}`, "application/json", true); c != http.StatusNoContent {
		t.Fatalf("区分の保存 = %d", c)
	}
	if c := do("PUT", "/api/me/lecture-categories", `{"categories":{"../x":"a"}}`, "application/json", true); c != http.StatusBadRequest {
		t.Fatalf("不正な講義コードを受け付けた: %d", c)
	}
	got, _ := database.LectureCategories("student")
	if got["990327002"].Category != "知能情報アドバンスト" || got["990327002"].Source != "user" {
		t.Fatalf("categories = %+v", got)
	}
	if c := do("DELETE", "/api/me/plan/items", "", "", true); c != http.StatusNoContent {
		t.Fatalf("全削除 = %d", c)
	}
	if got, _ := database.LectureCategories("student"); len(got) != 0 {
		t.Fatalf("全削除後の区分 = %+v", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
