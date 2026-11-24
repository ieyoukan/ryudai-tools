package kyomu

import (
	"reflect"
	"testing"
)

func TestSyllabusPlanSessions(t *testing.T) {
	s, err := parseSyllabus(loadFixture(t, "syllabus_detail.html", DefaultBaseURL))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.PlanSessions) != 16 || s.PlanSessions[0].No != 1 || s.PlanSessions[0].Content != "準備" || s.PlanSessions[15].Content != "最終発表" {
		t.Fatalf("%+v", s.PlanSessions)
	}
	for _, tc := range []struct {
		name, plan string
		want       []SyllabusPlanSession
	}{
		{"fullwidth and continuation", "進度によって変更\n第 １ 回：導入\n事前に資料を読む\n第２回 実習", []SyllabusPlanSession{{1, "導入\n事前に資料を読む"}, {2, "実習"}}},
		{"no explicit number", "講義と演習を行います。全15回。", nil},
		{"range is not one session", "第1回～第3回 基礎\n第4回 応用", []SyllabusPlanSession{{4, "応用"}}},
		{"duplicate is ambiguous", "第1回 前半\n第1回 後半\n第2回 演習", []SyllabusPlanSession{{2, "演習"}}},
		{"empty heading", "第1回\n第2回 演習", []SyllabusPlanSession{{2, "演習"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePlanSessions(tc.plan); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}
