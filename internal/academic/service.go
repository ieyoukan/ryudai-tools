// Package academic は教務システム（kyomu プロバイダ）の上に、学生が履修を考えるための
// 機能をまとめたサービス層。Web UI・JSON API・MCP サーバーから共通で使う。
//
//   - 公開シラバスの検索・詳細（キャッシュ・同時接続数制限つき）
//   - ユーザーごとの教務システムセッション管理（ログイン時の Cookie を保存し、.env の本人だけ自動再ログイン）
//   - 履修状況の要約・空きコマの候補講義探し・履修プランのチェック
//
// 教務システムへの書き込み（履修登録の追加・削除）は行わない。
package academic

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
	"github.com/ieyoukan/ryudai-tools/internal/provider/university"
)

// ErrNoCredentials は教務システムのセッションが無く、再ログインもできないことを示す。
var ErrNoCredentials = errors.New("教務システムのセッションが切れています。Ryudai Tools に再ログインしてください")

// PasswordFunc はユーザーの平文パスワードを返す。自動再ログインできないユーザーは false を返す。
// 公開サーバーはパスワードを保存しないので、.env の本人（WEBCLASS_USERNAME）だけが対象になる。
type PasswordFunc func(username string) (string, bool)

// CookieStore は教務システムのログイン Cookie を永続化する（db.DB が実装）。
type CookieStore interface {
	KyomuCookies(userID string) ([]*http.Cookie, error)
	SaveKyomuCookies(userID string, cookies []*http.Cookie) error
	DeleteKyomuCookies(userID string) error
}

// Service は教務システム関連機能の入口。ゼロ値ではなく New で作ること。
type Service struct {
	baseURL  string
	password PasswordFunc

	// upstream は教務システムへの同時リクエスト数を抑えるセマフォ（公開 API 経由の過負荷防止）。
	upstream chan struct{}

	searchCache   *ttlCache[[]kyomu.SyllabusSummary]
	syllabusCache *ttlCache[*kyomu.Syllabus]
	optionsCache  *ttlCache[*kyomu.SearchOptions]
	candCache     *ttlCache[candidateBatch]
	profileCache  *ttlCache[*kyomu.StudentProfile]
	snapshotCache *ttlCache[*gradeSnapshot]
	scheduleCache *ttlCache[*kyomu.ScheduleWeek]
	lectureCache  *ttlCache[*kyomu.LectureSchedule]
	regStateCache *ttlCache[*kyomu.RegistrationStatus]
	University    *university.Client

	// Plans を設定すると、卒業要件の見通しに履修計画を反映する。
	Plans PlanStore
	// Categories を設定すると、履修登録の期間後も今期の登録科目の区分を使える（期間中に見えた区分・利用者が選んだ区分）。
	Categories CategoryStore

	// Public を設定すると、シラバスなどの公開情報を保存して全員で共有し、再起動後も使う（UsePublicStore で設定）。
	Public university.PublicStore

	// Cookies を設定すると教務システムの Cookie を再起動後も使える。
	// 自動再ログインできないユーザーは、この Cookie が切れるまで教務システムの機能を使える。
	Cookies CookieStore

	// PeriodStarts は時限 → 開始時刻（"HH:MM"）。休み時間の計算に使う。
	PeriodStarts map[int]string

	mu    sync.Mutex
	users map[string]*userSession
}

type userSession struct {
	scheduleMu sync.Mutex // 週キャッシュの再確認と一括取得を直列化する
	mu         sync.Mutex // 同一ユーザーの教務システムアクセスを直列化する
	cookies    []*http.Cookie
}

// New は Service を生成する。password が nil なら自動再ログインはしない。
func New(baseURL string, password PasswordFunc) *Service {
	if baseURL == "" {
		baseURL = kyomu.DefaultBaseURL
	}
	return &Service{
		baseURL:       baseURL,
		password:      password,
		upstream:      make(chan struct{}, 4),
		searchCache:   newTTLCache[[]kyomu.SyllabusSummary](10*time.Minute, 500),
		syllabusCache: newTTLCache[*kyomu.Syllabus](6*time.Hour, 3000),
		optionsCache:  newTTLCache[*kyomu.SearchOptions](24*time.Hour, 100),
		candCache:     newTTLCache[candidateBatch](5*time.Minute, 2000),
		profileCache:  newTTLCache[*kyomu.StudentProfile](24*time.Hour, 2000),
		snapshotCache: newTTLCache[*gradeSnapshot](3*time.Minute, 500),
		scheduleCache: newTTLCache[*kyomu.ScheduleWeek](5*time.Minute, 2000),
		lectureCache:  newTTLCache[*kyomu.LectureSchedule](10*time.Minute, 5000),
		regStateCache: newTTLCache[*kyomu.RegistrationStatus](5*time.Minute, 2000),
		University:    university.New(),
		PeriodStarts: map[int]string{
			1: "08:30", 2: "10:20", 3: "12:50", 4: "14:40", 5: "16:20", 6: "18:00", 7: "19:40", 8: "21:10",
		},
		users: make(map[string]*userSession),
	}
}

