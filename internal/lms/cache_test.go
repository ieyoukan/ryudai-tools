package lms

import (
	"context"
	"testing"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// 時間割と資料一覧はキャッシュから返し、「最新にする」とログアウトで取り直す。
// 出席確認は短い時間だけ持ち、利用者ごとに分ける。
func TestCachesReduceWebClassRequests(t *testing.T) {
	fake := siblingCourses()
	ts := fake.Start()
	defer ts.Close()
	store := session.NewMemStore()
	s := New(ts.URL, store, nil)
	ctx := context.Background()
	a := loggedIn(t, s, store, "e000001", "pw")
	b := loggedIn(t, s, store, "e000002", "pw2")

	for range 3 {
		if _, err := s.Courses(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.Hits("top"); got != 1 {
		t.Fatalf("timetable fetched %d times, want 1", got)
	}
	if _, err := s.Courses(Refresh(ctx), a); err != nil || fake.Hits("top") != 2 {
		t.Fatalf("refresh did not refetch: hits %d err %v", fake.Hits("top"), err)
	}
	// 別の利用者には、同じ時間割でも自分の分を取りに行く
	if _, err := s.Courses(ctx, b); err != nil || fake.Hits("top") != 3 {
		t.Fatalf("other user's cache was shared: hits %d err %v", fake.Hits("top"), err)
	}

	const course = "26S990001001"
	if _, err := s.CourseContents(ctx, a, course); err != nil {
		t.Fatal(err)
	}
	coursePages, attendance := fake.Hits("course"), fake.Hits("attendance")
	for range 2 {
		cc, err := s.CourseContents(ctx, a, course)
		if err != nil || len(cc.Materials) != 2 {
			t.Fatalf("contents = %+v, err %v", cc, err)
		}
	}
	if fake.Hits("course") != coursePages || fake.Hits("attendance") != attendance {
		t.Fatalf("cached contents refetched: course page %d→%d, attendance %d→%d", coursePages, fake.Hits("course"), attendance, fake.Hits("attendance"))
	}
	// 出席確認だけ期限が切れたら、資料はキャッシュのまま出席確認だけ取り直す
	s.attendance.now = func() time.Time { return time.Now().Add(AttendanceTTL + time.Second) }
	if _, err := s.CourseContents(ctx, a, course); err != nil || fake.Hits("attendance") != 2 {
		t.Fatalf("attendance was not refreshed: %d err %v", fake.Hits("attendance"), err)
	}

	s.Forget("e000001")
	if _, err := s.Courses(ctx, a); err != nil || fake.Hits("top") != 4 {
		t.Fatalf("cache survived logout: hits %d err %v", fake.Hits("top"), err)
	}
}
