package webclass

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrUnexpectedPage は、WebClass から期待したページ（コースのページなど）ではないものが返ったことを示す。
// 空の一覧として扱うと「資料がない」と区別できなくなるため、取得の失敗として返す。
var ErrUnexpectedPage = errors.New("WebClass から想定外のページが返りました")

var pageTitlePattern = regexp.MustCompile(`(?is)<title>\s*(.*?)\s*</title>`)

// isLoginPage はログイン画面、またはログイン画面への転送ページか。
func isLoginPage(body []byte) bool {
	return loginNavigation.Match(body) || (usernameInput.Match(body) && passwordInput.Match(body))
}

// pageTitle はエラーの手がかりにするページの <title>（無ければ空）。
func pageTitle(body []byte) string {
	if m := pageTitlePattern.FindSubmatch(body); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// checkLoggedIn はログイン画面が返っていないかを確かめる。
func checkLoggedIn(body []byte) error {
	if isLoginPage(body) {
		return ErrSessionExpired
	}
	return nil
}

// checkCoursePage は courseID のコースのページが返ったかを確かめる。資料の無いコースでもページには
// コース ID が含まれるので、含まれなければログイン画面・エラー画面・別のコースのいずれかとみなす。
func checkCoursePage(body []byte, courseID string) error {
	if err := checkLoggedIn(body); err != nil {
		return err
	}
	if !bytes.Contains(body, []byte(courseID)) {
		return fmt.Errorf("%w（コース %s のページではありません: %q）", ErrUnexpectedPage, courseID, pageTitle(body))
	}
	return nil
}
