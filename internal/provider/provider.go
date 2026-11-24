package provider

import "sync"

// Provider は1つの教育システムを表す。全プロバイダが実装する最小の識別情報。
type Provider interface {
	ID() string   // "webclass" / "kyomu" など一意な識別子
	Name() string // 表示名（"WebClass" / "教務システム"）
}

// --- 能力インターフェース ---
// 各プロバイダは対応するものだけを実装する。UI/API は型アサーションで有無を判定する。

// Authenticator は資格情報でログインしてセッションを返せるプロバイダ。
type Authenticator interface {
	Login(creds Credentials) (*Session, error)
	// RequiresCampusNetwork はログインや一部機能が学内ネットワーク限定かを示す。
	RequiresCampusNetwork() bool
}

// TimetableSource は認証済みセッションで時間割を取得できるプロバイダ。
type TimetableSource interface {
	Timetable(sess *Session) ([]Course, error)
}

// SyllabusSource はシラバスを検索・取得できるプロバイダ（通常は未ログインで可）。
type SyllabusSource interface {
	SearchSyllabus(query string) ([]Syllabus, error)
}

// GradeSource は認証済みセッションで成績を取得できるプロバイダ（学内限定の場合あり）。
type GradeSource interface {
	Grades(sess *Session) ([]Grade, error)
}

// Registry は ID からプロバイダを引く登録簿。
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	order     []string
}

// NewRegistry は空のレジストリを生成する。
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register はプロバイダを登録する（同じ ID は上書き）。
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[p.ID()]; !exists {
		r.order = append(r.order, p.ID())
	}
	r.providers[p.ID()] = p
}

// Get は ID でプロバイダを返す。
func (r *Registry) Get(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// All は登録順にプロバイダを返す。
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.providers[id])
	}
	return out
}
