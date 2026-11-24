package kyomu

import (
	"strconv"
	"time"
)

const studentInfoPath = "StudentApp/StudentInfo/BasicInfo.aspx"

// StudentProfile は履修の妥当性を判断するための所属情報。
// 氏名・生年月日・住所などの個人情報は読み取らない。
type StudentProfile struct {
	EnterYear  int       `json:"enter_year,omitempty"` // 入学年度
	Faculty    string    `json:"faculty,omitempty"`    // 学部等
	Department string    `json:"department,omitempty"` // 学科等
	Course     string    `json:"course,omitempty"`     // コース等
	Grade      string    `json:"grade,omitempty"`      // 年次（例: ３年後期）
	Source     string    `json:"source,omitempty"`
	FetchedAt  time.Time `json:"fetched_at,omitempty"`
}

// FetchStudentProfile は学籍情報ページから所属情報だけを取得する（読み取りのみ）。
func (c *Client) FetchStudentProfile() (*StudentProfile, error) {
	p, err := c.get(studentInfoPath)
	if err != nil {
		return nil, err
	}
	profile := parseStudentProfile(p)
	profile.Source = c.wc.BaseURL + "/" + studentInfoPath
	profile.FetchedAt = time.Now().UTC()
	return profile, nil
}

func parseStudentProfile(p *page) *StudentProfile {
	field := func(name string) string { return squash(p.text("ctl00_phContents_ucForm_tr" + name + "_lbl_lbl")) }
	sp := &StudentProfile{
		Faculty:    field("FacName"),
		Department: field("DptName"),
		Course:     field("DivName"),
		Grade:      field("Grad"),
	}
	sp.EnterYear, _ = strconv.Atoi(normalizeLabel(field("EnterYear")))
	return sp
}
