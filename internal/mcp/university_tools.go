package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
)

func addUniversityTools(s *Server, svc *academic.Service) {
	s.AddTool(Tool{
		Name: "get_required_courses", Title: "コース別の必修科目一覧",
		Description: "工学部の便覧に載る必修・選択必修の一覧（出典・ページ付き）。本人の修得判定は check_my_required_courses。未認証時は enter_year 必須。",
		InputSchema: objectSchema(map[string]any{"enter_year": integer("適用便覧年度。本人連携時は省略可"), "faculty": str("学部名。現在は工学部に対応"), "course": str("所属コース名。未認証で省略すると全コース"), "all_courses": boolean("認証済みでも全コースを取得する場合 true")}),
		Handler: func(ctx context.Context, user string, raw json.RawMessage) (any, error) {
			var a struct {
				Year    int    `json:"enter_year"`
				Faculty string `json:"faculty"`
				Course  string `json:"course"`
				All     bool   `json:"all_courses"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, err
			}
			return svc.RequiredCourses(ctx, user, a.Year, a.Faculty, a.Course, a.All)
		},
	})
	s.AddTool(Tool{
		Name: "check_my_required_courses", Title: "本人の必修科目の修得状況",
		Description: "本人の必修だけを成績・今期登録と照合。修得済み／履修中／未修得／確認不能を区別。卒業全体の見通しは get_graduation_status。",
		InputSchema: objectSchema(map[string]any{"enter_year": integer("編入等で適用便覧が入学年度と異なる場合の指定。通常は省略")}), RequiresUser: true,
		Handler: func(ctx context.Context, user string, raw json.RawMessage) (any, error) {
			var a struct {
				Year int `json:"enter_year"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, err
			}
			return svc.RequiredCourseStatus(ctx, user, a.Year)
		},
	})
	s.AddTool(Tool{
		Name: "get_academic_calendar", Title: "学年暦と現在の時期",
		Description: "公式学年暦の学期・登録期間・締切・休日・振替曜日。履修相談の最初に使う。本人の日別予定は get_my_schedule。",
		InputSchema: objectSchema(map[string]any{"date": str("YYYY-MM-DD。省略時は日本時間の今日"), "days": integer("今後何日分の予定を返すか。既定30、最大366"), "include_all": boolean("年度全体の予定も返すか")}),
		Handler: func(ctx context.Context, _ string, raw json.RawMessage) (any, error) {
			var a struct {
				Date string `json:"date"`
				Days int    `json:"days"`
				All  bool   `json:"include_all"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, fmt.Errorf("引数が読めません: %w", err)
			}
			return svc.University.Calendar(ctx, a.Date, a.Days, a.All)
		},
	})
	s.AddTool(Tool{
		Name: "get_course_timetable", Title: "公式時間割と履修上の注意",
		Description: "公式時間割PDFの開講科目・公開定員・履修条件・訂正表。現在の登録者数はシラバス系ツールの include_availability=true で確認。",
		InputSchema: objectSchema(map[string]any{"year": integer("開講年度。省略時は今年度"), "term": str("前学期/後学期。省略時は現在の学期"), "faculty": str("学部名。既定は工学部"), "course": str("コース名。例: 知能情報コース。省略時は学部全体"), "lecture_codes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 50, "description": "任意の講義コードによる絞り込み"}}),
		Handler: func(ctx context.Context, _ string, raw json.RawMessage) (any, error) {
			var a struct {
				Year    int      `json:"year"`
				Term    string   `json:"term"`
				Faculty string   `json:"faculty"`
				Course  string   `json:"course"`
				Codes   []string `json:"lecture_codes"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, err
			}
			if len(a.Codes) > 50 {
				return nil, fmt.Errorf("lecture_codes は50件までです")
			}
			return svc.University.Timetable(ctx, a.Year, a.Term, a.Faculty, a.Course, a.Codes)
		},
	})
	s.AddTool(Tool{
		Name: "get_study_rules", Title: "入学年度別の履修・卒業ルール",
		Description: "適用便覧の卒業・先修・卒業研究・工学融合の条件を出典ページ付きで取得。keyword で本文を絞れる。未認証時は enter_year 必須。",
		InputSchema: objectSchema(map[string]any{"enter_year": integer("入学年度/適用便覧年度。例: 2024。本人連携時は省略可"), "faculty": str("学部名。未認証時の既定は工学部"), "course": str("所属コース名。例: 知能情報コース"), "keyword": str("本文を含むページで絞る。例: 工学融合、卒業、先修")}),
		Handler: func(ctx context.Context, user string, raw json.RawMessage) (any, error) {
			var a struct {
				Year    int    `json:"enter_year"`
				Faculty string `json:"faculty"`
				Course  string `json:"course"`
				Keyword string `json:"keyword"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, err
			}
			return svc.StudyRules(ctx, user, a.Year, a.Faculty, a.Course, a.Keyword)
		},
	})
}
