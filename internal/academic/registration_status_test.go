package academic

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRegistrationStatusCachesPerUserAndClearsOnLogout(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/StudentApp/Regist/RegistEditStart.aspx" {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
			return
		}
		calls.Add(1)
		cookie, _ := r.Cookie("session")
		if cookie != nil && cookie.Value == "unknown" {
			fmt.Fprint(w, "メンテナンス中")
			return
		}
		fmt.Fprint(w, `<span id="ctl00_phContents_ucRegistMessage_lblMessage_lbl">履修登録の期間内ではありません。</span>`)
	}))
	defer srv.Close()
	svc := New(srv.URL, nil)
	svc.Cookies = memCookies{"alice": {{Name: "session", Value: "alice"}}, "bob": {{Name: "session", Value: "bob"}}, "unknown": {{Name: "session", Value: "unknown"}}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := svc.RegistrationStatus("alice")
			if err != nil || s.State != "closed" {
				t.Errorf("%+v %v", s, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate requests: %d", calls.Load())
	}
	if _, err := svc.RegistrationStatus("bob"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("status cache shared between users")
	}
	for range 2 {
		if s, err := svc.RegistrationStatus("unknown"); err != nil || s.State != "unknown" {
			t.Fatalf("%+v %v", s, err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal("unknown status was cached")
	}
	svc.Forget("alice")
	if _, err := svc.RegistrationStatus("alice"); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("cached status remained after logout: %v", err)
	}
}
