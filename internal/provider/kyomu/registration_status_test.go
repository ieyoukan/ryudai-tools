package kyomu

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRegistrationStatusDoesNotConfuseConfirmationWithClosedPeriod(t *testing.T) {
	for _, tc := range []struct{ name, path, body, state string }{
		{"closed", "RegistEditStart.aspx", `<span id="ctl00_phContents_ucRegistMessage_lblMessage_lbl">履修登録の期間内ではありません。</span>`, "closed"},
		{"editable", "RegistEdit.aspx", `<input id="x_btnInsert" type="submit">`, "open"},
		{"confirmation requested", "RegistListConfirm.aspx", `<span id="ctl00_phContents_rrMain_ttTable_lblYearTerm">2026年度後期</span>`, "open"},
		{"unexpected edit page", "RegistEdit.aspx", `<p>メンテナンス中</p>`, "unknown"},
		{"unexpected redirect", "Top.aspx", `<p>トップ</p>`, "unknown"},
		{"disabled button", "RegistEditStart.aspx", `<input id="x_btnInsert" disabled>`, "unknown"},
		{"maintenance", "RegistEditStart.aspx", `<span id="ctl00_phContents_ucRegistMessage_lblMessage_lbl">メンテナンス中</span>`, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := parseHTML("https://example.test/"+tc.path, tc.body)
			if got := registrationStatus(p, "source"); got.State != tc.state || got.Source != "source" || got.FetchedAt.IsZero() {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestRegistrationEntryAndOutsidePeriodTimetable(t *testing.T) {
	list, err := os.ReadFile("testdata/regist_list.html")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/StudentApp/Regist/RegistEditStart.aspx":
			fmt.Fprint(w, `<span id="ctl00_phContents_ucRegistMessage_lblMessage_lbl">履修登録の期間内ではありません。</span>`)
		case "/StudentApp/Regist/RegistList.aspx":
			w.Write(list)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	status, err := c.FetchRegistrationStatus()
	if err != nil || status.State != "closed" || len(paths) != 1 {
		t.Fatalf("%+v %v %v", status, err, paths)
	}
	r, err := c.FetchRegistration()
	if err != nil || r.YearTerm != "2026年度後期" || r.Open || len(r.Lectures) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRegistrationRejectsUnrelatedPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `<form><p>トップページ</p></form>`) }))
	defer srv.Close()
	c, _ := New(srv.URL)
	if r, err := c.FetchRegistration(); err == nil {
		t.Fatalf("unrelated page became empty registration: %+v", r)
	}
}

func TestRegistrationFollowsDisplayedMenusWhenDirectAccessWouldRedirect(t *testing.T) {
	list, err := os.ReadFile("testdata/regist_list.html")
	if err != nil {
		t.Fatal(err)
	}
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/StudentApp/Regist/RegistEditStart.aspx":
			fmt.Fprint(w, `<form action="/start"><input type="hidden" name="__VIEWSTATE" value="start-state"><a id="main" href="javascript:__doPostBack('dynamic$courseMenu','')">履修・成績情報</a></form>`)
		case r.Method == http.MethodPost && r.URL.Path == "/start":
			posts++
			if r.FormValue("__EVENTTARGET") != "dynamic$courseMenu" || r.FormValue("__VIEWSTATE") != "start-state" {
				t.Error("lost entry form state")
			}
			fmt.Fprint(w, `<form action="/blank"><input type="hidden" name="__VIEWSTATE" value="menu-state"><a id="list" href="javascript:__doPostBack('dynamic$timetable','')">履修時間割表</a></form>`)
		case r.Method == http.MethodPost && r.URL.Path == "/blank":
			posts++
			if r.FormValue("__EVENTTARGET") != "dynamic$timetable" || r.FormValue("__VIEWSTATE") != "menu-state" {
				t.Error("lost menu form state")
			}
			w.Write(list)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	r, err := c.FetchRegistration()
	if err != nil || r.YearTerm != "2026年度後期" || r.Open || posts != 2 {
		t.Fatalf("%+v %v posts=%d", r, err, posts)
	}
}
