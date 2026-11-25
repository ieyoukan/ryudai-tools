package university

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type memStore map[string]struct {
	data     []byte
	etag, lm string
	at       time.Time
}

func (m memStore) LoadPublic(key string) ([]byte, string, string, time.Time, bool) {
	v, ok := m[key]
	return v.data, v.etag, v.lm, v.at, ok
}

func (m memStore) SavePublic(key string, value []byte, etag, lm string) error {
	m[key] = struct {
		data     []byte
		etag, lm string
		at       time.Time
	}{value, etag, lm, time.Now()}
	return nil
}

// 公式資料は保存して再起動後も使い、取り直すときは ETag の条件付きで、変わっていなければ本文を受け取らない。
func TestOfficialDocumentsArePersistedAndRevalidated(t *testing.T) {
	const page = "https://www.u-ryukyu.ac.jp/aboutus/calendar/"
	store := memStore{}
	var requests, notModified int
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified++
			return &http.Response{StatusCode: http.StatusNotModified, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
		}
		h := http.Header{"Etag": {`"v1"`}}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("学年暦")), Header: h, Request: r}, nil
	})

	first := New()
	first.Store = store
	first.HTTP.Transport = transport
	if data, _, err := first.fetch(context.Background(), page); err != nil || string(data) != "学年暦" {
		t.Fatalf("first fetch = %q, %v", data, err)
	}

	// 再起動した想定: 新しいクライアントでも、保存した内容を通信なしで使う
	second := New()
	second.Store = store
	second.HTTP.Transport = transport
	if data, src, err := second.fetch(context.Background(), page); err != nil || string(data) != "学年暦" || requests != 1 || src.FetchedAt.IsZero() {
		t.Fatalf("after restart = %q %+v %v, requests %d", data, src, err, requests)
	}

	// 取り直す時期になったら条件付きで問い合わせ、304 なら保存した本文を使う
	entry := store["official:"+page]
	entry.at = time.Now().Add(-refreshAfter - time.Minute)
	store["official:"+page] = entry
	third := New()
	third.Store = store
	third.HTTP.Transport = transport
	if data, _, err := third.fetch(context.Background(), page); err != nil || string(data) != "学年暦" || notModified != 1 {
		t.Fatalf("revalidation = %q %v, 304s %d", data, err, notModified)
	}
	if time.Since(store["official:"+page].at) > time.Minute {
		t.Fatal("304 did not update the saved fetch time")
	}
}
