package kyomu

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	loginPath          = "Login.aspx"
	registListPath     = "StudentApp/Regist/RegistList.aspx"
	registStartPath    = "StudentApp/Regist/RegistEditStart.aspx"
	registCandidateFmt = "StudentApp/Regist/RegistSearchResults.aspx?day=%d&period=%d&label=%d-%d"
)

// RegistrationStatus は登録入口の期間判定。Open（本人の編集可否）とは別に扱う。
type RegistrationStatus struct {
	State     string    `json:"state"` // open / closed / unknown
	Message   string    `json:"message,omitempty"`
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
}

// FetchRegistrationStatus は期間チェック用の入口を1回GETする。登録・確認依頼は送信しない。
func (c *Client) FetchRegistrationStatus() (*RegistrationStatus, error) {
	p, err := c.get(registStartPath)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(p.url), "login.aspx") {
		return nil, ErrSessionExpired
	}
	return registrationStatus(p, c.abs(registStartPath)), nil
}

func registrationStatus(p *page, source string) *RegistrationStatus {
	r := &RegistrationStatus{State: "unknown", Source: source, FetchedAt: time.Now().UTC()}
	r.Message = squash(p.text("ctl00_phContents_ucRegistMessage_lblMessage_lbl"))
	if strings.Contains(r.Message, "履修登録の期間内ではありません") {
		r.State = "closed"
		return r
	}
	// 入口の期間チェックを通過した場合は編集画面か確認依頼後の確認画面へ転送される。
	// 確認依頼済みでも期間中なので、追加・削除ボタンの有無では判定しない。
	u := strings.ToLower(strings.SplitN(p.url, "?", 2)[0])
	if (strings.HasSuffix(u, "/registedit.aspx") || strings.HasSuffix(u, "/registlistconfirm.aspx")) && registrationReadable(p, parseRegistration(p)) {
		r.State = "open"
	}
	for id, n := range p.ids {
		if strings.HasSuffix(id, "_btnInsert") || strings.HasSuffix(id, "_btnDelete") {
			disabled := false
			for _, a := range n.Attr {
				if a.Key == "disabled" {
					disabled = true
				}
			}
			if !disabled {
				r.State = "open"
			}
		}
	}
	return r
}

// Login は ID・パスワードで教務システムにログインする。成功すると Client に cookie が入る。
func (c *Client) Login(username, password string) error {
	p, err := c.get(loginPath)
	if err != nil {
		return err
	}
	btnName, btnValue := p.submitButton("_btnLogin")
	if btnName == "" {
		return fmt.Errorf("ログインボタンが見つかりません")
	}
	p, err = c.submit(p, btnName, btnValue, map[string]string{
		"txtID":       username,
		"txtPassWord": password,
	})
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(p.url), "login.aspx") {
		return ErrLoginFailed
	}
	if p.has(weekGrid) {
		c.loginSchedule = p
		c.loginScheduleAt = time.Now()
	}
	return nil
}

// dayCodes は教務システムの曜日番号（候補検索 URL の day=）。集中等は 8。
var dayCodes = map[string]int{"月": 1, "火": 2, "水": 3, "木": 4, "金": 5, "土": 6, "集中等": 8}

var registDayNames = map[string]string{
	"Mon": "月", "Tue": "火", "Wed": "水", "Thu": "木", "Fri": "金", "Sat": "土", "Other": "集中等",
}

// RegisteredLecture は履修時間割上の 1 講義。
type RegisteredLecture struct {
	Day         string  `json:"day"`    // 月〜土 / 集中等
	Period      int     `json:"period"` // 時限（集中等は枠番号）
	Rank        int     `json:"rank"`   // 同じコマ内の希望順位（1 = 第1希望）
	LectureCode string  `json:"lecture_code"`
	Title       string  `json:"title"`
	Instructors string  `json:"instructors"`
	Credits     float64 `json:"credits"`
	Category    string  `json:"category,omitempty"` // [専門教育科目（知能情報関連）] など（登録画面のみ）
	// CategorySource は Category の出どころ。空なら教務システムの画面、saved / grades / series は academic が補ったもの。
	CategorySource string `json:"category_source,omitempty"`
	CategoryBasis  string `json:"category_basis,omitempty"` // 推定の根拠にした成績の科目名など
	// SuggestedCategory は区分が分からないときの候補（同じ開講部署で講義コードが近い、成績のある科目の区分）。利用者が選ぶときの初期値に使う。
	SuggestedCategory string `json:"suggested_category,omitempty"`
	Required          string `json:"required,omitempty"`   // 必修等の表示
	Term              string `json:"term,omitempty"`       // 前半/後半 など
	Adjustment        string `json:"adjustment,omitempty"` // 登録調整の状況
	LotResult         string `json:"lot_result,omitempty"` // 抽選結果
	Year              int    `json:"year,omitempty"`
}

