package lms

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass/webclasstest"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// 同じ授業が学科ごとに 2 コースに分かれ、資料は片方にだけある（発電工学で起きた形）。
func siblingCourses() *webclasstest.Server {
	return webclasstest.New(map[string]string{"e000001": "pw", "e000002": "pw2"},
		webclasstest.Course{ID: "26S990001001", Name: "試験工学[2026後期][試験336]", Day: "木", Period: 2,
			Materials: []webclasstest.Material{{ID: "m1", Name: "講義資料01"}, {ID: "m2", Name: "講義資料02"}},
			Other:     []webclasstest.Material{{ID: "t1", Name: "小テスト", Category: "試験"}}},
		webclasstest.Course{ID: "26S990002001", Name: "試験工学[2026後期][試験985]", Day: "木", Period: 2},
	)
}

func loggedIn(t *testing.T, s *Service, store session.Store, user, pass string) *session.Entry {
	t.Helper()
	name, cookies, err := s.Login(context.Background(), user, pass)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := store.Get(store.Create(name, cookies))
	return entry
}

func TestServiceAgainstFakeWebClass(t *testing.T) {
	fake := siblingCourses()
	fake.Delay = 20 * time.Millisecond
	ts := fake.Start()
	defer ts.Close()
	store := session.NewMemStore()
	s := New(ts.URL, store, nil)

	if _, _, err := s.Login(context.Background(), "e000001", "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, _, err := s.Login(context.Background(), "student@gmail.com", "pw"); !errors.Is(err, webclass.ErrEmailAsUsername) {
		t.Fatalf("non-university email: %v", err)
	}
	entry := loggedIn(t, s, store, "e000001@cs.u-ryukyu.ac.jp", "pw")
	if entry.Username != "e000001" {
		t.Fatalf("username = %q", entry.Username)
	}

	courses, err := s.Courses(context.Background(), entry)
	if err != nil || len(courses) != 2 || courses[0].Day != "木" || courses[0].Period != 2 {
		t.Fatalf("courses = %+v, err %v", courses, err)
	}

	// 2 コースを同時に開いても入れ替わらない。資料は資料だけ（試験は除く）、空のコースは空の一覧。
	for round := 0; round < 3; round++ {
		var wg sync.WaitGroup
		got := map[string]int{}
		var mu sync.Mutex
		for _, id := range []string{"26S990001001", "26S990002001"} {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				cc, err := s.CourseContents(context.Background(), entry, id)
				if err != nil {
					t.Errorf("%s: %v", id, err)
					return
				}
				mu.Lock()
				got[id] = len(cc.Materials)
				mu.Unlock()
			}(id)
		}
		wg.Wait()
		if got["26S990001001"] != 2 || got["26S990002001"] != 0 {
			t.Fatalf("round %d: materials = %v", round, got)
		}
	}

	// 存在しないコースは空ではなくエラー
	if _, err := s.CourseContents(context.Background(), entry, "26S000000000"); !errors.Is(err, webclass.ErrUnexpectedPage) {
		t.Fatalf("missing course err = %v", err)
	}
}

// WebClass 側でセッションが切れたら、.env の本人は再ログインして続け、他の人には「ログインが切れた」と伝える。
func TestServiceSessionExpiry(t *testing.T) {
	fake := siblingCourses()
	ts := fake.Start()
	defer ts.Close()
	store := session.NewMemStore()
	s := New(ts.URL, store, func(u string) (string, bool) { return "pw", u == "e000001" })

	owner := loggedIn(t, s, store, "e000001", "pw")
	other := loggedIn(t, s, store, "e000002", "pw2")
	fake.ExpireSessions()

	if _, err := s.Courses(context.Background(), owner); err != nil {
		t.Fatalf("owner after expiry: %v", err)
	}
	if fake.Logins() != 3 {
		t.Fatalf("logins = %d, want 3 (2 + 1 re-login)", fake.Logins())
	}
	_, err := s.Courses(context.Background(), other)
	if !errors.Is(err, ErrNoPassword) {
		t.Fatalf("other user after expiry: %v", err)
	}
	if msg := Describe("コース一覧", err); msg != "WebClass のログインが切れました。ログアウトしてもう一度ログインしてください。" {
		t.Fatalf("message = %q", msg)
	}
}
