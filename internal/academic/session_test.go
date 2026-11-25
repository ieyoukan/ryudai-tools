package academic

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

type memCookies map[string][]*http.Cookie

func (m memCookies) KyomuCookies(userID string) ([]*http.Cookie, error) { return m[userID], nil }
func (m memCookies) SaveKyomuCookies(userID string, cookies []*http.Cookie) error {
	m[userID] = cookies
	return nil
}
func (m memCookies) DeleteKyomuCookies(userID string) error {
	delete(m, userID)
	return nil
}

func TestWithUserUsesStoredCookiesWithoutPassword(t *testing.T) {
	stored := memCookies{"student": {{Name: "ASP.NET_SessionId", Value: "saved", Path: "/"}}}
	svc := New("https://kyomu.example/portal", nil)
	svc.Cookies = stored

	var got string
	err := svc.withUser("student", func(c *kyomu.Client) error {
		for _, cookie := range c.Cookies() {
			if cookie.Name == "ASP.NET_SessionId" {
				got = cookie.Value
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withUser: %v", err)
	}
	if got != "saved" {
		t.Fatalf("session cookie = %q, want saved", got)
	}
}

func TestWithUserDropsExpiredCookiesWithoutPassword(t *testing.T) {
	stored := memCookies{"student": {{Name: "ASP.NET_SessionId", Value: "old", Path: "/"}}}
	svc := New("https://kyomu.example/portal", nil)
	svc.Cookies = stored

	calls := 0
	err := svc.withUser("student", func(c *kyomu.Client) error {
		calls++
		return kyomu.ErrSessionExpired
	})
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("err = %v, want ErrNoCredentials", err)
	}
	if calls != 1 {
		t.Fatalf("fn calls = %d, want 1", calls)
	}
	if _, ok := stored["student"]; ok {
		t.Fatal("expired cookies were kept")
	}
}

func TestForgetDeletesStoredCookies(t *testing.T) {
	stored := memCookies{"student": {{Name: "ASP.NET_SessionId", Value: "saved", Path: "/"}}}
	svc := New("https://kyomu.example/portal", nil)
	svc.Cookies = stored
	svc.Forget("student")
	if _, ok := stored["student"]; ok {
		t.Fatal("cookies were kept after logout")
	}
}

type memPublic map[string][]byte

func (m memPublic) LoadPublic(key string) ([]byte, string, string, time.Time, bool) {
	v, ok := m[key]
	return v, "", "", time.Now(), ok
}
func (m memPublic) SavePublic(key string, value []byte, _, _ string) error {
	m[key] = value
	return nil
}

// シラバス（公開情報）は保存して共有し、再起動後は教務システムへ取り直さずに使う。
func TestSyllabusIsSharedThroughPublicStore(t *testing.T) {
	store := memPublic{"syllabus/2026/990000001": []byte(`{"Year":2026,"LectureCode":"990000001","Title":"試験科目"}`)}
	s := New("http://127.0.0.1:9", nil) // 教務システムには届かない
	s.UsePublicStore(store)
	sy, err := s.Syllabus(2026, "990000001")
	if err != nil || sy.Title != "試験科目" {
		t.Fatalf("syllabus = %+v, err %v", sy, err)
	}
	if s.University.Store == nil {
		t.Fatal("official documents do not use the public store")
	}
}