// BaseURL は教務システムのベース URL を返す。
func (s *Service) BaseURL() string { return s.baseURL }

func (s *Service) acquire() func() {
	s.upstream <- struct{}{}
	return func() { <-s.upstream }
}

func (s *Service) userSession(username string) *userSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	us := s.users[username]
	if us == nil {
		us = &userSession{}
		s.users[username] = us
	}
	return us
}

// StartSession はログイン画面で受け取ったパスワードで教務システムにログインし、Cookie を保存する。
// パスワードは保存しない。
func (s *Service) StartSession(username, password string) error {
	us := s.userSession(username)
	us.mu.Lock()
	defer us.mu.Unlock()
	release := s.acquire()
	defer release()

	c, err := kyomu.New(s.baseURL)
	if err != nil {
		return err
	}
	if err := c.Login(username, password); err != nil {
		return err
	}
	us.cookies = c.Cookies()
	s.scheduleCache.delPrefix(username + "\x00")
	s.lectureCache.delPrefix(username + "\x00")
	s.regStateCache.del(username)
	s.cacheLoginSchedule(username, c)
	s.saveCookies(username, us.cookies)
	return nil
}

// withUser はユーザーの教務システムセッションで fn を実行する。
// メモリに無ければ保存済み Cookie を使い、切れていれば .env の本人だけ再ログインして 1 回やり直す。
func (s *Service) withUser(username string, fn func(c *kyomu.Client) error) error {
	us := s.userSession(username)

	us.mu.Lock()
	defer us.mu.Unlock()
	release := s.acquire()
	defer release()

	for attempt := 0; attempt < 2; attempt++ {
		var c *kyomu.Client
		if us.cookies == nil && attempt == 0 {
			us.cookies = s.storedCookies(username)
		}
		if us.cookies == nil {
			var err error
			c, err = s.login(username, us)
			if err != nil {
				return err
			}
		}
		if c == nil {
			var err error
			c, err = kyomu.NewFromCookies(s.baseURL, us.cookies)
			if err != nil {
				return err
			}
		}
		err := fn(c)
		if errors.Is(err, kyomu.ErrSessionExpired) {
			us.cookies = nil
			if s.Cookies != nil {
				s.Cookies.DeleteKyomuCookies(username)
			}
			continue
		}
		us.cookies = c.Cookies()
		s.saveCookies(username, us.cookies)
		return err
	}
	return kyomu.ErrSessionExpired
}

func (s *Service) login(username string, us *userSession) (*kyomu.Client, error) {
	if s.password == nil {
		return nil, ErrNoCredentials
	}
	pass, ok := s.password(username)
	if !ok {
		return nil, ErrNoCredentials
	}
	c, err := kyomu.New(s.baseURL)
	if err != nil {
		return nil, err
	}
	if err := c.Login(username, pass); err != nil {
		return nil, err
	}
	us.cookies = c.Cookies()
	s.cacheLoginSchedule(username, c)
	s.saveCookies(username, us.cookies)
	return c, nil
}

func (s *Service) storedCookies(username string) []*http.Cookie {
	if s.Cookies == nil {
		return nil
	}
	cookies, err := s.Cookies.KyomuCookies(username)
	if err != nil || len(cookies) == 0 {
		return nil
	}
	return cookies
}

func (s *Service) saveCookies(username string, cookies []*http.Cookie) {
	if s.Cookies == nil || len(cookies) == 0 {
		return
	}
	if err := s.Cookies.SaveKyomuCookies(username, cookies); err != nil {
		log.Printf("教務システム Cookie の保存失敗 (%s): %v", username, err)
	}
}

// Forget はユーザーの教務システムセッションを破棄する（ログアウト時）。
func (s *Service) Forget(username string) {
	s.mu.Lock()
	delete(s.users, username)
	s.mu.Unlock()
	s.snapshotCache.del(strings.ToLower(username))
	s.scheduleCache.delPrefix(username + "\x00")
	s.lectureCache.delPrefix(username + "\x00")
	s.regStateCache.del(username)
	if s.Cookies != nil {
		s.Cookies.DeleteKyomuCookies(username)
	}
}

// planItems はユーザーの履修計画を返す。保存先が無い・読めないときは計画なしとして扱う。
func (s *Service) planItems(username string) []db.PlanItem {
	if s.Plans == nil {
		return nil
	}
	items, err := s.Plans.PlanItems(username)
	if err != nil {
		log.Printf("履修計画の読み取り失敗 (%s): %v", username, err)
		return nil
	}
	return items
}

// UsePublicStore は公開情報（シラバス・検索の選択肢・大学の公式資料）の保存先を設定する。
func (s *Service) UsePublicStore(store university.PublicStore) {
	s.Public = store
	if s.University != nil {
		s.University.Store = store
	}
}
