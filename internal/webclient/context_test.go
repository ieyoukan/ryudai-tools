package webclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBindContextCancelsRequests(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // 応答しない大学側のサーバー
	}))
	defer ts.Close()
	defer close(release)

	c, err := New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c.BindContext(ctx)
	start := time.Now()
	_, err = c.FetchPage("index.php")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("FetchPage = %v after %v; want deadline exceeded promptly", err, time.Since(start))
	}
}
