// Package lms は WebClass（学習管理システム）を、利用者のログインセッションで使うためのサービス層。
//
// 画面や API のハンドラは WebClass のクライアントを直接作らず、ここを通す。ここで次をまとめて扱う。
//   - 利用者ごとに 1 つずつ使う。WebClass はセッションごとに「今いるコース」を 1 つだけ持つため、
//     同じ利用者のコースを並行して開くと中身を取り違える。
//   - リクエストの context を大学側への通信まで伝える。利用者がページを離れたら取得もやめる。
//   - WebClass 側でセッションが切れたときの再ログイン（.env の本人だけ。パスワードは保存しない）。
//   - 一覧などの読み取りの時間の上限と、利用者に見せるエラーの文言。
package lms

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// Sessions はログインセッションの保存先（session.Store の一部）。
type Sessions interface {
	Get(id string) (*session.Entry, bool)
	UpdateCookies(id string, cookies []*http.Cookie) bool
}

// ErrNoPassword は、.env の本人以外でセッションが切れたことを示す（公開サーバーはパスワードを保存しない）。
var ErrNoPassword = errors.New("WebClass のセッションが切れました。ログアウトして再ログインしてください")

// DefaultReadTimeout は一覧やファイル情報の読み取り 1 回の上限。ファイル本体の中継には使わない。
const DefaultReadTimeout = 60 * time.Second

// Service は WebClass のサービス層。
type Service struct {
	baseURL  string
	sessions Sessions
	// password は自動で再ログインできる本人のパスワードを返す（.env の WEBCLASS_USERNAME だけ）。
	password func(username string) (string, bool)
	users    sync.Map // username -> *user

	// ReadTimeout は Courses などの読み取りの上限（0 なら DefaultReadTimeout）。
	ReadTimeout time.Duration
}

type user struct {
	use   chan struct{} // 1 枠のセマフォ。待っている間も context の取り消しで抜けられる
	login sync.Mutex    // 同時に切れたセッションの再ログインを 1 回にまとめる
}

// New は Service を作る。password は nil でもよい（その場合は誰も自動で再ログインしない）。
func New(baseURL string, sessions Sessions, password func(username string) (string, bool)) *Service {
	if password == nil {
		password = func(string) (string, bool) { return "", false }
	}
	return &Service{baseURL: baseURL, sessions: sessions, password: password}
}

func (s *Service) user(username string) *user {
	u, _ := s.users.LoadOrStore(strings.ToLower(username), &user{use: make(chan struct{}, 1)})
	return u.(*user)
}

// Login は ID とパスワードで WebClass にログインし、正規化したユーザー名と Cookie を返す。
// 大学のメールアドレスは @ より前を ID にし、それ以外のメールアドレスは WebClass に送らずに
// webclass.ErrEmailAsUsername を返す。
func (s *Service) Login(ctx context.Context, input, password string) (string, []*http.Cookie, error) {
	username, err := webclass.NormalizeUsername(input)
	if err != nil {
		return "", nil, err
	}
	if username == "" || password == "" {
		return "", nil, errors.New("ユーザー名とパスワードを入力してください")
	}
	client, err := webclass.New(s.baseURL)
	if err != nil {
		return "", nil, err
	}
	client.BindContext(ctx)
	cookies, err := client.Login(username, password)
	if err != nil {
		return "", nil, err
	}
	return username, cookies, nil
}

// Open は利用者のセッションで WebClass を使い始める。返した release を呼ぶまで、同じ利用者の
// 他の操作は待つ。ctx が終わると、順番待ちも大学側との通信も止まる。release は err があっても呼んでよい。
func (s *Service) Open(ctx context.Context, entry *session.Entry) (*webclass.Client, func(), error) {
	u := s.user(entry.Username)
	select {
	case u.use <- struct{}{}:
	case <-ctx.Done():
		return nil, func() {}, ctx.Err()
	}
	release := func() { <-u.use }

	// 別のリクエストが再ログインして Cookie が新しくなっていることがあるので、保存先の最新を使う。
	if current, ok := s.sessions.Get(entry.SessionID); ok {
		entry.Cookies = current.Cookies
	}
	client, err := webclass.NewFromCookies(s.baseURL, entry.Cookies)
	if err != nil {
		release()
		return nil, func() {}, err
	}
	failed := entry.Cookies
	client.SetSessionRecovery(func() ([]*http.Cookie, error) { return s.recover(ctx, entry, failed) })
	client.BindContext(ctx)
	return client, release, nil
}

// Do は Open から release までを fn の実行の間だけ行う。
func (s *Service) Do(ctx context.Context, entry *session.Entry, fn func(*webclass.Client) error) error {
	client, release, err := s.Open(ctx, entry)
	defer release()
	if err != nil {
		return err
	}
	return fn(client)
}

// read は読み取りの時間の上限を付けて Do する。
func (s *Service) read(ctx context.Context, entry *session.Entry, fn func(*webclass.Client) error) error {
	timeout := s.ReadTimeout
	if timeout <= 0 {
		timeout = DefaultReadTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.Do(ctx, entry, fn)
}

// recover は WebClass にログインを求められたとき、.env の本人だけ再ログインして新しい Cookie を返す。
func (s *Service) recover(ctx context.Context, entry *session.Entry, failed []*http.Cookie) ([]*http.Cookie, error) {
	u := s.user(entry.Username)
	u.login.Lock()
	defer u.login.Unlock()
	current, ok := s.sessions.Get(entry.SessionID)
	if !ok {
		return nil, errors.New("ログインセッションが無効になりました")
	}
	// 別のリクエストがすでに再ログインしていれば、その Cookie を使う。
	if !sameCookieValues(current.Cookies, failed) {
		return current.Cookies, nil
	}
	if err := s.relogin(ctx, current); err != nil {
		return nil, err
	}
	return current.Cookies, nil
}

// relogin は .env の本人のときだけ、そのパスワードで再ログインして保存先の Cookie を更新する。
func (s *Service) relogin(ctx context.Context, entry *session.Entry) error {
	password, ok := s.password(entry.Username)
	if !ok {
		return ErrNoPassword
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := webclass.New(s.baseURL)
	if err != nil {
		return err
	}
	client.BindContext(ctx)
	cookies, err := client.Login(entry.Username, password)
	if err != nil {
		return fmt.Errorf("WebClass の再ログインに失敗しました: %w", err)
	}
	if !s.sessions.UpdateCookies(entry.SessionID, cookies) {
		return errors.New("ログインセッションが無効になりました")
	}
	entry.Cookies = cookies
	return nil
}

// RenewIfExpiring は WebClass の Cookie（WCAC）の期限が近いとき、.env の本人に限り再ログインして
// 期限を延ばす。WebClass の keep-alive は Cookie を再発行しないため、この方法でのみ延長できる。
func (s *Service) RenewIfExpiring(ctx context.Context, entry *session.Entry) {
	u := s.user(entry.Username)
	u.login.Lock()
	defer u.login.Unlock()
	current, ok := s.sessions.Get(entry.SessionID)
	if !ok {
		return
	}
	entry.Cookies = current.Cookies
	expiresAt, hasExpiry := session.WebClassCookieExpiresAt(entry.Cookies)
	if !hasExpiry || time.Until(expiresAt) > session.WebClassRenewBefore {
		return
	}
	if _, ok := s.password(entry.Username); !ok {
		return
	}
	if err := s.relogin(ctx, entry); err != nil {
		log.Printf("WebClass Cookie 更新失敗: %v", err)
	}
}

func sameCookieValues(a, b []*http.Cookie) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}
