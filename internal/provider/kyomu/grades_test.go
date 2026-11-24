package kyomu

import (
	"strings"
	"testing"
)

func TestParseGrades(t *testing.T) {
	row := func(cells ...string) string {
		return "<tr><td>" + strings.Join(cells, "</td><td>") + "</td></tr>"
	}
	// 教務の画面と同じく、見出しの前に全幅のタイトル行がある
	body := `<html><body><table id="` + gradesGridID + `"><tr><td colspan="16">成績</td></tr>` +
		row("年度", "評価学期", "講義コード", "科目名", "教員名", "単位数", "評価", "区分", "取得時Grade", "備考") +
		row("［共通教育科目（英語）］", "", "", "［共通教育科目（英語）］", "", "", "", "", "", "") +
		row("2024", "前期", "994701012", "試験英語", "教員A", "２", "Ａ", "", "１年前期", "") +
		row("2025", "後期", "994702012", "試験英語演習", "教員B", "2", "F 不可", "", "２年後期", "遠隔") +
		row("［専門教育科目（研究・実験）］", "", "", "［専門教育科目（研究・実験）］", "", "", "", "", "", "") +
		row("2026", "前期", "992314072", "演習", "教員C", "2", "Ｄ", "", "３年前期", "") +
		`</table><table id="` + gradesDetailID + `"><tr><th>遠隔授業 修得単位数</th><th>他大学等 修得単位数</th></tr><tr><td>6</td><td>0</td></tr></table></body></html>`
	p, _ := parseHTML("x", body)
	gl, err := parseGrades(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(gl.Grades) != 3 {
		t.Fatalf("件数 = %d", len(gl.Grades))
	}
	g := gl.Grades[0]
	if g.Year != 2024 || g.Credits != 2 || g.Grade != "A" || !g.Passed || g.Category != "共通教育科目（英語）" {
		t.Errorf("1 件目 = %+v", g)
	}
	if f := gl.Grades[1]; f.Grade != "F" || f.Passed || !f.Remote {
		t.Errorf("F の行 = %+v", f)
	}
	if d := gl.Grades[2]; d.Grade != "D" || !d.Passed || d.Category != "専門教育科目（研究・実験）" {
		t.Errorf("D は合格（可）扱い: %+v", d)
	}
	if gl.RemoteCredits != 6 || gl.OtherUnivCredits != 0 {
		t.Errorf("遠隔・他大学 = %v %v", gl.RemoteCredits, gl.OtherUnivCredits)
	}
}

// 個別成績表と同じ配置（左右 2 つの表、数字は列ごとの x 位置）の架空データを読み取る。
func TestParseGradeReportLayout(t *testing.T) {
	s := func(x, y float64, text string) pdfString { return pdfString{x: x, y: y, s: text} }
	strs := []pdfString{
		s(600, 60, "G P A"), s(640, 60, "3.08"),
		// 成績一覧の 3 列目（集計表より上）は無視される
		s(554, 100, "科目番号"), s(560, 150, "99"),
		// 見出し
		s(553, 205, "単位修得状況"), s(603, 205, "必須単位"), s(637, 205, "修得単位"),
		s(676, 205, "単位修得状況"), s(726, 205, "必須単位"), s(760, 205, "修得単位"),
		s(545, 213, "■共通教育"), s(668, 213, "■専門教育"),
		s(545, 221, "健康運動"), s(628, 221, "2"), s(662, 221, "2"),
		s(668, 221, "情報技術"), s(751, 221, "2"), s(785, 221, "2"),
		s(545, 255, "総合"), s(662, 255, "4"), // 必須欄が空
		s(668, 238, "研究実験"), s(748, 238, "16"), s(785, 238, "4"),
		s(545, 289, "小計１"), s(625, 289, "12"), s(659, 289, "11"),
		s(668, 391, "【合計】"), s(745, 391, "130"), s(779, 391, "98"),
		s(545, 468, "遠隔授業"), s(648, 468, "6"),
		s(545, 478, "【備考】"), s(629, 484, "４"), // 備考より下は無視
	}
	rep, err := parseGradeReport(strs)
	if err != nil {
		t.Fatal(err)
	}
	if rep.GPA == nil || *rep.GPA != 3.08 {
		t.Errorf("GPA = %v", rep.GPA)
	}
	byName := map[string]Requirement{}
	for _, r := range rep.Requirements {
		byName[r.Name] = r
	}
	check := func(name, section, kind string, req, earned, short float64) {
		t.Helper()
		r, ok := byName[name]
		if !ok {
			t.Errorf("%s が無い: %+v", name, rep.Requirements)
			return
		}
		if r.Section != section || r.Kind != kind || r.Required == nil || *r.Required != req || r.Earned == nil || *r.Earned != earned || *r.Shortfall != short {
			t.Errorf("%s = %+v (req=%v earned=%v)", name, r, deref(r.Required), deref(r.Earned))
		}
	}
	check("健康運動", "共通教育", "item", 2, 2, 0)
	check("情報技術", "専門教育", "item", 2, 2, 0)
	check("研究実験", "専門教育", "item", 16, 4, 12)
	check("小計１", "共通教育", "subtotal", 12, 11, 1)
	check("合計", "専門教育", "total", 130, 98, 32)
	if r := byName["総合"]; r.Required != nil || r.Earned == nil || *r.Earned != 4 || r.Shortfall != nil {
		t.Errorf("必須欄が空の行 = %+v", r)
	}
	if r := byName["遠隔授業"]; r.Kind != "info" || r.Earned == nil || *r.Earned != 6 {
		t.Errorf("遠隔授業 = %+v", r)
	}
	if _, ok := byName["99"]; ok {
		t.Error("集計表より上の文字列を拾っている")
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestParseGradeReportRejectsUnknownLayout(t *testing.T) {
	if _, err := parseGradeReport([]pdfString{{x: 1, y: 1, s: "成績表"}}); err == nil {
		t.Error("集計表が無い PDF でエラーにならない")
	}
	if _, err := ParseGradeReportPDF([]byte("%PDF-1.3 broken")); err == nil {
		t.Error("壊れた PDF でエラーにならない")
	}
}
