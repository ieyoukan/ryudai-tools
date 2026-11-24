package webclass

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// コースのページ以外（ログイン画面・エラー画面）が返ったら、資料 0 件ではなくエラーにする。
func TestFetchCourseContentsRejectsNonCoursePages(t *testing.T) {
	for name, tc := range map[string]struct {
		page string
		want error
	}{
		"login redirect": {`<html><head><title>PageJump - WebClass</title></head><script>window.top.location.href="/webclass/login.php";</script></html>`, ErrSessionExpired},
		"login form":     {`<form><input name="username"><input type="password" name="password"></form>`, ErrSessionExpired},
		"other page":     {`<html><head><title>エラー - WebClass</title></head><body>アクセスできません</body></html>`, ErrUnexpectedPage},
	} {
		t.Run(name, func(t *testing.T) {
			c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.page)
			}))
			defer ts.Close()
			if got, err := c.FetchCourseContents("C1"); !errors.Is(err, tc.want) {
				t.Fatalf("FetchCourseContents = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// 資料の無いコースは、エラーではなく空の一覧（nil ではない）を返す。
func TestFetchCourseContentsEmptyCourse(t *testing.T) {
	c, ts := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>空の科目 - WebClass</title></head><body><div id="courseLearningLayoutModal"></div><a href="/webclass/course.php/C1/manage">C1</a></body></html>`)
	}))
	defer ts.Close()
	got, err := c.FetchCourseContents("C1")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("FetchCourseContents = %#v, %v; want empty list", got, err)
	}
}
