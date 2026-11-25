package lms

import (
	"context"
	"errors"

	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

// Courses は時間割に載っている WebClass のコースを返す（CoursesTTL の間はキャッシュ）。
func (s *Service) Courses(ctx context.Context, entry *session.Entry) ([]webclass.CourseInfo, error) {
	key := cacheKey(entry.Username)
	use, err := s.useCache(ctx, entry)
	if err != nil {
		return nil, err
	}
	if v, ok := s.courses.get(key); ok && use {
		return v, nil
	}
	var courses []webclass.CourseInfo
	err = s.read(ctx, entry, func(c *webclass.Client) error {
		var err error
		courses, err = c.FetchTimetable()
		return err
	})
	if err != nil {
		return nil, err
	}
	s.courses.set(key, courses)
	return courses, nil
}

// CourseContents はコースの資料と出席確認。出席確認だけ取れなかったときは AttendanceErr に入れ、
// 資料は返す（資料の表示を出席確認の失敗で止めない）。
type CourseContents struct {
	Materials     []webclass.ContentInfo
	Attendance    []webclass.AttendanceItem
	AttendanceErr error
}

// CourseContents はコースの資料（試験・レポート・アンケートは除く）と出席確認を返す。
// 資料は MaterialsTTL、出席確認は AttendanceTTL の間キャッシュする。コースのページが取れなかったときは、
// 空の一覧ではなくエラーを返す。
func (s *Service) CourseContents(ctx context.Context, entry *session.Entry, courseID string) (*CourseContents, error) {
	key := cacheKey(entry.Username, courseID)
	res := &CourseContents{}
	var haveMaterials, haveAttendance bool
	use, err := s.useCache(ctx, entry)
	if err != nil {
		return nil, err
	}
	if use {
		res.Materials, haveMaterials = s.materials.get(key)
		res.Attendance, haveAttendance = s.attendance.get(key)
	}
	if haveMaterials && haveAttendance {
		return res, nil
	}
	err = s.read(ctx, entry, func(c *webclass.Client) error {
		if !haveMaterials {
			m, err := c.FetchCourseContents(courseID)
			if err != nil {
				return err
			}
			res.Materials = m
			s.materials.set(key, m)
		}
		if !haveAttendance {
			res.Attendance, res.AttendanceErr = c.FetchAttendanceList(courseID)
			if res.AttendanceErr == nil {
				s.attendance.set(key, res.Attendance)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ContentFiles は資料 1 件のファイルを返す（FilesTTL の間キャッシュ）。資料以外は開かずに
// webclass.ErrNotMaterial を返す。
func (s *Service) ContentFiles(ctx context.Context, entry *session.Entry, courseID, contentID string) ([]webclass.FileInfo, error) {
	var files []webclass.FileInfo
	err := s.read(ctx, entry, func(c *webclass.Client) error {
		var err error
		files, err = s.contentFilesWith(ctx, c, entry, courseID, contentID)
		return err
	})
	return files, err
}

// CourseFilesWith はコースの資料と、資料ごとのファイル（ファイルの無い資料は含めない）を返す。
// すでに Open した client で使う（まとめてダウンロードなど、取得の後もセッションを使い続ける処理向け）。
func (s *Service) CourseFilesWith(ctx context.Context, c *webclass.Client, entry *session.Entry, courseID string) ([]webclass.ContentInfo, map[string][]webclass.FileInfo, error) {
	key := cacheKey(entry.Username, courseID)
	use, err := s.useCache(ctx, entry)
	if err != nil {
		return nil, nil, err
	}
	materials, ok := s.materials.get(key)
	if !ok || !use {
		var err error
		if materials, err = c.FetchCourseContents(courseID); err != nil {
			return nil, nil, err
		}
		s.materials.set(key, materials)
	}
	out := make(map[string][]webclass.FileInfo, len(materials))
	for _, m := range materials {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if files, _ := s.contentFilesWith(ctx, c, entry, courseID, m.ID); len(files) > 0 {
			out[m.ID] = files
		}
	}
	return materials, out, nil
}

func (s *Service) contentFilesWith(ctx context.Context, c *webclass.Client, entry *session.Entry, courseID, contentID string) ([]webclass.FileInfo, error) {
	key := cacheKey(entry.Username, courseID, contentID)
	use, err := s.useCache(ctx, entry)
	if err != nil {
		return nil, err
	}
	if v, ok := s.files.get(key); ok && use {
		return v, nil
	}
	files, err := c.FetchContentFiles(courseID, contentID)
	if err != nil {
		return nil, err
	}
	s.files.set(key, files)
	return files, nil
}

// Inbox は受信箱のメッセージ（自分が送ったものを除く）と、コースの自動通知を除いた未読数を返す。
// 新着を見逃さないよう、受信箱はキャッシュしない。未読数が取れなくても一覧は返す。
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
	case errors.Is(err, webclass.ErrSessionExpired), errors.Is(err, ErrNoPassword), errors.Is(err, ErrSessionInvalid):
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
