package academic

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ieyoukan/daigaku-hack/internal/db"
	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
)

// CategoryStore は今期の登録科目の科目区分を保存する（db.DB が実装）。
// 履修登録の期間が終わると教務システムの画面に科目区分が出なくなるため、期間中に見えた区分や利用者が選んだ区分を残す。
type CategoryStore interface {
	LectureCategories(userID string) (map[string]db.LectureCategory, error)
	SaveLectureCategory(userID string, c db.LectureCategory) error
}

// seriesSuffix は「情報技術演習II」「ソフトウエア開発演習Ⅲ」「英語講読演習2」の番号部分。
var seriesSuffix = regexp.MustCompile(`\s*(?:[ⅠⅡⅢⅣⅤⅥⅦⅧⅨⅩ]+|(?:I|II|III|IV|V|VI|VII|VIII)|[0-9０-９]+|[A-DＡ-Ｄ])$`)

// seriesStem は科目名から番号を除いた部分。番号が無ければ空文字（シリーズとみなさない）。
func seriesStem(title string) string {
	t := baseTitle(title)
	stem := seriesSuffix.ReplaceAllString(t, "")
	if stem == t || utf8.RuneCountInString(stem) < 3 {
		return ""
	}
	return stem
}

// fillLectureCategories は今期の登録科目のうち科目区分が分からないものを、次の順で補う。
//  1. 保存済みの区分（履修登録の期間中に教務システムで見えた区分、または利用者が選んだ区分）
//  2. 同じ科目名の成績（再履修）の区分
//  3. 番号だけ違う科目（Ⅰ→Ⅱ など）の成績の区分（候補が 1 つに決まる場合だけ）
//
// 履修登録の期間中は、画面に出ている区分を保存して、期間後に使えるようにする。
func (s *Service) fillLectureCategories(username string, reg *kyomu.Registration, grades *kyomu.GradeList) {
	if reg == nil {
		return
	}
	saved := map[string]db.LectureCategory{}
	if s.Categories != nil {
		for _, l := range reg.Lectures {
			if l.Category != "" && l.LectureCode != "" {
				if err := s.Categories.SaveLectureCategory(username, db.LectureCategory{LectureCode: l.LectureCode, Category: l.Category, Source: "kyomu"}); err != nil {
					log.Printf("科目区分の保存失敗 (%s): %v", username, err)
				}
			}
		}
		if m, err := s.Categories.LectureCategories(username); err == nil {
			saved = m
		} else {
			log.Printf("科目区分の読み取り失敗 (%s): %v", username, err)
		}
	}
	byTitle := map[string]string{}
	byStem := map[string]map[string]string{} // stem → category → 根拠にした科目名
	if grades != nil {
		for _, g := range grades.Grades {
			if g.Category == "" {
				continue
			}
			byTitle[baseTitle(g.Title)] = g.Category
			if stem := seriesStem(g.Title); stem != "" {
				if byStem[stem] == nil {
					byStem[stem] = map[string]string{}
				}
				byStem[stem][g.Category] = g.Title
			}
		}
	}
	// 講義コードが近い科目の区分（候補として出すだけで、自動では使わない）。講義コードの先頭 6 桁は開講部署 3 桁 + 科目の番号で、
	// 番号の近い科目は同じ区分のことが多い（例: 番号が隣り合う「演習Ⅱ」と「演習Ⅲ」）。
	suggest := func(code string) string {
		n, err := strconv.Atoi(prefixDigits(code, 6))
		if err != nil || grades == nil {
			return ""
		}
		best, bestDist, bestCode := "", -1, 0
		for _, g := range grades.Grades {
			m, err := strconv.Atoi(prefixDigits(g.LectureCode, 6))
			if err != nil || g.Category == "" || m/1000 != n/1000 {
				continue
			}
			d := m - n
			if d < 0 {
				d = -d
			}
			if bestDist < 0 || d < bestDist || (d == bestDist && m < bestCode) {
				best, bestDist, bestCode = g.Category, d, m
			}
		}
		return best
	}

	for i := range reg.Lectures {
		l := &reg.Lectures[i]
		if l.Category != "" {
			continue
		}
		if c, ok := saved[l.LectureCode]; ok && l.LectureCode != "" {
			l.Category, l.CategorySource = c.Category, "saved"
			if c.Source == "user" {
				l.CategoryBasis = "あなたが選んだ区分"
			} else {
				l.CategoryBasis = "履修登録の期間中に教務システムで表示された区分"
			}
			continue
		}
		if c, ok := byTitle[baseTitle(l.Title)]; ok {
			l.Category, l.CategorySource, l.CategoryBasis = c, "grades", "同じ科目の成績"
			continue
		}
		if cats := byStem[seriesStem(l.Title)]; len(cats) == 1 {
			for c, from := range cats {
				l.Category, l.CategorySource, l.CategoryBasis = c, "series", "「"+strings.TrimSpace(from)+"」の成績"
			}
			continue
		}
		l.SuggestedCategory = suggest(l.LectureCode)
	}
}

// prefixDigits は講義コードの先頭 n 桁（足りなければ空文字）。
func prefixDigits(code string, n int) string {
	if len(code) < n {
		return ""
	}
	return code[:n]
}
