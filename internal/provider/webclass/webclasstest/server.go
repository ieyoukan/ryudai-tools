// Package webclasstest は、テストと手元の開発で使う偽の WebClass。
//
// 本物の WebClass と同じく、ログインすると WCAC と WBT_Session の Cookie を返し、セッションごとに
// 「今いるコース」を 1 つだけ持つ（course.php/{id}/login で切り替わり、別のコースに入ると前のコースの
// ページは出せなくなる）。セッションが無効なら login.php への転送ページを返す。
// 実データは含めず、コースや資料は呼び出し側が架空の値で用意する。
//
// 対応している画面: ログイン、トップページの時間割、コースのページ（資料の一覧）、出席確認の一覧（空）、
// 受信箱（空）、未読数。資料のファイルの取得（do_contents.php 以降）は対応していない。
package webclasstest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Course は偽の WebClass のコース。
type Course struct {
	ID        string
	Name      string // 例: 試験工学[2026後期][試験001]
	Day       string // 月〜土
	Period    int
	Materials []Material
	// Other は資料以外のコンテンツ（試験・レポートなど。一覧には出ても資料としては返らない）
	Other []Material
}

// Material はコースのコンテンツ 1 件。Category は「資料」「試験」など WebClass の表示どおり。
type Material struct {
	ID       string
	Name     string
	Category string
}

// Server は偽の WebClass。Handler を httptest.Server や http.Server で配信する。
type Server struct {
	// Users は ID → パスワード。
	Users map[string]string
	// Courses はすべての利用者の時間割に載るコース。
	Courses []Course
	// Delay はコースに入るときの待ち時間。並行してコースを開いたときの取り違えを再現しやすくする。
	Delay time.Duration

	mu       sync.Mutex
	sessions map[string]*sessionState
	logins   int
	hits     map[string]int
}

type sessionState struct {
	user    string
	current string // 今いるコース
}

// New は利用者とコースを指定して偽の WebClass を作る。
func New(users map[string]string, courses ...Course) *Server {
	return &Server{Users: users, Courses: courses, sessions: map[string]*sessionState{}, hits: map[string]int{}}
}

// Start は httptest.Server で配信する。URL は WebClass のベース URL として使える。
func (s *Server) Start() *httptest.Server {
	return httptest.NewServer(s.Handler())
}

// Logins はログインに成功した回数（再ログインの検証用）。
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

// Hits はページを返した回数（"top": 時間割、"course": コースのページ、"attendance": 出席確認）。
// キャッシュが効いて WebClass への取得が減っているかの検証に使う。
func (s *Server) Hits(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[kind]
}

func (s *Server) hit(kind string) {
	s.mu.Lock()
	s.hits[kind]++
	s.mu.Unlock()
}

// ExpireSessions はすべてのセッションを無効にする（WebClass 側のセッション切れを再現する）。
func (s *Server) ExpireSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]*sessionState{}
}

