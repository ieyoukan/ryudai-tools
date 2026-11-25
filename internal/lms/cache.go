package lms

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// 利用者ごとのキャッシュの保持時間。WebClass の資料一覧には資料ごとの更新日時が無いため、
// 変更は「この時間がたったら取り直す」と画面の「最新にする」（Refresh）で拾う。
// 中身は本人のものとしてメモリ上だけに持ち、データベースには保存しない（プライバシーポリシーどおり）。
const (
	CoursesTTL    = 3 * time.Hour    // 時間割（学期中はほぼ変わらない）
	MaterialsTTL  = 10 * time.Minute // コースの資料一覧
	AttendanceTTL = 1 * time.Minute  // 出席確認（受付状況・出席済みかはすぐ変わる）
	FilesTTL      = 6 * time.Hour    // 資料 1 件のファイル一覧（取得のたびに WebClass で資料を開くため長めに持つ）
)

type refreshKey struct{}

// Refresh は ctx を「キャッシュを使わずに取り直す」指定にする（画面の「最新にする」）。
func Refresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, refreshKey{}, true)
}

func isRefresh(ctx context.Context) bool {
	v, _ := ctx.Value(refreshKey{}).(bool)
	return v
}

// userCache は利用者ごとに分けたキャッシュ。キーの先頭は必ず利用者名なので、
// 同じコースでも別の利用者の結果は使わない（受講していない人に資料が見えないようにする）。
type userCache[T any] struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry[T]
}

type cacheEntry[T any] struct {
	value   T
	expires time.Time
}

func newUserCache[T any](ttl time.Duration) *userCache[T] {
	return &userCache[T]{ttl: ttl, now: time.Now, entries: map[string]cacheEntry[T]{}}
}

func cacheKey(username string, parts ...string) string {
	return strings.ToLower(username) + "\x00" + strings.Join(parts, "\x00")
}

func (c *userCache[T]) get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.now().After(e.expires) {
		delete(c.entries, key)
		var zero T
		return zero, false
	}
	return e.value, true
}

func (c *userCache[T]) set(key string, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	// 期限切れを掃除して、使われなくなった利用者の分がたまり続けないようにする。
	if len(c.entries) > 1000 {
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	c.entries[key] = cacheEntry[T]{value: v, expires: now.Add(c.ttl)}
}

// forget は利用者のキーをすべて消す（prefix が空なら利用者の全件）。
func (c *userCache[T]) forget(username string, prefix ...string) {
	p := cacheKey(username, prefix...)
	if len(prefix) == 0 {
		p = strings.ToLower(username) + "\x00"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if strings.HasPrefix(k, p) {
			delete(c.entries, k)
		}
	}
}

// ErrSessionInvalid はログインセッションがもう無いことを示す（ログアウト済みなど）。
var ErrSessionInvalid = errors.New("ログインセッションが無効になりました")

// useCache はキャッシュを返してよいか。「最新にする」の指定が無く、ログインセッションがまだ有効なときだけ。
func (s *Service) useCache(ctx context.Context, entry *session.Entry) (bool, error) {
	if _, ok := s.sessions.Get(entry.SessionID); !ok {
		return false, ErrSessionInvalid
	}
	return !isRefresh(ctx), nil
}