// CreditSummary は登録単位数の集計（画面上部の表）。
type CreditSummary struct {
	Limit           float64 `json:"limit"`            // 履修上限単位数
	LimitedCredits  float64 `json:"limited_credits"`  // 履修制限対象科目の登録単位数
	ExemptCredits   float64 `json:"exempt_credits"`   // 対象外科目の登録単位数
	TotalCredits    float64 `json:"total_credits"`    // 合計登録単位数
	RemainingCredit float64 `json:"remaining_credit"` // 上限までの残り（Limit - LimitedCredits）
}

// Registration は現在の履修登録状況。
type Registration struct {
	YearTerm string              `json:"year_term"`         // "2026年度後期"
	Open     bool                `json:"open"`              // 追加・削除のボタンがあるか。確認依頼後も false になる。
	Credits  *CreditSummary      `json:"credits,omitempty"` // 取得できた場合のみ
	Lectures []RegisteredLecture `json:"lectures"`
}

// FetchRegistration は履修登録状況を取得する（読み取りのみ）。
// 編集可能なときは登録画面から希望順位・科目区分つきで、確認依頼後は履修確認、期間外は履修時間割表から取得する。
func (c *Client) FetchRegistration() (*Registration, error) {
	if p, err := c.get(registStartPath); err == nil {
		r := parseRegistration(p)
		// 確認依頼後は履修確認（RegistListConfirm）に転送され、履修時間割表（RegistList）と同じ一覧が載っている。
		// 読めていればそのまま使い、RegistList への 2 回目のアクセスを省く。
		if registrationReadable(p, r) {
			return r, nil
		}
		// 直接GETがトップへ戻される場合があるため、入口のフォームから「履修・成績情報」→
		// 「履修時間割表」と通常のメニュー遷移を行う。すでにサブメニューがあればそのまま使う。
		if registrationMenuTarget(p, "履修時間割表") == "" {
			if target := registrationMenuTarget(p, "履修・成績情報"); target != "" {
				p, err = c.postback(p, target, "", nil)
				if err != nil {
					return nil, err
				}
			}
		}
		if target := registrationMenuTarget(p, "履修時間割表"); target != "" {
			p, err = c.postback(p, target, "", nil)
			if err != nil {
				return nil, err
			}
			r = parseRegistration(p)
			if registrationReadable(p, r) {
				r.Open = false
				return r, nil
			}
		}
	} else if err == ErrSessionExpired {
		return nil, err
	}
	p, err := c.get(registListPath)
	if err != nil {
		return nil, err
	}
	r := parseRegistration(p)
	if !registrationReadable(p, r) {
		return nil, fmt.Errorf("履修時間割表を取得できませんでした（教務システムの転送先・画面形式を確認してください）")
	}
	r.Open = false
	return r, nil
}

var menuPostbackRE = regexp.MustCompile(`__doPostBack\('([^']+)'`)

