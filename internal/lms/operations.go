package lms

import (
	"context"
	"errors"

	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// Courses は時間割に載っている WebClass のコースを返す。
func (s *Service) Courses(ctx context.Context, entry *session.Entry) ([]webclass.CourseInfo, error) {
	var courses []webclass.CourseInfo
	err := s.read(ctx, entry, func(c *webclass.Client) error {
		var err error
		courses, err = c.FetchTimetable()
		return err
	})
	return courses, err
}

// CourseContents はコースの資料と出席確認。出席確認だけ取れなかったときは AttendanceErr に入れ、
// 資料は返す（資料の表示を出席確認の失敗で止めない）。
type CourseContents struct {
	Materials     []webclass.ContentInfo
	Attendance    []webclass.AttendanceItem
	AttendanceErr error
}

// CourseContents はコースの資料（試験・レポート・アンケートは除く）と出席確認を返す。
// コースのページが取れなかったときは、空の一覧ではなくエラーを返す。
func (s *Service) CourseContents(ctx context.Context, entry *session.Entry, courseID string) (*CourseContents, error) {
	res := &CourseContents{}
	err := s.read(ctx, entry, func(c *webclass.Client) error {
		var err error
		if res.Materials, err = c.FetchCourseContents(courseID); err != nil {
			return err
		}
		res.Attendance, res.AttendanceErr = c.FetchAttendanceList(courseID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ContentFiles は資料 1 件のファイルを返す。資料以外は開かずに webclass.ErrNotMaterial を返す。
func (s *Service) ContentFiles(ctx context.Context, entry *session.Entry, courseID, contentID string) ([]webclass.FileInfo, error) {
	var files []webclass.FileInfo
	err := s.read(ctx, entry, func(c *webclass.Client) error {
		var err error
		files, err = c.FetchContentFiles(courseID, contentID)
		return err
	})
	return files, err
}

// Inbox は受信箱のメッセージ（自分が送ったものを除く）と、コースの自動通知を除いた未読数を返す。未読数が取れなくても一覧は返す。
func (s *Service) Inbox(ctx context.Context, entry *session.Entry) ([]webclass.Message, int, error) {
	var messages []webclass.Message
	unread := 0
	err := s.read(ctx, entry, func(c *webclass.Client) error {
		var err error
		if messages, err = c.FetchInboxMessages(entry.Username); err != nil {
			return err
		}
		unread, _ = c.FetchUnreadMessageCount(true) // コースからの自動通知は数えない
		return nil
	})
	return messages, unread, err
}

// Describe は利用者に見せるエラーの文言を返す。what は「資料の一覧」のような取得しようとしたもの。
func Describe(what string, err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, webclass.ErrSessionExpired), errors.Is(err, ErrNoPassword):
		return "WebClass のログインが切れました。ログアウトしてもう一度ログインしてください。"
	case errors.Is(err, context.DeadlineExceeded):
		return what + "を取得できませんでした。WebClass の応答がありません。時間をおいてもう一度お試しください。"
	case errors.Is(err, context.Canceled):
		return what + "の取得を中断しました。"
	case errors.Is(err, webclass.ErrNotMaterial):
		return err.Error() + "。WebClass で直接開いてください。"
	case errors.Is(err, webclass.ErrUnexpectedPage):
		return what + "を取得できませんでした（" + err.Error() + "）。WebClass で直接開いて確かめてください。"
	default:
		return what + "の取得に失敗しました: " + err.Error()
	}
}
