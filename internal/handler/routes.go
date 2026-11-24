package handler

import (
	"net/http"

	"github.com/ieyoukan/ryudai-tools/internal/routes"
)

// Routes は JSON API（/api 以下）の経路の表。サーバーはこの表から登録し、docs/api.md もここから作る。
func (h *AcademicHandler) Routes() []routes.Route {
	get, post, put, del := http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete
	return []routes.Route{
		routes.New(get, "/api/academic/calendar", routes.Public, "学年暦（公式ページから取得。24時間キャッシュ）", h.Calendar).With(`date, days, all=1`),
		routes.New(get, "/api/academic/timetable", routes.Public, "開講時間割（年度・学期・学部・コース）", h.Timetable).With(`year, term, faculty, course, lecture_codes`),
		routes.New(get, "/api/academic/rules", routes.OptionalUser, "便覧の履修規程（入学年度・学部・コース）", h.StudyRules).With(`enter_year, faculty, course, keyword`),
		routes.New(get, "/api/academic/required", routes.OptionalUser, "便覧の必修科目（未ログインなら enter_year が必要）", h.RequiredCourses).With(`enter_year, faculty, course, all_courses=1`),
		routes.New(get, "/api/syllabus/search", routes.OptionalUser, "シラバス検索", h.SearchSyllabus).With(`subject, faculty, term, day, period, limit, details`),
		routes.New(get, "/api/syllabus/options", routes.Public, "シラバス検索の選択肢（学部・学期など）", h.SyllabusOptions).With(`faculty`),
		routes.New(get, "/api/syllabus/{year}/{code}", routes.OptionalUser, "シラバス詳細", h.Syllabus),
		routes.New(get, "/api/me/required", routes.SessionOrToken, "本人の必修科目の修得状況", h.MyRequiredCourses).With(`enter_year`),
		routes.New(get, "/api/me/registration", routes.SessionOrToken, "本人の履修登録状況", h.MyRegistration),
		routes.New(get, "/api/me/schedule", routes.SessionOrToken, "本人の実日付の予定（format=ics でカレンダーファイル）", h.MySchedule).With(`date, days, format=ics, refresh=1`),
		routes.New(get, "/api/me/lectures/{year}/{code}", routes.SessionOrToken, "講義 1 件の全授業回（授業明細スケジュール）", h.MyLecture),
		routes.New(get, "/api/me/candidates", routes.SessionOrToken, "空きコマに入る候補講義", h.MyCandidates).With(`slots=月2,木3, scopes=general,own, keywords, with_syllabus=1, offset, limit`),
		routes.New(post, "/api/me/plan", routes.SessionOrToken, "履修プランのチェック", h.MyPlan).With(`{"lecture_codes": [...]}`),
		routes.New(get, "/api/me/graduation", routes.SessionOrToken, "成績と卒業要件の見通し", h.MyGraduation).With(`include_grades=1`),
		routes.New(get, "/api/me/plan/suggestions", routes.SessionOrToken, "卒業までの計画に入れる科目の提案", h.PlanSuggestions).With(`term=2027前期`),
		routes.New(post, "/api/me/plan/items", routes.Session, "卒業までの計画に科目を追加", h.AddPlanItem),
		routes.New(del, "/api/me/plan/items", routes.Session, "卒業までの計画と選んだ区分をすべて削除", h.DeletePlan),
		routes.New(del, "/api/me/plan/items/{id}", routes.Session, "卒業までの計画から科目を 1 件削除", h.DeletePlanItem),
		routes.New(put, "/api/me/lecture-categories", routes.Session, "今期の講義の科目区分を自分で選んで保存", h.SaveLectureCategories),
	}
}
