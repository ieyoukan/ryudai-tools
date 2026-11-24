package webclass

import (
	"errors"
	"strings"

	"golang.org/x/text/width"
)

// ErrEmailAsUsername は、大学以外のメールアドレスがユーザー名として入力されたときのエラー。
var ErrEmailAsUsername = errors.New("メールアドレスではなく、WebClass のユーザー名（琉大ID）を入力してください")

// universityDomain は、@ より前を WebClass のユーザー名とみなすメールアドレスのドメイン（サブドメインを含む）。
const universityDomain = "u-ryukyu.ac.jp"

// NormalizeUsername は、ログイン画面に入力された ID を WebClass のユーザー名にそろえる。
//   - 前後の空白（全角を含む）を除き、全角の英数字・記号を半角にする。
//   - 大学のメールアドレス（u-ryukyu.ac.jp とそのサブドメイン）なら、@ より前をユーザー名にする。
//   - それ以外のメールアドレスは ErrEmailAsUsername にする。大学のシステムへ送らないので、
//     失敗したログインの試行を増やさない。
//
// パスワードには使わない（入力どおりに送る）。
func NormalizeUsername(input string) (string, error) {
	s := strings.TrimSpace(width.Narrow.String(input))
	at := strings.LastIndex(s, "@")
	if at < 0 {
		return s, nil
	}
	local, domain := strings.TrimSpace(s[:at]), strings.ToLower(strings.TrimSpace(s[at+1:]))
	if local == "" || strings.Contains(local, "@") ||
		(domain != universityDomain && !strings.HasSuffix(domain, "."+universityDomain)) {
		return "", ErrEmailAsUsername
	}
	return local, nil
}
