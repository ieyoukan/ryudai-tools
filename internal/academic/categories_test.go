package academic

import (
	"sort"
	"strings"
	"testing"

	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/provider/kyomu"
)

type memCategories map[string]db.LectureCategory

func (m memCategories) LectureCategories(string) (map[string]db.LectureCategory, error) {
	return m, nil
}
func (m memCategories) SaveLectureCategory(_ string, c db.LectureCategory) error {
	if old, ok := m[c.LectureCode]; ok && old.Source == "user" && c.Source != "user" {
		return nil
	}
	m[c.LectureCode] = c
	return nil
}

func TestSeriesStem(t *testing.T) {
	for title, want := range map[string]string{
		"試験技術演習II": "試験技術演習", "試験開発演習Ⅲ": "試験開発演習", "セミナーⅠ 5組": "セミナー",
		"英語講読演習2": "英語講読演習", "試験機械学習": "", "AI": "",
	} {
		if got := seriesStem(title); got != want {
			t.Errorf("seriesStem(%q) = %q, want %q", title, got, want)
		}
	}
}

// 履修登録の期間後（区分が画面に出ない）の今期の登録に、成績・保存済みの区分から区分を補う。
func TestFillLectureCategoriesAfterRegistrationCloses(t *testing.T) {
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{
		{Title: "試験技術演習I", Category: "専門教育科目（知能情報関連）", Passed: true},
		{Title: "試験開発演習Ⅱ", Category: "専門教育科目（知能情報）", Passed: true},
		{Title: "試験ロボット工学", Category: "専門教育科目（知能情報）", Passed: false}, // 再履修
		{Title: "試験数学Ⅰ", Category: "専門教育科目（数学基礎）", Passed: true},
		{Title: "試験数学Ⅱ", Category: "共通教育科目（専門基礎科目）", Passed: true}, // 同じ番号違いで区分が割れる
	}}
	reg := &kyomu.Registration{Lectures: []kyomu.RegisteredLecture{
		{LectureCode: "990342002", Title: "試験技術演習II", Rank: 1},
		{LectureCode: "990357002", Title: "試験開発演習Ⅲ", Rank: 1},
		{LectureCode: "990334002", Title: "試験ロボット工学", Rank: 1},
		{LectureCode: "990327002", Title: "試験機械学習", Rank: 1},
		{LectureCode: "992314072", Title: "試験デザイン演習 7組", Rank: 1},
		{LectureCode: "999", Title: "試験数学Ⅲ", Rank: 1},
	}}
	store := memCategories{"990327002": {LectureCode: "990327002", Category: "知能情報アドバンスト", Source: "user"}}
	s := &Service{Categories: store}
	s.fillLectureCategories("student", reg, grades)

	want := map[string][2]string{
		"試験技術演習II":    {"専門教育科目（知能情報関連）", "series"},
		"試験開発演習Ⅲ":     {"専門教育科目（知能情報）", "series"},
		"試験ロボット工学":    {"専門教育科目（知能情報）", "grades"},
		"試験機械学習":      {"知能情報アドバンスト", "saved"},
		"試験デザイン演習 7組": {"", ""},
		"試験数学Ⅲ":       {"", ""}, // 候補が 2 つあるので推定しない
	}
	for _, l := range reg.Lectures {
		if got := [2]string{l.Category, l.CategorySource}; got != want[l.Title] {
			t.Errorf("%s: got %v, want %v", l.Title, got, want[l.Title])
		}
	}
}

func TestFillLectureCategoriesSavesWhileRegistrationIsOpen(t *testing.T) {
	store := memCategories{}
	s := &Service{Categories: store}
	reg := &kyomu.Registration{Open: true, Lectures: []kyomu.RegisteredLecture{
		{LectureCode: "990342002", Title: "試験技術演習II", Category: "[専門教育科目（知能情報関連）]", Rank: 1}}}
	s.fillLectureCategories("student", reg, nil)
	if store["990342002"].Category != "[専門教育科目（知能情報関連）]" || store["990342002"].Source != "kyomu" {
		t.Fatalf("saved = %+v", store)
	}
}

