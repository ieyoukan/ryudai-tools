package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass/webclasstest"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// 同じ利用者が同じ授業の 2 コースを同時に開いても、それぞれのコースの資料が返る
// （講義ページで発電工学の 2 コースが空になった不具合）。資料の無いコースは空、無いコースはエラー。
func TestContentsOfTwoCoursesAtOnceDoNotMixUp(t *testing.T) {
	fake := webclasstest.New(map[string]string{"e000001": "pw"},
		webclasstest.Course{ID: "A", Name: "試験工学[2026後期][試験336]", Day: "木", Period: 2, Materials: []webclasstest.Material{{ID: "a1", Name: "講義資料01"}}},
		webclasstest.Course{ID: "B", Name: "試験工学[2026後期][試験985]", Day: "木", Period: 2},
	)
	fake.Delay = 20 * time.Millisecond
	ts := fake.Start()
	defer ts.Close()

	store := session.NewMemStore()
	h := NewHandler(&config.Config{WebClassBaseURL: ts.URL}, store, nil, nil)
	_, cookies, err := h.lms.Login(context.Background(), "e000001", "pw")
	if err != nil {
		t.Fatal(err)
	}
	sid := store.Create("e000001", cookies)

	open := func(courseID string) ContentsData {
		r := httptest.NewRequest(http.MethodGet, "/courses/"+url.PathEscape(courseID), nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		rc := chi.NewRouteContext()
		rc.URLParams.Add("courseID", courseID)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
		w := httptest.NewRecorder()
		h.ShowContents(w, r)
		var data ContentsData
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatalf("%s: %v %s", courseID, err, w.Body.String())
		}
		return data
	}

	for round := 0; round < 3; round++ {
		var wg sync.WaitGroup
		var mu sync.Mutex
		got := map[string]ContentsData{}
		for _, id := range []string{"A", "B"} {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				d := open(id)
				mu.Lock()
				got[id] = d
				mu.Unlock()
			}(id)
		}
		wg.Wait()
		if a := got["A"]; a.Error != "" || len(a.Contents) != 1 || a.Contents[0].ID != "a1" {
			t.Fatalf("round %d: A = %+v", round, a)
		}
		if b := got["B"]; b.Error != "" || len(b.Contents) != 0 {
			t.Fatalf("round %d: B = %+v", round, b)
		}
	}
	if d := open("missing"); d.Error == "" || !strings.Contains(d.Error, "想定外のページ") {
		t.Fatalf("missing course = %+v", d)
	}
}
