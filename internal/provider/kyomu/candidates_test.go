package kyomu

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFetchCandidatesDistinguishesEmptyAndUnavailable(t *testing.T) {
	fixture, err := os.ReadFile("testdata/candidates.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body string
		count      int
		wantError  bool
	}{
		{"available", string(fixture), 11, false},
		{"empty", `<table id="gv"><tr><th>講義コード</th><th>科目名</th><th>定員</th><th>登録者数</th></tr></table>`, 0, false},
		{"redirected page", `<p>照会画面を利用できません</p>`, 0, true},
		{"changed columns", `<table id="gv"><tr><th>講義コード</th></tr></table>`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "RegistSearchResults.aspx") {
					t.Errorf("did not directly read the candidate window: %s %s", r.Method, r.URL)
				}
				w.Write([]byte(tc.body))
			}))
			defer ts.Close()
			c, err := New(ts.URL)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.FetchCandidates("火", 3, ScopeOwn)
			if (err != nil) != tc.wantError || len(got) != tc.count {
				t.Fatalf("count=%d error=%v; want count=%d error=%v", len(got), err, tc.count, tc.wantError)
			}
		})
	}
}