func TestCategoryAliasesAndProjection(t *testing.T) {
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{
		{Title: "試験画像処理", Category: "専門教育科目（知能情報）", Credits: 2, Passed: true},
		{Title: "試験データ分析", Category: "専門教育科目（知能情報）", Credits: 2, Passed: true},
		{Title: "試験計算機システム", Category: "専門教育科目（知能情報コア）", Credits: 2, Passed: true},
		{Title: "試験情報総合実験", Category: "専門教育科目（研究・実験）", Credits: 4, Passed: true},
	}}
	reqs := []kyomu.Requirement{
		{Section: "専門教育", Name: "知能情報コア", Kind: "item", Required: fp(26), Earned: fp(2)},
		{Section: "専門教育", Name: "知能情報アドバンスト", Kind: "item", Earned: fp(4)},
		{Section: "専門教育", Name: "知能情報関連", Kind: "item", Earned: fp(5)},
		{Section: "専門教育", Name: "研究実験", Kind: "item", Required: fp(16), Earned: fp(4), Shortfall: fp(12)},
	}
	aliases := categoryAliases(grades, reqs)
	if aliases["知能情報"] != "知能情報アドバンスト" || len(aliases) != 1 {
		t.Fatalf("aliases = %v", aliases)
	}

	reg := &kyomu.Registration{Lectures: []kyomu.RegisteredLecture{
		{Title: "試験開発演習Ⅲ", Credits: 2, Rank: 1, Category: "専門教育科目（知能情報）", CategorySource: "series", CategoryBasis: "「試験開発演習Ⅱ」の成績"},
		{Title: "試験特別実験", Credits: 4, Rank: 1, Category: "専門教育科目（研究・実験）"},
	}}
	tp := projectTerm(reg, reqs, []kyomu.Requirement{reqs[3]}, aliases)
	if len(tp.Unmapped) != 0 || len(tp.Inferred) != 1 || tp.Inferred[0].Source != "series" {
		t.Fatalf("term plan = %+v", tp)
	}
	if len(tp.Projection) != 1 || tp.Projection[0].RegisteredNow != 4 || tp.Projection[0].ShortfallAfter != 8 {
		t.Fatalf("projection = %+v", tp.Projection)
	}
}

// 区分が分からない科目の候補は、同じ開講部署で講義コードが近い成績の科目の区分にする。
func TestSuggestedCategoryFromNearestLectureCode(t *testing.T) {
	grades := &kyomu.GradeList{Grades: []kyomu.Grade{
		{LectureCode: "990314002", Title: "試験開発演習I", Category: "専門教育科目（知能情報コア）"},
		{LectureCode: "990313002", Title: "試験人工知能", Category: "専門教育科目（知能情報コア）"},
		{LectureCode: "990312002", Title: "試験データベース論", Category: "専門教育科目（知能情報コア）"},
		{LectureCode: "990333002", Title: "試験セキュリティ", Category: "専門教育科目（知能情報）"},
		{LectureCode: "990356001", Title: "試験開発演習Ⅱ", Category: "専門教育科目（知能情報）"},
		{LectureCode: "991347001", Title: "試験電気機器", Category: "選択科目（工学融合科目）"},
	}}
	reg := &kyomu.Registration{Lectures: []kyomu.RegisteredLecture{
		{LectureCode: "990357002", Title: "試験開発演習Ⅲ", Rank: 1},
		{LectureCode: "990334002", Title: "試験ロボット工学", Rank: 1},
		{LectureCode: "991348002", Title: "試験発電工学", Rank: 1},
		{LectureCode: "993303002", Title: "試験論理入門", Rank: 1}, // 同じ開講部署の成績が無い
	}}
	(&Service{}).fillLectureCategories("student", reg, grades)
	want := map[string]string{
		"試験開発演習Ⅲ":  "専門教育科目（知能情報）", // 区分の多い「知能情報コア」ではなく、講義コードが隣の同Ⅱ
		"試験ロボット工学": "専門教育科目（知能情報）",
		"試験発電工学":   "選択科目（工学融合科目）",
		"試験論理入門":   "",
	}
	for _, l := range reg.Lectures {
		if l.Category != "" || l.SuggestedCategory != want[l.Title] {
			t.Errorf("%s: category %q, suggested %q, want suggested %q", l.Title, l.Category, l.SuggestedCategory, want[l.Title])
		}
	}
}

