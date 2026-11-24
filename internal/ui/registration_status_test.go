package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ieyoukan/daigaku-hack/internal/academic"
	"github.com/ieyoukan/daigaku-hack/internal/config"
	"github.com/ieyoukan/daigaku-hack/internal/session"
)

type registrationCookies struct{}

func (registrationCookies) KyomuCookies(string) ([]*http.Cookie, error) {
	return []*http.Cookie{{Name: "session", Value: "fixture"}}, nil
}
func (registrationCookies) SaveKyomuCookies(string, []*http.Cookie) error { return nil }
func (registrationCookies) DeleteKyomuCookies(string) error               { return nil }

func TestClosedRegistrationPageDoesNotFetchOverview(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "RegistEditStart.aspx") {
			t.Errorf("unnecessary request: %s", r.URL)
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `<span id="ctl00_phContents_ucRegistMessage_lblMessage_lbl">履修登録の期間内ではありません。</span>`)
	}))
	defer srv.Close()
	svc := academic.New(srv.URL, nil)
	svc.Cookies = registrationCookies{}
	store := session.NewMemStore()
	id := store.Create("student", nil)
	h := NewHandler(&config.Config{}, store, nil, svc)
	for _, path := range []string{"/registration/status", "/registration"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: id})
		w := httptest.NewRecorder()
		h.PageAPI().ServeHTTP(w, r)
		var data RegistrationData
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err, w.Body)
		}
		if w.Code != 200 || data.Status == nil || data.Status.State != "closed" || data.Overview != nil || data.Error != "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		if path == "/registration" && !strings.HasSuffix(data.RegistrationURL, "/StudentApp/Regist/RegistList.aspx") {
			t.Fatal(data.RegistrationURL)
		}
	}
	if calls != 1 {
		t.Fatalf("upstream requests = %d", calls)
	}
	w := httptest.NewRecorder()
	h.PageAPI().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/registration/status", nil))
	if !strings.Contains(w.Body.String(), `"redirect":"/login"`) {
		t.Fatal(w.Body)
	}
}

func TestLectureSyllabusSkipsPrivateAvailabilityRequests(t *testing.T) {
	body, err := os.ReadFile("../provider/kyomu/testdata/syllabus_detail.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/Public/Syllabus/DetailMain.aspx") {
			t.Errorf("private request: %s", r.URL)
			w.WriteHeader(404)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	svc := academic.New(srv.URL, nil)
	svc.Cookies = registrationCookies{}
	store := session.NewMemStore()
	id := store.Create("student", nil)
	h := NewHandler(&config.Config{}, store, nil, svc)
	r := httptest.NewRequest(http.MethodGet, "/syllabus/2026/990000005?availability=0", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: id})
	w := httptest.NewRecorder()
	h.PageAPI().ServeHTTP(w, r)
	var data SyllabusDetailData
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Error != "" || data.Syllabus == nil || data.Syllabus.Availability.Status != "public_only" || len(data.Syllabus.PlanSessions) != 16 || data.Syllabus.Grading == "" || data.Syllabus.TextbookNote == "" {
		t.Fatalf("%s", w.Body)
	}
}
