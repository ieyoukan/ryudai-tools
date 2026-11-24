package ui

import (
	"net/http"

	"github.com/ieyoukan/ryudai-tools/internal/routes"
)

// PageRoutes は画面用のページ API（/api/page 以下）の経路の表。応答は web/src/api/gen/ui.ts の型。
// GET 以外は X-Ryudai-Page ヘッダーが必要（他サイトからの送信を防ぐ）。
func (h *Handler) PageRoutes() []routes.Route {
	get, post := http.MethodGet, http.MethodPost
	return []routes.Route{
		routes.New(get, "/me", routes.OptionalUser, "ログイン中のユーザー名（画面のヘッダー用）", h.ShowMe).Returns("BaseData"),
		routes.New(get, "/login", routes.Public, "ログイン画面のデータ", h.ShowLogin).Returns("LoginData"),
		routes.New(post, "/login", routes.Public, "WebClass の ID・パスワードでログイン（大学のメールアドレスは @ より前を ID にする）", h.HandleLogin).Returns("LoginData"),
		routes.New(post, "/logout", routes.Session, "ログアウト", h.HandleLogout),
		routes.New(get, "/terms", routes.Public, "利用規約", h.ShowTerms).Returns("BaseData"),
		routes.New(get, "/privacy", routes.Public, "プライバシーポリシー", h.ShowPrivacy).Returns("BaseData"),
		routes.New(get, "/courses", routes.Session, "WebClass の時間割", h.ShowCourses).Returns("CoursesData"),
		routes.New(get, "/courses/{courseID}", routes.Session, "コースの資料と出席確認", h.ShowContents).Returns("ContentsData"),
		routes.New(get, "/courses/{courseID}/contents/{contentID}", routes.Session, "資料 1 件のファイル", h.ShowFiles).Returns("FilesData"),
		routes.New(get, "/messages", routes.Session, "WebClass の受信箱", h.ShowMessages).Returns("MessagesData"),
		routes.New(get, "/settings", routes.Session, "設定（API トークン・連携中のアプリ）", h.ShowSettings).Returns("SettingsData"),
		routes.New(post, "/settings/api-tokens", routes.Session, "API トークンを発行", h.HandleCreateAPIToken).Returns("SettingsData"),
		routes.New(post, "/settings/api-tokens/delete", routes.Session, "API トークンを削除", h.HandleDeleteAPIToken).Returns("SettingsData"),
		routes.New(post, "/settings/oauth-grants/revoke", routes.Session, "AI アプリとの連携を解除", h.HandleRevokeOAuthGrant).Returns("SettingsData"),
		routes.New(get, "/registration", routes.Session, "履修登録の状況", h.ShowRegistration).Returns("RegistrationData"),
		routes.New(get, "/registration/status", routes.Session, "履修登録の期間中か（履修登録タブの表示判定）", h.ShowRegistrationStatus).Returns("RegistrationStatusData"),
		routes.New(get, "/registration/candidates", routes.Session, "空きコマの候補講義", h.ShowCandidates).Returns("CandidatesData"),
		routes.New(get, "/syllabus", routes.Public, "シラバス検索", h.ShowSyllabusSearch).Returns("SyllabusSearchData"),
		routes.New(get, "/syllabus/{year}/{code}", routes.Public, "シラバス詳細", h.ShowSyllabusDetail).Returns("SyllabusDetailData"),
	}
}

// FileRoutes は資料ファイルの中継・まとめてダウンロードなど、画面ではない経路の表（ログイン Cookie が必要）。
func (h *Handler) FileRoutes() []routes.Route {
	get := http.MethodGet
	return []routes.Route{
		routes.New(get, "/courses/{courseID}/files.json", routes.Session, "コースの全資料のファイル一覧", h.CourseFilesJSON),
		routes.New(get, "/courses/{courseID}/files.zip", routes.Session, "コースの全資料を ZIP でダウンロード", h.DownloadCourseZip),
		routes.New(get, "/courses/{courseID}/summary", routes.Session, "AI 要約用に資料の本文をまとめたテキスト", h.CourseSummary),
		routes.New(get, "/download", routes.Session, "WebClass のファイル 1 件を中継", h.Download).With("url, name"),
	}
}
