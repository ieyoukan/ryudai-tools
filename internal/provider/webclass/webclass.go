// Package webclass は WebClass LMS 向けのプロバイダ実装。
// 汎用の HTTP 基盤（webclient）の上に、WebClass 固有のログイン・時間割・教材ファイル・
// 出席（このプロバイダ独自）を実装する。
package webclass

import (
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/ieyoukan/ryudai-tools/internal/provider"
	"github.com/ieyoukan/ryudai-tools/internal/webclient"
)

// providerID / providerName は登録・表示に使う識別子。
const (
	providerID   = "webclass"
	providerName = "WebClass"
)

// CourseInfo は provider.Course の別名（時間割コースは全プロバイダ共通型）。
type CourseInfo = provider.Course

// Client は WebClass の認証済み操作クライアント。汎用 HTTP 基盤を埋め込む。
type Client struct {
	*webclient.Client

	acsMu    sync.Mutex
	acsCache map[string]string // courseID → acs_ トークン
	// materialCache は courseID → 資料のコンテンツ ID（FetchCourseContents で更新）
	materialCache map[string]map[string]bool
}

// New はベース URL を持つ WebClass クライアントを生成する。
func New(baseURL string) (*Client, error) {
	base, err := webclient.New(baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{Client: base, acsCache: make(map[string]string), materialCache: make(map[string]map[string]bool)}, nil
}

// NewFromCookies は既存 cookie で WebClass クライアントを生成する。
func NewFromCookies(baseURL string, cookies []*http.Cookie) (*Client, error) {
	base, err := webclient.NewFromCookies(baseURL, cookies)
	if err != nil {
		return nil, err
	}
	return &Client{Client: base, acsCache: make(map[string]string), materialCache: make(map[string]map[string]bool)}, nil
}

// CourseACSToken はコースページを訪問して acs_ トークンを取得する。
// コースセッション確立と CSRF トークン取得を兼ね、コースごとにキャッシュする。
func (c *Client) CourseACSToken(courseID string) string {
	c.acsMu.Lock()
	defer c.acsMu.Unlock()
	if v, ok := c.acsCache[courseID]; ok {
		return v
	}
	resp, err := c.FollowJSRedirect(fmt.Sprintf("course.php/%s/login", courseID))
	if err != nil {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return ""
	}
	acs := extractACSToken(body)
	if acs != "" {
		c.acsCache[courseID] = acs
	}
	return acs
}

// --- provider インターフェース実装 ---

// Provider は WebClass を provider.Provider として公開するアダプタ。
type Provider struct {
	BaseURL string
}

// NewProvider は WebClass プロバイダを生成する。
func NewProvider(baseURL string) *Provider { return &Provider{BaseURL: baseURL} }

func (p *Provider) ID() string   { return providerID }
func (p *Provider) Name() string { return providerName }

// RequiresCampusNetwork は WebClass が学外からも使えるため false。
func (p *Provider) RequiresCampusNetwork() bool { return false }

// Login は資格情報でログインし、セッション（cookie）を返す。
func (p *Provider) Login(creds provider.Credentials) (*provider.Session, error) {
	c, err := New(p.BaseURL)
	if err != nil {
		return nil, err
	}
	cookies, err := c.Login(creds.Username, creds.Password)
	if err != nil {
		return nil, err
	}
	return &provider.Session{Provider: providerID, Username: creds.Username, Cookies: cookies}, nil
}

// Timetable はセッションから時間割を取得する。
func (p *Provider) Timetable(sess *provider.Session) ([]provider.Course, error) {
	c, err := NewFromCookies(p.BaseURL, sess.Cookies)
	if err != nil {
		return nil, err
	}
	return c.FetchTimetable()
}

// Messages はセッションから受信箱メッセージを取得する。
// excludeSelfID が空でない場合、FromID が一致する自己通知を除外する。
func (p *Provider) Messages(sess *provider.Session, excludeSelfID string) ([]Message, error) {
	c, err := NewFromCookies(p.BaseURL, sess.Cookies)
	if err != nil {
		return nil, err
	}
	return c.FetchInboxMessages(excludeSelfID)
}

// UnreadMessageCount は未読メッセージ数を mbl.php API で取得する。
// notCourse が true の場合、コース自動通知を除外する。
func (p *Provider) UnreadMessageCount(sess *provider.Session, notCourse bool) (int, error) {
	c, err := NewFromCookies(p.BaseURL, sess.Cookies)
	if err != nil {
		return 0, err
	}
	return c.FetchUnreadMessageCount(notCourse)
}

// コンパイル時にインターフェース実装を保証する。
var (
	_ provider.Provider        = (*Provider)(nil)
	_ provider.Authenticator   = (*Provider)(nil)
	_ provider.TimetableSource = (*Provider)(nil)
)
