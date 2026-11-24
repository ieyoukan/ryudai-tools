package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

type candidateTransport func(*http.Request) (*http.Response, error)

func (f candidateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCandidatePageReturnsBeforeEnrichmentAndLoadsPrivateDetails(t *testing.T) {
	fixture := func(name string) []byte {
		b, err := os.ReadFile("../provider/kyomu/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	reg, cands, syllabus := fixture("regist_edit.html"), fixture("candidates.html"), fixture("syllabus_detail.html")
	var syllabusCalls, candidateCalls, officialCalls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/Login.aspx" && r.Method == http.MethodGet:
			fmt.Fprint(w, `<form action="/Login.aspx"><input type="submit" name="x_btnLogin" id="x_btnLogin" value="login"></form>`)
		case r.URL.Path == "/Login.aspx" && r.Method == http.MethodPost:
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "fixture", Path: "/"})
			http.Redirect(w, r, "/StudentApp/Top.aspx", http.StatusFound)
		case r.URL.Path == "/StudentApp/Top.aspx":
			fmt.Fprint(w, "ok")
		case strings.Contains(r.URL.Path, "BasicInfo.aspx"):
			fmt.Fprint(w, `<span id="ctl00_phContents_ucForm_trEnterYear_lbl_lbl">2024</span><span id="ctl00_phContents_ucForm_trFacName_lbl_lbl">工学部</span><span id="ctl00_phContents_ucForm_trDivName_lbl_lbl">知能情報コース</span>`)
		case strings.Contains(r.URL.Path, "RegistEditStart.aspx"):
			w.Write(reg)
		case strings.Contains(r.URL.Path, "RegistSearchResults.aspx"):
			candidateCalls.Add(1)
			w.Write(cands)
		case strings.Contains(r.URL.Path, "DetailMain.aspx"):
			syllabusCalls.Add(1)
			w.Write(syllabus)
		default:
			t.Errorf("unexpected upstream request %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	svc := academic.New(ts.URL, func(string) (string, bool) { return "fixture", true })
	svc.University.HTTP.Transport = candidateTransport(func(r *http.Request) (*http.Response, error) {
		officialCalls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<a href="/2026/">2026年度 学部生用 後学期 配当表</a>`)), Request: r}, nil
	})
	store := session.NewMemStore()
	id := store.Create("alice", nil)
	h := NewHandler(&config.Config{}, store, nil, svc)
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: id})
		w := httptest.NewRecorder()
		h.ShowCandidates(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) CandidatesData {
		var d CandidatesData
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err, w.Body.String())
		}
		return d
	}
	q := "?go=1&slots=%E6%9C%882&scope=general&filter=%E8%A9%A6%E9%A8%93%E5%BF%83%E7%90%86%E5%AD%A6"
	// まずはシラバスの補足を待たずに一覧だけ返す
	w := request("/registration/candidates" + q)
	first := decode(w)
	if w.Code != 200 || first.Result == nil || len(first.Result.Candidates) != 1 || first.Result.Candidates[0].LectureCode != "990000101" ||
		!first.DetailsDeferred || syllabusCalls.Load() != 0 || officialCalls.Load() != 0 {
		t.Fatalf("initial list waited for enrichment: status=%d syllabus=%d official=%d body=%s", w.Code, syllabusCalls.Load(), officialCalls.Load(), w.Body.String())
	}
	// details=full で補足つき。候補の一覧は読み直さない（キャッシュ）
	w = request("/registration/candidates" + q + "&details=full")
	full := decode(w)
	if w.Code != 200 || full.DetailsDeferred || len(full.Result.Candidates) != 1 ||
		len(full.Result.Errors) == 0 || syllabusCalls.Load() == 0 || officialCalls.Load() == 0 || candidateCalls.Load() != 1 {
		t.Fatalf("details, cache or warning lost: %+v status=%d candidates=%d", full.Result, w.Code, candidateCalls.Load())
	}
	// 未ログインはログイン画面へ（画面には {"redirect": "/login"} で返る）
	w = httptest.NewRecorder()
	h.PageAPI().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/registration/candidates"+q, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"redirect":"/login"`) {
		t.Fatalf("unauthenticated candidates: %d %s", w.Code, w.Body.String())
	}
}
