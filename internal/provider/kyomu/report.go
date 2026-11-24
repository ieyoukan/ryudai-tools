package kyomu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

// 個別成績表（PDF）。成績明細（HTML）には無い「区分ごとの必須単位と修得単位」（卒業要件の集計）と GPA が載っている。
const gradeReportPath = "00_ReportClass/Report/ResultsReport/ResultsReportPdf.aspx"

// Requirement は卒業要件の集計表の 1 行。
type Requirement struct {
	Section   string   `json:"section"`             // 共通教育 / 専門教育 / 専門基礎教育
	Name      string   `json:"name"`                // 例: 情報技術、（外国語計）、合計
	Kind      string   `json:"kind"`                // item（区分）/ subtotal（小計・〇〇計）/ total（【】の合計）/ info（遠隔授業など）
	Required  *float64 `json:"required,omitempty"`  // 必須単位（空欄なら nil）
	Earned    *float64 `json:"earned,omitempty"`    // 修得単位（空欄なら nil）
	Shortfall *float64 `json:"shortfall,omitempty"` // 不足（必須がある行のみ。足りていれば 0）
}

// GradeReport は個別成績表から読み取った集計。
type GradeReport struct {
	GPA          *float64      `json:"gpa,omitempty"`
	Requirements []Requirement `json:"requirements"`
}