// Handler は偽の WebClass の HTTP ハンドラ。どのパスの下に置いても動くよう、パスの末尾で判定する。
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	path := r.URL.Path
	if strings.HasSuffix(path, "/login.php") {
		s.login(w, r)
		return
	}
	st := s.session(r)
	if st == nil {
		fmt.Fprint(w, `<html><head><title>PageJump - WebClass</title></head><script>window.top.location.href="/webclass/login.php";</script></html>`)
		return
	}
	switch {
	case strings.Contains(path, "/mbl.php/messages/api/count"):
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"count":0}`)
	case strings.HasSuffix(path, "/msg_editor.php"):
		fmt.Fprint(w, `<html><head><title>メッセージ - WebClass</title></head><body><table class="table"></table></body></html>`)
	case strings.Contains(path, "/course.php/"):
		s.course(w, path, st)
	default:
		s.top(w)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fmt.Fprint(w, `<html><head><title>ログイン - WebClass</title></head><body><form method="post"><input type="hidden" name="token" value="fake-token"><input name="username"><input type="password" name="val"></form></body></html>`)
		return
	}
	r.ParseForm()
	user, pass := r.PostForm.Get("username"), r.PostForm.Get("val")
	if want, ok := s.Users[user]; !ok || want != pass {
		fmt.Fprint(w, `<html><head><title>ログイン - WebClass</title></head><body><p>ユーザ名またはパスワードが違います</p><input name="username"><input type="password" name="val"></body></html>`)
		return
	}
	b := make([]byte, 12)
	rand.Read(b)
	id := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[id] = &sessionState{user: user}
	s.logins++
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "WCAC", Value: "Authenticated", Path: "/", Expires: time.Now().Add(7 * 24 * time.Hour)})
	http.SetCookie(w, &http.Cookie{Name: "WBT_Session", Value: id, Path: "/"})
	fmt.Fprint(w, `<html><head><title>WebClass</title></head><body>ログインしました</body></html>`)
}

func (s *Server) session(r *http.Request) *sessionState {
	c, err := r.Cookie("WBT_Session")
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[c.Value]
}

func (s *Server) top(w http.ResponseWriter) {
	s.hit("top")
	var b strings.Builder
	b.WriteString(`<html><head><title>WebClass</title></head><body><table class="schedule-table"><tr><th></th>`)
	days := []string{"月", "火", "水", "木", "金", "土"}
	for _, d := range days {
		fmt.Fprintf(&b, "<th>%s</th>", d)
	}
	b.WriteString("</tr>")
	for period := 1; period <= 6; period++ {
		fmt.Fprintf(&b, "<tr><th>%d限</th>", period)
		for _, d := range days {
			b.WriteString("<td>")
			for _, c := range s.Courses {
				if c.Day == d && c.Period == period {
					fmt.Fprintf(&b, `<a href="/webclass/course.php/%s/login?acs_=fa11ce">» %s</a>`, html.EscapeString(c.ID), html.EscapeString(c.Name))
				}
			}
			b.WriteString("</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table></body></html>")
	fmt.Fprint(w, b.String())
}

// course はコースのページ。course.php/{id}/login で「今いるコース」を切り替え、少し待ってから
// その時点で今いるコースのページを返す（並行して別のコースに入られると、本物と同じく中身が入れ替わる）。
func (s *Server) course(w http.ResponseWriter, path string, st *sessionState) {
	rest := path[strings.Index(path, "/course.php/")+len("/course.php/"):]
	id, action, _ := strings.Cut(rest, "/")
	if action == "login" {
		s.mu.Lock()
		st.current = id
		s.mu.Unlock()
		time.Sleep(s.Delay)
	}
	s.mu.Lock()
	current := st.current
	s.mu.Unlock()
	if action == "attendance" {
		s.hit("attendance")
		fmt.Fprintf(w, `<html><head><title>出席 - WebClass</title></head><body><a href="/webclass/course.php/%s/login">戻る</a></body></html>`, html.EscapeString(current))
		return
	}
	s.hit("course")
	course, ok := s.find(current)
	if !ok {
		fmt.Fprint(w, `<html><head><title>エラー - WebClass</title></head><body>コースが見つかりません</body></html>`)
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<html><head><title>%s - WebClass</title></head><body><div id="courseLearningLayoutModal"></div><a href="/webclass/course.php/%s/login?acs_=fa11ce">%s</a>`,
		html.EscapeString(course.Name), html.EscapeString(course.ID), html.EscapeString(course.Name))
	for _, m := range append(append([]Material(nil), course.Materials...), course.Other...) {
		category := m.Category
		if category == "" {
			category = "資料"
		}
		fmt.Fprintf(&b, `<section data-contents-id="%s" data-contents-name="%s"><div class="cl-contentsList_categoryLabel">%s</div></section>`,
			html.EscapeString(m.ID), html.EscapeString(m.Name), html.EscapeString(category))
	}
	b.WriteString("</body></html>")
	fmt.Fprint(w, b.String())
}

func (s *Server) find(id string) (Course, bool) {
	for _, c := range s.Courses {
		if c.ID == id {
			return c, true
		}
	}
	return Course{}, false
}