func registrationMenuTarget(p *page, label string) string {
	for _, n := range p.ids {
		if n.Data == "a" && strings.HasPrefix(squash(nodeText(n)), label) {
			if m := menuPostbackRE.FindStringSubmatch(attr(n, "href")); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

func registrationReadable(p *page, r *Registration) bool {
	if r.Open || r.YearTerm != "" || len(r.Lectures) > 0 {
		return true
	}
	for id := range p.ids {
		if strings.HasSuffix(id, "_ttTable_lblYearTerm") {
			return true
		}
	}
	return false
}

var registCellRe = regexp.MustCompile(`^ctl00_phContents_(?:rrMain|reTable)_ttTable_lct(Mon|Tue|Wed|Thu|Fri|Sat|Other)(\d+)_ctl(\d+)_lblLctCd$`)

var yearInTermRe = regexp.MustCompile(`(\d{4})年度`)

func parseRegistration(p *page) *Registration {
	r := &Registration{Lectures: []RegisteredLecture{}}
	year := 0
	for id, n := range p.ids {
		if strings.HasSuffix(id, "_ttTable_lblYearTerm") {
			r.YearTerm = squash(nodeText(n))
			if m := yearInTermRe.FindStringSubmatch(r.YearTerm); m != nil {
				year, _ = strconv.Atoi(m[1])
			}
		}
		if strings.HasSuffix(id, "_btnInsert") || strings.HasSuffix(id, "_btnDelete") {
			r.Open = true
		}
		if strings.HasSuffix(id, "ucRegistScore_gv") {
			r.Credits = parseCreditSummary(n)
		}
	}

	type entry struct {
		lec RegisteredLecture
		ctl int
	}
	cells := map[string][]entry{}
	for id := range p.ids {
		m := registCellRe.FindStringSubmatch(id)
		if m == nil {
			continue
		}
		code := squash(p.text(id))
		if code == "" {
			continue
		}
		base := strings.TrimSuffix(id, "lblLctCd")
		one := func(suffix string) string { return squash(p.text(base + suffix)) }
		period, _ := strconv.Atoi(m[2])
		ctl, _ := strconv.Atoi(m[3])
		lec := RegisteredLecture{
			Day:         registDayNames[m[1]],
			Period:      period,
			LectureCode: code,
			Title:       one("lblSbjName"),
			Instructors: one("lblStaffName"),
			Category:    strings.Trim(one("lblSbjDivName"), "[]"),
			Required:    one("lblReqName"),
			Term:        one("lblTermName"),
			Adjustment:  one("lblAdjustName"),
			LotResult:   one("lblLotResultName"),
			Year:        year,
		}
		lec.Credits, _ = strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(one("lblCredit"), "単位")), 64)
		key := m[1] + m[2]
		cells[key] = append(cells[key], entry{lec, ctl})
	}
	for _, es := range cells {
		sort.Slice(es, func(i, j int) bool { return es[i].ctl < es[j].ctl })
		for i, e := range es {
			e.lec.Rank = i + 1
			r.Lectures = append(r.Lectures, e.lec)
		}
	}
	sort.Slice(r.Lectures, func(i, j int) bool {
		a, b := r.Lectures[i], r.Lectures[j]
		if dayCodes[a.Day] != dayCodes[b.Day] {
			return dayCodes[a.Day] < dayCodes[b.Day]
		}
		if a.Period != b.Period {
			return a.Period < b.Period
		}
		return a.Rank < b.Rank
	})
	return r
}

// parseCreditSummary は「履修上限単位数 / 登録単位数 / 対象外登録単位数 / 合計」の表を読む。
func parseCreditSummary(table *html.Node) *CreditSummary {
	var last []string
	walk(table, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "tr" {
			var tds []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "td" {
					tds = append(tds, squash(nodeText(c)))
				}
			}
			if len(tds) >= 4 {
				last = tds
			}
			return false
		}
		return true
	})
	if last == nil {
		return nil
	}
	num := func(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
	cs := &CreditSummary{
		Limit:          num(last[0]),
		LimitedCredits: num(last[1]),
		ExemptCredits:  num(last[2]),
		TotalCredits:   num(last[3]),
	}
	cs.RemainingCredit = cs.Limit - cs.LimitedCredits
	return cs
}