// FetchGradeReport は個別成績表の PDF を取得して卒業要件の集計を読み取る（読み取りのみ）。
func (c *Client) FetchGradeReport() (*GradeReport, error) {
	// 先に成績照会メニューを開いておく（画面遷移と同じ順序にする）
	if _, err := c.get(gradesMenuPath); err != nil {
		return nil, err
	}
	// prm は画面側の get_rand_prm() と同じくキャッシュ回避用の値
	prm := fmt.Sprintf("%d%d", rand.Intn(16), time.Now().UnixNano()/int64(time.Millisecond))
	resp, err := c.wc.HTTPClient.Get(c.abs(gradeReportPath + "?prm=" + prm))
	if err != nil {
		return nil, fmt.Errorf("個別成績表の取得: %w", err)
	}
	defer resp.Body.Close()
	if strings.Contains(resp.Request.URL.String(), "RichTimeOut.aspx") {
		return nil, ErrSessionExpired
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(body, []byte("%PDF")) {
		return nil, errors.New("個別成績表が PDF ではありません（成績照会が利用できない期間の可能性があります）")
	}
	return ParseGradeReportPDF(body)
}

// pdfString は PDF 上の 1 つの文字列（左端の座標つき。y は上からの距離）。
type pdfString struct {
	x, y float64
	s    string
}

// ParseGradeReportPDF は個別成績表の PDF から集計を読み取る。
func ParseGradeReportPDF(data []byte) (rep *GradeReport, err error) {
	defer func() {
		if r := recover(); r != nil { // PDF ライブラリは壊れた入力で panic することがある
			rep, err = nil, fmt.Errorf("個別成績表の PDF を読めません: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("個別成績表の PDF を開けません: %w", err)
	}
	if r.NumPage() < 1 {
		return nil, errors.New("個別成績表にページがありません")
	}
	var all []pdfString
	for i := 1; i <= r.NumPage(); i++ {
		all = append(all, pageStrings(r.Page(i))...)
	}
	return parseGradeReport(all)
}

// pageStrings は文字単位のテキストを、同じ描画位置ごとに文字列へまとめる。
// この PDF（ActiveReports 製）は 1 つの文字列の全文字が同じ座標で出てくる。
func pageStrings(p pdf.Page) []pdfString {
	height := 612.0
	if box := p.V.Key("MediaBox"); box.Len() == 4 {
		height = box.Index(3).Float64()
	}
	var out []pdfString
	var cur *pdfString
	for _, t := range p.Content().Text {
		y := height - t.Y
		if cur != nil && math.Abs(cur.x-t.X) < 0.01 && math.Abs(cur.y-y) < 0.01 {
			cur.s += t.S
			continue
		}
		out = append(out, pdfString{x: t.X, y: y, s: t.S})
		cur = &out[len(out)-1]
	}
	return out
}

var (
	gpaRe    = regexp.MustCompile(`G\s*P\s*A\s*([0-9]+(?:\.[0-9]+)?)`)
	numberRe = regexp.MustCompile(`^[0-9０-９]+(?:\.[0-9]+)?$`)
)

// parseGradeReport は文字列の配置から集計表を復元する。
// 集計表は「単位修得状況 / 必須単位 / 修得単位」の見出しが左右 2 組あり、左が共通教育、右が専門教育。
func parseGradeReport(strs []pdfString) (*GradeReport, error) {
	rep := &GradeReport{Requirements: []Requirement{}}

	// GPA: 同じ行の文字列をつなげて「G P A 3.08」を探す
	for _, line := range groupRows(strs) {
		var sb strings.Builder
		for _, s := range line {
			sb.WriteString(s.s)
			sb.WriteString(" ")
		}
		if m := gpaRe.FindStringSubmatch(sb.String()); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				rep.GPA = &v
			}
			break
		}
	}

	headers := func(label string) []pdfString {
		var hs []pdfString
		for _, s := range strs {
			if strings.TrimSpace(s.s) == label {
				hs = append(hs, s)
			}
		}
		sort.Slice(hs, func(i, j int) bool { return hs[i].x < hs[j].x })
		return hs
	}
	status, req, earned := headers("単位修得状況"), headers("必須単位"), headers("修得単位")
	if len(status) != 2 || len(req) != 2 || len(earned) != 2 {
		return nil, errors.New("個別成績表の「単位修得状況」の表が見つかりません（様式が変わった可能性があります）")
	}
	headerY := status[0].y
	stopY := math.MaxFloat64
	for _, s := range strs {
		if strings.HasPrefix(strings.TrimSpace(s.s), "【備考】") && s.y > headerY {
			stopY = math.Min(stopY, s.y)
		}
	}
	// 表の境目: 右表の見出しより少し左から右表。ラベルは見出しより 8pt ほど左から始まる。
	leftStart, rightStart := status[0].x-12, status[1].x-12

	type table struct {
		reqX, earnedX float64
		section       string
		strs          []pdfString
	}
	tables := []*table{
		{reqX: req[0].x, earnedX: earned[0].x},
		{reqX: req[1].x, earnedX: earned[1].x},
	}
	for _, s := range strs {
		if s.y <= headerY+2 || s.y >= stopY || s.x < leftStart || strings.TrimSpace(s.s) == "" {
			continue
		}
		if s.x >= rightStart {
			tables[1].strs = append(tables[1].strs, s)
		} else {
			tables[0].strs = append(tables[0].strs, s)
		}
	}

	for _, t := range tables {
		for _, line := range groupRows(t.strs) {
			var label []string
			var reqV, earnedV *float64
			for _, s := range line {
				text := strings.TrimSpace(s.s)
				if numberRe.MatchString(text) && s.x >= t.reqX-10 {
					v, _ := strconv.ParseFloat(normalizeLabel(text), 64)
					if s.x < t.earnedX-2 {
						reqV = &v
					} else {
						earnedV = &v
					}
					continue
				}
				label = append(label, text)
			}
			name := strings.Join(label, "")
			if name == "" {
				continue
			}
			if strings.HasPrefix(name, "■") {
				t.section = strings.TrimPrefix(name, "■")
				continue
			}
			rq := Requirement{Section: t.section, Name: name, Required: reqV, Earned: earnedV, Kind: requirementKind(name)}
			rq.Name = strings.Trim(rq.Name, "【】")
			if rq.Required != nil {
				e := 0.0
				if rq.Earned != nil {
					e = *rq.Earned
				}
				short := math.Max(0, *rq.Required-e)
				rq.Shortfall = &short
			}
			rep.Requirements = append(rep.Requirements, rq)
		}
	}
	return rep, nil
}

func requirementKind(name string) string {
	switch {
	case strings.HasPrefix(name, "【"):
		return "total"
	case strings.HasPrefix(name, "小計"), strings.HasPrefix(name, "（") && strings.HasSuffix(name, "計）"):
		return "subtotal"
	case strings.HasPrefix(name, "遠隔授業"), strings.HasPrefix(name, "他大学等"), name == "合計に含まない":
		return "info"
	}
	return "item"
}

// groupRows は y がほぼ同じ文字列を行にまとめ、行内は x 順に並べる。
func groupRows(strs []pdfString) [][]pdfString {
	sorted := append([]pdfString(nil), strs...)
	sort.Slice(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].y-sorted[j].y) > 1.5 {
			return sorted[i].y < sorted[j].y
		}
		return sorted[i].x < sorted[j].x
	})
	var rows [][]pdfString
	for _, s := range sorted {
		if n := len(rows); n > 0 && math.Abs(rows[n-1][0].y-s.y) <= 1.5 {
			rows[n-1] = append(rows[n-1], s)
			continue
		}
		rows = append(rows, []pdfString{s})
	}
	for _, r := range rows {
		sort.Slice(r, func(i, j int) bool { return r[i].x < r[j].x })
	}
	return rows
}
