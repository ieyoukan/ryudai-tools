package academic

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ieyoukan/daigaku-hack/internal/provider/kyomu"
)

// LectureDigest は講義 1 件の要点。科目名だけでは中身が分からない講義を、AI が一覧で比べられる粒度にまとめる。
//
// WorkloadSignals と EligibilityNotes はシラバスの文面から機械的に拾った手がかりで、正確さは保証しない。
type LectureDigest struct {
	Quota              string               `json:"quota,omitempty"`
	Availability       *LectureAvailability `json:"availability,omitempty"`
	Year               int                  `json:"year"`
	LectureCode        string               `json:"lecture_code"`
	Title              string               `json:"title"`
	Credits            float64              `json:"credits"`
	Term               string               `json:"term,omitempty"`
	DayPeriod          string               `json:"day_period,omitempty"`
	Slots              []kyomu.Slot         `json:"slots,omitempty"`
	Room               string               `json:"room,omitempty"`
	Faculty            string               `json:"faculty,omitempty"`
	Instructors        string               `json:"instructors,omitempty"`
	Language           string               `json:"language,omitempty"`
	TeachingFormat     string               `json:"teaching_format,omitempty"`
	LectureTypes       []string             `json:"lecture_types,omitempty"`
	RegistrationMethod string               `json:"registration_method,omitempty"`
	Outline            string               `json:"outline,omitempty"`      // 授業内容と方法（抜粋）
	Goals              string               `json:"goals,omitempty"`        // 達成目標（抜粋）
	Grading            string               `json:"grading,omitempty"`      // 評価方法（抜粋）
	Requirements       string               `json:"requirements,omitempty"` // 履修条件（抜粋）
	// WorkloadSignals は負担の目安（例: "期末試験", "毎回の課題"）。シラバスの文面からの推定。
	WorkloadSignals []string `json:"workload_signals,omitempty"`
	// EligibilityNotes は「その学生が取るのに適切か」を判断するための注意文（履修条件・メッセージなどから抜粋）。
	EligibilityNotes []string `json:"eligibility_notes,omitempty"`
	URL              string   `json:"url"`
	Error            string   `json:"error,omitempty"`
}

// DescribeLectures は複数の講義の要点をまとめて返す（シラバスを 1 件ずつ読む手間を省く）。最大 30 件。
func (s *Service) DescribeLectures(year int, codes []string) ([]LectureDigest, error) {
	if len(codes) == 0 {
		return nil, errors.New("講義コードを 1 つ以上指定してください")
	}
	if len(codes) > 30 {
		return nil, errors.New("一度に要約できるのは 30 件までです")
	}
	if year == 0 {
		year = kyomu.AcademicYear(time.Now())
	}
	out := make([]LectureDigest, len(codes))
	var wg sync.WaitGroup
	for i, code := range codes {
		wg.Add(1)
		go func(i int, code string) {
			defer wg.Done()
			out[i] = s.digest(year, strings.TrimSpace(code))
		}(i, code)
	}
	wg.Wait()
	return out, nil
}

func (s *Service) digest(year int, code string) LectureDigest {
	syl, err := s.Syllabus(year, code)
	if err != nil {
		return LectureDigest{Year: year, LectureCode: code, URL: kyomu.SyllabusURL(s.baseURL, year, code), Error: err.Error()}
	}
	return digestOf(syl)
}

func digestOf(syl *kyomu.Syllabus) LectureDigest {
	return LectureDigest{
		Year:               syl.Year,
		Quota:              syl.Quota,
		LectureCode:        syl.LectureCode,
		Title:              syl.Title,
		Credits:            syl.Credits,
		Term:               syl.Term,
		DayPeriod:          syl.DayPeriod,
		Slots:              syl.Slots,
		Room:               syl.Room,
		Faculty:            syl.Faculty,
		Instructors:        syl.Instructors,
		Language:           syl.Language,
		TeachingFormat:     syl.TeachingFormat,
		LectureTypes:       syl.LectureTypes,
		RegistrationMethod: syl.RegistrationMethod,
		Outline:            excerpt(syl.Outline, 300),
		Goals:              excerpt(syl.Goals, 150),
		Grading:            excerpt(syl.Grading, 200),
		Requirements:       excerpt(syl.Requirements, 300),
		WorkloadSignals:    workloadSignals(syl),
		EligibilityNotes:   eligibilityNotes(syl),
		URL:                syl.URL,
	}
}

