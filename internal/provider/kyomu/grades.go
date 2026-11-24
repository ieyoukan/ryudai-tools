package kyomu

import (
	"errors"
	"strconv"
	"strings"
)

const (
	gradesMenuPath = "StudentApp/ReferResults/Menu.aspx"
	gradesGridID   = "ctl00_phContents_Results1_gridResultsRec_rdlGrid_gridList"
	gradesDetailID = "ctl00_phContents_Results1_gridResultsRec_tblDetail"
)

// Grade は成績 1 件（成績照会の「成績明細」）。
type Grade struct {
	Year        int     `json:"year"`
	Term        string  `json:"term"` // 前期 / 後期
	LectureCode string  `json:"lecture_code"`
	Title       string  `json:"title"`
	Instructor  string  `json:"instructor,omitempty"`
	Credits     float64 `json:"credits"`
	Grade       string  `json:"grade"` // A / B / C / D / F / P / R など（半角に正規化）
	Passed      bool    `json:"passed"`
	Category    string  `json:"category"`              // 科目区分（例: 専門教育科目（知能情報コア））
	GradeLevel  string  `json:"grade_level,omitempty"` // 取得時の年次（例: １年後期）
	Remote      bool    `json:"remote,omitempty"`      // 遠隔授業
	Note        string  `json:"note,omitempty"`
}

// GradeList は過去の全成績。
type GradeList struct {
	Grades           []Grade `json:"grades"`
	RemoteCredits    float64 `json:"remote_credits"`     // 遠隔授業の修得単位数（卒業単位は最大 60 まで）
	OtherUnivCredits float64 `json:"other_univ_credits"` // 他大学等の修得単位数（同上）
}

// FetchGrades は成績照会の「成績明細の表示（過去の全成績）」を取得する（読み取りのみ）。
func (c *Client) FetchGrades() (*GradeList, error) {
	p, err := c.get(gradesMenuPath)
	if err != nil {
		return nil, err
	}
	btn, val := p.submitButton("_btnResul")
	if btn == "" {
		return nil, errors.New("成績明細の表示ボタンが見つかりません（成績照会が利用できない期間の可能性があります）")
	}
	p, err = c.submit(p, btn, val, map[string]string{"ctl00$phContents$menuResults$Results": "rbtnResult"})
	if err != nil {
		return nil, err
	}
	return parseGrades(p)
}

func parseGrades(p *page) (*GradeList, error) {
	table, ok := p.ids[gradesGridID]
	if !ok {
		return nil, errors.New("成績の表が見つかりません")
	}
	col, rows := gridRowsWithHeader(table, "講義コード")
	if _, ok := col["講義コード"]; !ok {
		return nil, errors.New("成績の表の見出しが想定と違います")
	}
	gl := &GradeList{Grades: []Grade{}}
	category := ""
	for _, r := range rows {
		cell := func(name string) string { return gridCell(col, r, name) }
		first := ""
		if len(r) > 0 {
			first = squash(nodeText(r[0]))
		}
		// 科目区分の見出し行: ［専門教育科目（知能情報コア）］
		if strings.HasPrefix(first, "［") {
			category = strings.Trim(first, "［］")
			continue
		}
		year, err := strconv.Atoi(cell("年度"))
		if err != nil || cell("講義コード") == "" {
			continue
		}
		g := Grade{
			Year:        year,
			Term:        cell("評価学期"),
			LectureCode: cell("講義コード"),
			Title:       cell("科目名"),
			Instructor:  cell("教員名"),
			Grade:       normalizeGrade(cell("評価")),
			Category:    category,
			GradeLevel:  cell("取得時Grade"),
			Note:        cell("備考"),
		}
		g.Credits, _ = strconv.ParseFloat(normalizeLabel(cell("単位数")), 64)
		g.Passed = gradePassed(g.Grade)
		g.Remote = strings.Contains(g.Note, "遠隔")
		gl.Grades = append(gl.Grades, g)
	}
	if t, ok := p.ids[gradesDetailID]; ok {
		_, detail := gridRows(t)
		if len(detail) > 0 && len(detail[0]) >= 2 {
			gl.RemoteCredits, _ = strconv.ParseFloat(normalizeLabel(squash(nodeText(detail[0][0]))), 64)
			gl.OtherUnivCredits, _ = strconv.ParseFloat(normalizeLabel(squash(nodeText(detail[0][1]))), 64)
		}
	}
	return gl, nil
}

// normalizeGrade は「Ａ」「F 不可」などを "A" "F" に揃える。
func normalizeGrade(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r >= 'Ａ' && r <= 'Ｚ' {
			return r - 'Ａ' + 'A'
		}
		return r
	}, s))
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return s
}

// gradePassed は評価が合格（単位修得）かを返す。A〜D は合格、F は不合格、P（合格）・R（認定）は修得。
func gradePassed(g string) bool {
	switch g {
	case "A", "B", "C", "D", "P", "R", "S", "合", "認":
		return true
	}
	return false
}
