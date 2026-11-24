package academic

import (
	"errors"
	"net/http"
	"testing"

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