// workloadRules は「何があれば負担の手がかりとみなすか」。上から順に判定する。
var workloadRules = []struct {
	label string
	re    *regexp.Regexp
}{
	{"期末試験", regexp.MustCompile(`期末(試験|テスト|考査)|定期試験|最終試験`)},
	{"中間試験", regexp.MustCompile(`中間(試験|テスト|考査)`)},
	{"小テスト", regexp.MustCompile(`小テスト|確認テスト|ミニテスト|単語テスト`)},
	{"毎回の課題", regexp.MustCompile(`毎回の?(課題|レポート|提出|演習|宿題|小レポート|リアクション)|毎週の?(課題|レポート|提出|宿題)`)},
	{"レポート", regexp.MustCompile(`レポート`)},
	{"発表・プレゼン", regexp.MustCompile(`発表|プレゼン`)},
	{"グループワーク", regexp.MustCompile(`グループ(ワーク|活動|討論|学習)|ディスカッション|ペアワーク`)},
	{"実験・実習", regexp.MustCompile(`実験|実習|フィールドワーク`)},
	{"出席・参加を評価", regexp.MustCompile(`出席|授業(への)?参加|平常点`)},
	{"事前・事後学習が多め", regexp.MustCompile(`予習|復習|事前に.{0,10}(読|視聴|準備)`)},
}

func workloadSignals(syl *kyomu.Syllabus) []string {
	text := strings.Join([]string{syl.Grading, syl.Plan, syl.Outline, syl.Preparation, syl.Review}, "\n")
	var out []string
	for _, r := range workloadRules {
		if r.re.MatchString(text) {
			out = append(out, r.label)
		}
	}
	if containsAny(syl.LectureTypes, "演習", "実習") && !contains(out, "実験・実習") {
		out = append(out, "演習形式")
	}
	return out
}

// eligibilityRe は「対象者・前提・制限」を表す言い回し。
var eligibilityRe = regexp.MustCompile(`対象|のみ|限る|限定|履修不可|受講(しない|できない|不可)|要相談|相談すること|前提|既修|修得(済|して)|単位を修得|入学生|年次|年生|留学生|コース|学科の学生|学部の学生|許可|抽選|定員`)

var sentenceSplit = regexp.MustCompile(`[。．\n]`)

// transferNote は振替科目の注意。候補一覧の科目名にだけ「(振替)」が付くことがあるので、呼び出し側でも使う。
const transferNote = "振替科目: 旧カリキュラム等の別科目として単位が認定される開講。自分の便覧で振替の対象か確認が必要"

// japaneseCourseNote は国際教育センターの日本語科目の注意（シラバスに対象者が書かれていないことが多い）。
const japaneseCourseNote = "国際教育センター開講の日本語科目。シラバスに明記は無いが、主に日本語を学ぶ留学生向けの授業の可能性が高い（推定）"

func eligibilityNotes(syl *kyomu.Syllabus) []string {
	var out []string
	if strings.Contains(syl.Title, "振替") {
		out = append(out, transferNote)
	}
	if strings.Contains(syl.Faculty, "国際教育センター") && strings.HasPrefix(strings.TrimLeft(syl.Title, "☆"), "日本語") {
		out = append(out, japaneseCourseNote)
	}
	seen := map[string]bool{}
	for _, src := range []string{syl.Requirements, syl.Message} {
		for _, sent := range sentenceSplit.Split(src, -1) {
			sent = strings.TrimSpace(strings.Trim(strings.TrimSpace(sent), "・※"))
			if sent == "" || seen[sent] || !eligibilityRe.MatchString(sent) {
				continue
			}
			seen[sent] = true
			out = append(out, excerpt(sent, 120))
			if len(out) >= 6 {
				return out
			}
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func containsAny(list []string, vs ...string) bool {
	for _, v := range vs {
		if contains(list, v) {
			return true
		}
	}
	return false
}
