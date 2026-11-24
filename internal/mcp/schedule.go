package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ieyoukan/daigaku-hack/internal/academic"
)

func addScheduleTool(s *Server, svc *academic.Service) {
	s.AddTool(Tool{
		Name: "get_my_schedule", Title: "今日・指定日の大学の予定", RequiresUser: true,
		Description: "本人の実日付の時間割・休講・教室・Googleカレンダー追加リンク。既定は今日1日、最大31日。曜日固定の時間割で補完しない。全学行事は get_academic_calendar。",
		InputSchema: objectSchema(map[string]any{"date": str("開始日 YYYY-MM-DD。省略時は日本時間の今日"), "days": integer("日数。既定1、最大31"), "refresh": boolean("5分間の週キャッシュを更新する。既定false")}),
		Handler: func(ctx context.Context, user string, raw json.RawMessage) (any, error) {
			var a struct {
				Date    string `json:"date"`
				Days    int    `json:"days"`
				Refresh bool   `json:"refresh"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return nil, fmt.Errorf("引数が読めません: %w", err)
				}
			}
			return svc.MySchedule(ctx, user, a.Date, a.Days, a.Refresh)
		},
	})
}

func addLectureScheduleTool(s *Server, svc *academic.Service) {
	s.AddTool(Tool{
		Name: "get_my_lecture_schedule", Title: "講義の全授業回（日付・時刻・教室）", RequiresUser: true,
		Description: "本人の講義1件の全授業回・時刻・教室・休講等を取得。year と lecture_code は本人の時間割・履修登録から指定。日別の予定は get_my_schedule。",
		InputSchema: objectSchema(map[string]any{"year": integer("開講年度（例: 2026）"), "lecture_code": str("講義コード"), "refresh": boolean("10分間のキャッシュを更新する。既定false")}, "year", "lecture_code"),
		Handler: func(ctx context.Context, user string, raw json.RawMessage) (any, error) {
			var a struct {
				Year        int    `json:"year"`
				LectureCode string `json:"lecture_code"`
				Refresh     bool   `json:"refresh"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return nil, fmt.Errorf("引数が読めません: %w", err)
			}
			return svc.MyLectureSchedule(user, a.Year, a.LectureCode, a.Refresh)
		},
	})
}