// 個別成績表の小計の構成を、修得単位の和から読み取る（知能情報コースの様式）。
func TestSubtotalMembers(t *testing.T) {
	item := func(sec, name string, earned *float64) kyomu.Requirement {
		return kyomu.Requirement{Section: sec, Name: name, Kind: "item", Earned: earned}
	}
	sub := func(sec, name string, required, earned float64) kyomu.Requirement {
		return kyomu.Requirement{Section: sec, Name: name, Kind: "subtotal", Required: f64(required), Earned: f64(earned)}
	}
	reqs := []kyomu.Requirement{
		item("共通教育", "健康運動", f64(2)), item("共通教育", "人文", f64(4)), item("共通教育", "社会", f64(2)),
		item("共通教育", "総合", f64(4)), item("共通教育", "キャリア関係", nil), item("共通教育", "琉大特色・地域創生", nil),
		sub("共通教育", "（総合領域計）", 2, 4), sub("共通教育", "小計１", 12, 10),
		item("共通教育", "自然", nil), item("共通教育", "共通教育科目（日本事情）", nil), sub("共通教育", "小計２", 14, 10),
		{Section: "共通教育", Name: "共通計", Kind: "total", Required: f64(30), Earned: f64(25)},
		item("専門教育", "知能情報コア", f64(24)), item("専門教育", "工学融合（選択）", f64(2)), item("専門教育", "数学基礎（選択）", nil),
		item("専門教育", "知能情報アドバンスト", f64(14)), item("専門教育", "知能情報関連", f64(5)), sub("専門教育", "（数情計）", 22, 19),
		item("専門教育", "自由", nil), item("専門教育", "教職", nil), sub("専門教育", "（数情等計）", 37, 21),
		sub("専門教育", "（空の小計）", 2, 0),
	}
	got := subtotalMembers(reqs)
	names := func(key string) []string {
		var out []string
		for _, m := range got[key] {
			out = append(out, strings.SplitN(m, "\x00", 2)[1])
		}
		sort.Strings(out)
		return out
	}
	want := map[string][]string{
		"共通教育\x00（総合領域計）": {"キャリア関係", "琉大特色・地域創生", "総合"},
		"共通教育\x00小計１":     {"キャリア関係", "人文", "琉大特色・地域創生", "社会", "総合"},
		"共通教育\x00小計２":     {"キャリア関係", "人文", "共通教育科目（日本事情）", "琉大特色・地域創生", "社会", "総合", "自然"},
		"専門教育\x00（数情計）":   {"知能情報アドバンスト", "知能情報関連"},
		"専門教育\x00（数情等計）":  {"工学融合（選択）", "教職", "数学基礎（選択）", "知能情報アドバンスト", "知能情報関連", "自由"},
	}
	for key, w := range want {
		sort.Strings(w)
		if g := names(key); strings.Join(g, ",") != strings.Join(w, ",") {
			t.Errorf("%q = %v, want %v", key, g, w)
		}
	}
	if _, ok := got["専門教育\x00（空の小計）"]; ok {
		t.Error("修得単位 0 の小計は構成を決めない")
	}

	// 今期の登録が小計にも入り、到達度グラフ・見通しに出る
	reg := &kyomu.Registration{Lectures: []kyomu.RegisteredLecture{
		{Rank: 1, Title: "試験開発演習Ⅲ", Credits: 2, Category: "専門教育科目（知能情報アドバンスト）"},
		{Rank: 1, Title: "試験技術演習II", Credits: 2, Category: "専門教育科目（知能情報関連）"},
		{Rank: 1, Title: "試験発電工学", Credits: 2, Category: "選択科目（工学融合科目）"},
	}}
	tp := projectTerm(reg, reqs, []kyomu.Requirement{reqs[20]}, nil)
	if v := tp.RegisteredFor("専門教育", "（数情等計）"); v != 6 {
		t.Errorf("（数情等計）の今期の登録 = %v, want 6", v)
	}
	if v := tp.RegisteredFor("専門教育", "知能情報アドバンスト"); v != 2 {
		t.Errorf("知能情報アドバンストの今期の登録 = %v, want 2（不足の無い区分も数える）", v)
	}
	if len(tp.Projection) != 1 || tp.Projection[0].RegisteredNow != 6 || tp.Projection[0].ShortfallAfter != 10 {
		t.Errorf("projection = %+v", tp.Projection)
	}
}
