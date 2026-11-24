package webclass

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

const materialsCoursePage = `<html><head><title>テスト科目[2026後期][試験001] - WebClass</title></head><body>
<a href="/webclass/course.php/C1/manage">コース C1</a>
<section data-contents-id="m1" data-contents-name="第1回 資料"><div class="cl-contentsList_categoryLabel">資料</div></section>
<section data-contents-id="t1" data-contents-name="中間試験"><div class="cl-contentsList_categoryLabel">試験</div></section>
<section data-contents-id="r1" data-contents-name="課題1"><div class="cl-contentsList_categoryLabel">レポート</div></section>
<section data-contents-id="q1" data-contents-name="授業アンケート"><div class="cl-contentsList_categoryLabel">匿名アンケート</div></section>
</body></html>`

func TestFetchCourseContentsReturnsOnlyMaterials(t *testing.T) {
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, materialsCoursePage)
	}))
	defer ts.Close()

	contents, err := c.FetchCourseContents("C1")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].ID != "m1" {
		t.Fatalf("contents = %+v, want only m1", contents)
	}
}

// 試験やアンケートを do_contents.php で開くと「開始」扱いになるので、資料以外はアクセス自体をしない。
func TestFetchContentFilesRejectsNonMaterialsWithoutOpeningThem(t *testing.T) {
	var opened atomic.Int32
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "do_contents.php") || strings.Contains(r.URL.Path, "show_info.php") || r.Method == http.MethodPost {
			opened.Add(1)
		}
		fmt.Fprint(w, materialsCoursePage)
	}))
	defer ts.Close()

	for _, id := range []string{"t1", "r1", "q1", "unknown"} {
		if _, err := c.FetchContentFiles("C1", id); !errors.Is(err, ErrNotMaterial) {
			t.Fatalf("FetchContentFiles(%s) err = %v, want ErrNotMaterial", id, err)
		}
		if _, err := c.FetchContentText("C1", id); !errors.Is(err, ErrNotMaterial) {
			t.Fatalf("FetchContentText(%s) err = %v, want ErrNotMaterial", id, err)
		}
	}
	if n := opened.Load(); n != 0 {
		t.Fatalf("non-material content was opened %d times", n)
	}
}
