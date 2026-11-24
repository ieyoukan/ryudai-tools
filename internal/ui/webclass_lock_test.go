package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// WebClass はセッションごとに「今いるコース」を 1 つだけ持つ。同じ利用者が同じ授業の 2 コースを
// 同時に開いても、それぞれのコースの資料が返ること（講義ページで発電工学の 2 コースが空になった不具合）。
func TestContentsOfTwoCoursesAtOnceDoNotMixUp(t *testing.T) {
	var mu sync.Mutex
	current := ""
	webclassServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutPrefix(r.URL.Path, "/course.php/"); ok && strings.HasSuffix(id, "/login") {
			mu.Lock()
			current = strings.TrimSuffix(id, "/login")
			mu.Unlock()
			time.Sleep(30 * time.Millisecond) // この間に別のコースへ入ると、実物と同じく中身が入れ替わる
		}
		mu.Lock()
		course := current
		mu.Unlock()
		fmt.Fprintf(w, `<html><body><section data-contents-id="m-%s" data-contents-name="%s の資料"><div class="cl-contentsList_categoryLabel">資料</div></section></body></html>`, course, course)
	}))
	defer webclassServer.Close()

	store := session.NewMemStore()
	sid := store.Create("student", []*http.Cookie{{Name: "WBT_Session", Value: "s", Path: "/"}})
	h := NewHandler(&config.Config{WebClassBaseURL: webclassServer.URL}, store, nil, nil)

	open := func(courseID string) string {
		r := httptest.NewRequest(http.MethodGet, "/courses/"+courseID, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		rc := chi.NewRouteContext()
		rc.URLParams.Add("courseID", courseID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
		w := httptest.NewRecorder()
		h.ShowContents(w, r)
		return w.Body.String()
	}

	for round := 0; round < 3; round++ {
		var wg sync.WaitGroup
		bodies := map[string]string{}
		var bmu sync.Mutex
		for _, id := range []string{"A", "B"} {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				body := open(id)
				bmu.Lock()
				bodies[id] = body
				bmu.Unlock()
			}(id)
		}
		wg.Wait()
		for id, body := range bodies {
			if !strings.Contains(body, "m-"+id) {
				t.Fatalf("round %d: course %s got another course's contents: %s", round, id, body)
			}
		}
	}
}
