package kyomu

// 実際の教務システムに接続する結合テスト。通常の go test では skip される。
//
//	KYOMU_LIVE=1 go test ./internal/provider/kyomu/ -run Live -v
//
// ログインが必要なテストは KYOMU_USERNAME / KYOMU_PASSWORD（無ければ WEBCLASS_*、
// 環境変数に無ければリポジトリ直下の .env）を使う。いずれも読み取りのみで、
// 履修登録の追加・削除は行わない。

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("KYOMU_LIVE") == "" {
		t.Skip("KYOMU_LIVE=1 のときだけ実サーバに接続する")
	}
	c, err := New(liveEnv("KYOMU_BASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func liveLogin(t *testing.T) *Client {
	t.Helper()
	c := liveClient(t)
	user := firstNonEmpty(liveEnv("KYOMU_USERNAME"), liveEnv("WEBCLASS_USERNAME"))
	pass := firstNonEmpty(liveEnv("KYOMU_PASSWORD"), liveEnv("WEBCLASS_PASSWORD"))
	if user == "" || pass == "" {
		t.Skip("認証情報が無いのでログインが必要なテストは skip")
	}
	if err := c.Login(user, pass); err != nil {
		t.Fatalf("ログイン失敗: %v", err)
	}
	return c
}

func TestLiveSyllabusSearch(t *testing.T) {
	c := liveClient(t)
	res, err := c.SearchSyllabus(SearchParams{Subject: "プログラミング"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("「プログラミング」で 1 件も見つからない")
	}
	for _, r := range res {
		if r.LectureCode == "" || r.Title == "" || r.Year == 0 {
			t.Errorf("欠けた行: %+v", r)
		}
	}
	t.Logf("%d 件", len(res))
}

func TestLiveSyllabusSearchCascade(t *testing.T) {
	c := liveClient(t)
	res, err := c.SearchSyllabus(SearchParams{Faculty: "工学部", Department: "工学科", Term: "後期", Day: "火", Period: "5"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("工学部工学科 後期 火5 が 0 件")
	}
	for _, r := range res {
		if !strings.Contains(r.DayPeriod, "火") {
			t.Errorf("曜日が絞り込まれていない: %+v", r)
		}
	}
}

func TestLiveSyllabusDetail(t *testing.T) {
	c := liveClient(t)
	res, err := c.SearchSyllabus(SearchParams{Subject: "プログラミング"})
	if err != nil || len(res) == 0 {
		t.Fatalf("検索失敗: %v", err)
	}
	s, err := c.FetchSyllabus(res[0].Year, res[0].LectureCode)
	if err != nil {
		t.Fatal(err)
	}
	if s.LectureCode != res[0].LectureCode || s.Title == "" || s.Credits <= 0 {
		t.Errorf("詳細が不完全: code=%s title=%q credits=%v", s.LectureCode, s.Title, s.Credits)
	}
	if _, err := c.FetchSyllabus(res[0].Year, "000000000"); err != ErrSyllabusNotFound {
		t.Errorf("存在しないコードで err = %v", err)
	}
}

func TestLiveOptions(t *testing.T) {
	o, err := liveClient(t).SyllabusOptions("工学部")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Faculties) == 0 || len(o.Departments) == 0 || len(o.Years) == 0 {
		t.Errorf("選択肢が空: %+v", o)
	}
}

func TestLiveLoginFailure(t *testing.T) {
	c := liveClient(t)
	if err := c.Login("e000000", "wrong-password"); err != ErrLoginFailed {
		t.Errorf("err = %v, want ErrLoginFailed", err)
	}
}

func TestLiveRegistration(t *testing.T) {
	c := liveLogin(t)
	status, err := c.FetchRegistrationStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.State == "unknown" || status.Source == "" || status.FetchedAt.IsZero() {
		t.Fatalf("registration period could not be determined: %+v", status)
	}
	t.Logf("registration period: %s", status.State)
	r, err := c.FetchRegistration()
	if err != nil {
		t.Fatal(err)
	}
	if r.YearTerm == "" {
		t.Error("年度学期が取れていない")
	}
	if r.Credits == nil {
		t.Error("単位数の集計が取れていない")
	} else {
		var sum float64
		for _, l := range r.Lectures {
			if l.Rank == 1 {
				sum += l.Credits
			}
		}
		t.Logf("%s open=%v 合計=%v（講義から算出 %v）", r.YearTerm, r.Open, r.Credits.TotalCredits, sum)
	}
}

func TestLiveCandidates(t *testing.T) {
	c := liveLogin(t)
	for _, scope := range []CandidateScope{ScopeGeneral, ScopeOwn, ScopeOther} {
		cands, err := c.FetchCandidates("月", 2, scope)
		if err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		t.Logf("月2 %s: %d 件", scope, len(cands))
	}
}

func TestLiveSessionExpired(t *testing.T) {
	c := liveClient(t)
	if _, err := c.FetchRegistration(); err != ErrSessionExpired {
		t.Errorf("未ログインで err = %v, want ErrSessionExpired", err)
	}
}

// liveEnv は環境変数、無ければリポジトリ直下の .env から値を読む。
func liveEnv(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		if f, err := os.Open(filepath.Join(dir, ".env")); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
				if ok && strings.TrimSpace(k) == key {
					return strings.Trim(strings.TrimSpace(v), `"'`)
				}
			}
			return ""
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func TestLiveGrades(t *testing.T) {
	c := liveLogin(t)
	gl, err := c.FetchGrades()
	if err != nil {
		t.Fatal(err)
	}
	if len(gl.Grades) == 0 {
		t.Fatal("成績が 0 件")
	}
	for _, g := range gl.Grades {
		if g.Year == 0 || g.LectureCode == "" || g.Grade == "" || g.Category == "" {
			t.Errorf("欠けた成績: %+v", g)
		}
	}
	t.Logf("成績 %d 件", len(gl.Grades))
}

func TestLiveGradeReport(t *testing.T) {
	c := liveLogin(t)
	rep, err := c.FetchGradeReport()
	if err != nil {
		t.Fatal(err)
	}
	var total *Requirement
	for i, r := range rep.Requirements {
		if r.Kind == "total" && r.Name == "合計" {
			total = &rep.Requirements[i]
		}
	}
	if total == nil || total.Required == nil || total.Earned == nil {
		t.Fatalf("【合計】が読めない: %+v", rep.Requirements)
	}
	if rep.GPA == nil {
		t.Error("GPA が読めない")
	}
	t.Logf("区分 %d 行、合計 %v / %v", len(rep.Requirements), *total.Earned, *total.Required)
}
