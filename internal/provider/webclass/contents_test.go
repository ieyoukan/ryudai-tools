package webclass

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient はテストサーバーを向いた WebClass クライアントを生成する。
func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	c, err := New(ts.URL)
	if err != nil {
		t.Fatalf("クライアント作成: %v", err)
	}
	return c, ts
}

func TestResolveFileDownloadURL_NonHTML_ReturnsOriginal(t *testing.T) {
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4"))
	}))
	defer ts.Close()

	input := ts.URL + "/webclass/file_down.php?file=foo&file_name=foo.pdf"
	got := c.resolveFileDownloadURL(input)
	if got != input {
		t.Errorf("non-HTML 応答で URL が書き換わった: got=%q want=%q", got, input)
	}
}

func TestResolveFileDownloadURL_HTML_ExtractsRealURL(t *testing.T) {
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `
<html>
<body>
<a href='/webclass/download.php/real.pdf?file=abc&target_type=attach'>第1回-資料.pdf</a>
</body>
</html>`)
	}))
	defer ts.Close()

	input := ts.URL + "/webclass/file_down.php?file=foo&file_name=foo.pdf"
	got := c.resolveFileDownloadURL(input)
	want := ts.URL + "/webclass/download.php/real.pdf?file=abc&target_type=attach"
	if got != want {
		t.Errorf("実ファイル URL が抽出できない: got=%q want=%q", got, want)
	}
}

func TestResolveFileDownloadURL_HTML_NoLink_ReturnsOriginal(t *testing.T) {
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body>エラーが発生しました</body></html>`)
	}))
	defer ts.Close()

	input := ts.URL + "/webclass/file_down.php?file=foo&file_name=foo.pdf"
	got := c.resolveFileDownloadURL(input)
	if got != input {
		t.Errorf("ダウンロードリンクがない場合は元の URL を返すべき: got=%q want=%q", got, input)
	}
}

func TestResolveFileDownloadURL_HTML_RelativeLink_Resolved(t *testing.T) {
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body><a href="download.php/real.pdf?file=abc">資料</a></body></html>`)
	}))
	defer ts.Close()

	input := ts.URL + "/webclass/file_down.php?file=foo"
	got := c.resolveFileDownloadURL(input)
	want := ts.URL + "/webclass/download.php/real.pdf?file=abc"
	if got != want {
		t.Errorf("相対 URL が解決できない: got=%q want=%q", got, want)
	}
}

func TestParseChapterFiles_FileDownHTML_Resolved(t *testing.T) {
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "download.php") {
			w.Header().Set("Content-Type", "application/pdf")
			io.WriteString(w, "%PDF")
			return
		}
		// file_down.php 中継ページ
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<a href="/webclass/download.php/real.pdf?file=abc">資料</a>`)
	}))
	defer ts.Close()

	body := []byte(`
<html>
<body>
<table>
<tr data-page='1'>
  <td><span>第1章</span><span>サンプル</span></td>
  <td><a href="` + ts.URL + `/webclass/file_down.php?file=foo&file_name=foo.pdf">添付</a></td>
</tr>
</table>
</body>
</html>`)

	files := c.parseChapterFiles(body)
	if len(files) == 0 {
		t.Fatalf("ファイルが取得できない")
	}

	found := false
	for _, f := range files {
		if strings.Contains(f.URL, "download.php/real.pdf") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("HTML 中継ページが解決されていない: %+v", files)
	}
}