// Candidate は指定コマに追加できる講義 1 件（登録画面の「追加」で出る一覧）。
type Candidate struct {
	CapacityKnown   bool    `json:"capacity_known"`
	RegisteredKnown bool    `json:"registered_known"`
	Faculty         string  `json:"faculty"`
	LectureCode     string  `json:"lecture_code"`
	Title           string  `json:"title"`
	Instructors     string  `json:"instructors"`
	DayPeriod       string  `json:"day_period"`
	Slots           []Slot  `json:"slots,omitempty"`
	Credits         float64 `json:"credits"`
	Selection       string  `json:"selection,omitempty"` // 選択区分（必修/選択 など）
	Capacity        int     `json:"capacity"`            // 定員（0 は制限なし/不明）
	Registered      int     `json:"registered"`          // 現在の登録者数
	Method          string  `json:"method,omitempty"`    // 登録調整方法（自動登録制/Web教員許可制/抽選 など）
	Note            string  `json:"note,omitempty"`
	Year            int     `json:"year,omitempty"`
	Addable         bool    `json:"addable"` // 「追加」ボタンがあるか
	OverCapacity    bool    `json:"over_capacity"`
}

// CandidateScope は候補一覧のタブ。
type CandidateScope string

const (
	ScopeGeneral CandidateScope = "general" // 共通教育
	ScopeOwn     CandidateScope = "own"     // 自学部
	ScopeOther   CandidateScope = "other"   // 他学部
)

// FetchCandidates は候補ウィンドウを直接GETする（読み取りのみ。登録はしない）。
// 履修登録画面の編集可否によらず、候補画面の応答で照会の成否を判断する。
// day は "月"〜"土" または "集中等"。
func (c *Client) FetchCandidates(day string, period int, scope CandidateScope) ([]Candidate, error) {
	d, ok := dayCodes[strings.TrimSuffix(day, "曜日")]
	if !ok {
		return nil, fmt.Errorf("曜日 %q は指定できません（月〜土・集中等）", day)
	}
	path := fmt.Sprintf(registCandidateFmt, d, period, d, period)
	switch scope {
	case ScopeOwn:
		path += "&fac_mode=1"
	case ScopeOther:
		path += "&fac_mode=3"
	}
	p, err := c.get(path)
	if err != nil {
		return nil, err
	}
	table, ok := p.ids["gv"]
	if !ok {
		return nil, fmt.Errorf("候補一覧の表が見つかりません（教務システムから照会画面が返りませんでした）")
	}
	columns, _ := gridRows(table)
	for _, name := range []string{"講義コード", "科目名", "定員", "登録者数"} {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("候補一覧の表に「%s」列がありません", name)
		}
	}
	return parseCandidates(p), nil
}

func parseCandidates(p *page) []Candidate {
	out := []Candidate{}
	table, ok := p.ids["gv"]
	if !ok {
		return out
	}
	col, rows := gridRows(table)
	for _, r := range rows {
		cell := func(name string) string { return gridCell(col, r, name) }
		code := cell("講義コード")
		if code == "" {
			continue
		}
		cd := Candidate{
			Faculty:     cell("学部名"),
			LectureCode: code,
			Title:       cell("科目名"),
			Instructors: cell("教員名"),
			DayPeriod:   cell("時限"),
			Selection:   cell("選択区分"),
			Method:      cell("登録調整方法"),
			Note:        cell("備考"),
		}
		cd.Slots = ParseSlots(cd.DayPeriod)
		cd.Credits, _ = strconv.ParseFloat(cell("単位数"), 64)
		var capacityErr, registeredErr error
		cd.Capacity, capacityErr = strconv.Atoi(cell("定員"))
		cd.Registered, registeredErr = strconv.Atoi(cell("登録者数"))
		cd.CapacityKnown = capacityErr == nil
		cd.RegisteredKnown = registeredErr == nil
		cd.OverCapacity = cd.CapacityKnown && cd.RegisteredKnown && cd.Capacity > 0 && cd.Registered >= cd.Capacity
		if i, ok := col["科目名"]; ok && i < len(r) {
			if q := linkQuery(r[i]); q != nil {
				cd.Year, _ = strconv.Atoi(q.Get("lct_year"))
			}
		}
		for _, n := range r {
			walk(n, func(x *html.Node) bool {
				if x.Data == "input" && strings.HasSuffix(attr(x, "id"), "_btnAdd") {
					cd.Addable = true
				}
				return true
			})
		}
		out = append(out, cd)
	}
	return out
}
