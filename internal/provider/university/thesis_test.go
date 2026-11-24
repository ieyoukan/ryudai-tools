package university

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// R6 便覧の各コースの注記（PDF からの抽出結果に近い改行・空白入り）
func TestThesisRuleFromPassage(t *testing.T) {
	chinot := `※7 1つのコースの工学融合科目から4単位以上を履修する。
※8 4年次(6個学期在学後)または5個学期在学後の4月の時点で卒業研究Iを登録するためには，次の2項目の条件を満たしていなければならない。
 （１）6個学期在学の学生については，取得単位数が100以上であること。また，5個学期在学の学生については取得単位数が85以上であること。
 （２）原則とし3年後学期までの専門必修科目の全ての単位を取得していること。
※9 専門科目における科目分類`
	rule := thesisRuleFromPassage(Passage{Document: "知能情報コース", Page: 11, Text: chinot})
	if rule == nil || rule.CreditsBySemesters[6] != 100 || rule.CreditsBySemesters[5] != 85 || !rule.RequiresMajorRequired ||
		rule.MaxRemainingCredits != 0 || rule.MaxMissingMajorRequired != nil || len(rule.RequiredCourses) != 0 {
		t.Fatalf("知能情報 rule = %+v", rule)
	}
	ev := rule.Evidence.Text
	if !utf8.ValidString(ev) || !strings.HasPrefix(ev, "※8") || !strings.HasSuffix(ev, "取得していること。") || strings.Contains(ev, "※9") {
		t.Fatalf("evidence = %q", ev)
	}

	shakai := `※9 卒業研究を履修するためには，以下の条件をすべて満たす必要がある。
・共通教育，専門基礎教育，専門教育の区別なく，卒業要件を満たすために今後修得しなければならない単位数が，卒業研究を含めて合計20単位以下であること。
・卒業研究Ⅰ・Ⅱ以外の専門必修の未修得科目数が4科目以下であること。
・社会基盤デザイン実験の単位を修得していること。
・鋼構造設計演習，もしくは，コンクリート構造設計演習の単位を修得していること。`
	rule = thesisRuleFromPassage(Passage{Document: "社会基盤デザインコース", Page: 11, Text: shakai})
	if rule == nil || rule.MaxRemainingCredits != 20 || rule.MaxMissingMajorRequired == nil || *rule.MaxMissingMajorRequired != 4 ||
		len(rule.RequiredCourses) != 2 || rule.RequiredCourses[0][0] != "社会基盤デザイン実験" ||
		strings.Join(rule.RequiredCourses[1], "/") != "鋼構造設計演習/コンクリート構造設計演習" || len(rule.CreditsBySemesters) != 0 {
		t.Fatalf("社会基盤デザイン rule = %+v", rule)
	}

	kenchiku := `※8 卒業設計又は卒業研究を履修登録するためには，卒業要件を満たすために今後取得しなければならない単位数が，卒業設計又は卒業研究を含めて合計24単位以下であること。`
	rule = thesisRuleFromPassage(Passage{Document: "建築学コース", Page: 11, Text: kenchiku})
	if rule == nil || rule.MaxRemainingCredits != 24 || !rule.HasConditions() {
		t.Fatalf("建築学 rule = %+v", rule)
	}
}

func TestThesisRuleAbsentOrUnparsed(t *testing.T) {
	// 卒業研究の登録条件の注記が無い（機械工学コースなど）
	if rule := thesisRuleFromPassage(Passage{Text: "※8 エネルギー環境工学コースを除く１つのコースの工学融合科目から4単位以上を履修する。"}); rule != nil {
		t.Fatalf("rule = %+v", rule)
	}
	// 注記はあるが、読み取れる形の条件が無い
	rule := thesisRuleFromPassage(Passage{Text: "※5 卒業研究を履修するためには，指導教員の許可を得なければならない。"})
	if rule == nil || rule.HasConditions() || !strings.Contains(rule.Evidence.Text, "指導教員の許可") {
		t.Fatalf("rule = %+v", rule)
	}
}

// 閉じていない配列を含むページで、PDF ライブラリがメモリを使い切らずに止まること（third_party/ledongthuc-pdf の修正）。
func TestPDFPagesSurvivesUnterminatedArray(t *testing.T) {
	content := "BT /F1 12 Tf [ (abc) (def"
	obj := func(n int, body string) string { return fmt.Sprintf("%d 0 obj\n%s\nendobj\n", n, body) }
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := []int{}
	for i, body := range []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	} {
		offsets = append(offsets, b.Len())
		b.WriteString(obj(i+1, body))
	}
	xref := b.Len()
	b.WriteString("xref\n0 5\n0000000000 65535 f \n")
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)

	done := make(chan struct{})
	go func() {
		defer close(done)
		pages, err := pdfPages([]byte(b.String()))
		if err == nil && len(pages) != 1 {
			t.Errorf("pages = %d", len(pages))
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pdfPages did not finish (unterminated array loops forever)")
	}
}

func TestThesisMentionWithoutConditions(t *testing.T) {
	// 理学部物理系（R6）の科目表: 条件の中身は書かれていない
	text := "物物401卒業研究Ⅰ40－84年前期卒研着手条件をみたすこと究を行う特定のテーマについて専門的な研"
	rule := thesisRuleFromPassage(Passage{Document: "物理系", Page: 3, Text: text})
	if rule == nil || rule.HasConditions() || !strings.Contains(rule.Evidence.Text, "卒研着手条件をみたすこと") || !utf8.ValidString(rule.Evidence.Text) {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestAffiliationKey(t *testing.T) {
	for _, c := range []struct{ course, dept, want, title string }{
		{"知能情報コース", "工学科", "知能情報", "知能情報コース"},
		{"食品機能科学コース", "亜熱帯生物資源科学科", "食品機能科学", "亜熱帯生物資源科学科（生物機能開発学、食品機能科学、発酵・生命科学コース）"},
		{"", "国際法政学科", "国際法政", "国際法政学科"},
		{"経済学プログラム", "国際地域創造学部", "経済学", "経済学プログラム"},
	} {
		got := AffiliationKey(c.course, c.dept)
		if got != c.want || !CourseMatches(c.title, got) {
			t.Errorf("AffiliationKey(%q, %q) = %q (matches %q: %v)", c.course, c.dept, got, c.title, CourseMatches(c.title, got))
		}
	}
	if AffiliationKey("", "") != "" {
		t.Error("empty affiliation")
	}
}
