package webclass

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionRecovery(t *testing.T) {
	for _, mode := range []string{"redirect", "javascript", "form", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			calls, renewals := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				cookie, _ := r.Cookie("WBT_Session")
				if cookie != nil && cookie.Value == "fresh" {
					fmt.Fprint(w, "recovered")
					return
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, "/login.php", http.StatusFound)
				case "javascript":
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, `<script>window.location.href="/login.php?timeout=1";</script>`)
				case "form":
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, `<input name="username"><input type="password" name="val">`)
				case "unauthorized":
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			defer server.Close()
			client, _ := NewFromCookies(server.URL, []*http.Cookie{{Name: "WBT_Session", Value: "old"}})
			client.SetSessionRecovery(func() ([]*http.Cookie, error) {
				renewals++
				return []*http.Cookie{{Name: "WBT_Session", Value: "fresh", Path: "/"}}, nil
			})
			body, err := client.FetchBodyAbs(server.URL + "/")
			if err != nil || string(body) != "recovered" || calls != 2 || renewals != 1 {
				t.Fatalf("body=%q err=%v calls=%d renewals=%d", body, err, calls, renewals)
			}
		})
	}
}

func TestSessionRecoveryOnlyOnceAndDoesNotRetryWrites(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	client, _ := New(server.URL)
	calls := 0
	client.SetSessionRecovery(func() ([]*http.Cookie, error) { calls++; return nil, nil })
	_, err := client.FetchBodyAbs(server.URL + "/")
	if !errors.Is(err, ErrSessionExpired) || calls != 1 {
		t.Fatalf("err=%v renewals=%d", err, calls)
	}
	resp, err := client.HTTPClient.Post(server.URL+"/", "text/plain", strings.NewReader("answer"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 1 {
		t.Fatal("POST was retried")
	}
}

func TestSessionRecoveryPreservesHealthyResponses(t *testing.T) {
	want := `<a href="/login.php">ログアウト</a>` + strings.Repeat("x", 100000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, want)
	}))
	defer server.Close()
	client, _ := New(server.URL)
	client.SetSessionRecovery(func() ([]*http.Cookie, error) { t.Fatal("healthy page triggered login"); return nil, nil })
	resp, err := client.FetchPage("")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != want {
		t.Fatal("response body was truncated")
	}
}

func TestSessionRecoveryDoesNotSendCredentialsToOtherOrigin(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer other.Close()
	client, _ := New("http://example.invalid/webclass")
	client.SetSessionRecovery(func() ([]*http.Cookie, error) { t.Fatal("foreign origin triggered recovery"); return nil, nil })
	resp, err := client.FetchURL(other.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestSessionRecoveryReportsLoginFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	client, _ := New(server.URL)
	want := errors.New("saved credentials unavailable")
	client.SetSessionRecovery(func() ([]*http.Cookie, error) { return nil, want })
	if _, err := client.FetchBodyAbs(server.URL + "/"); !errors.Is(err, want) {
		t.Fatalf("err=%v, want recovery error", err)
	}
}
