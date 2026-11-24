package db

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ieyoukan/ryudai-tools/internal/session"
)

func TestKyomuCookiesRoundTrip(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if got, err := database.KyomuCookies("student"); err != nil || got != nil {
		t.Fatalf("empty cookies = %v, err %v", got, err)
	}
	if err := database.SaveKyomuCookies("student", []*http.Cookie{{Name: "ASP.NET_SessionId", Value: "v", Path: "/"}}); err != nil {
		t.Fatal(err)
	}
	got, err := database.KyomuCookies("student")
	if err != nil || len(got) != 1 || got[0].Name != "ASP.NET_SessionId" || got[0].Value != "v" {
		t.Fatalf("cookies = %v, err %v", got, err)
	}
	if err := database.DeleteKyomuCookies("student"); err != nil {
		t.Fatal(err)
	}
	if got, _ := database.KyomuCookies("student"); got != nil {
		t.Fatalf("cookies after delete = %v", got)
	}
}

func TestOpenDropsSavedPasswords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, wc_username_enc TEXT, wc_password_enc TEXT);
		INSERT INTO users VALUES ('student', 'u', 'p');
		CREATE TABLE settings (user_id TEXT, key TEXT, value TEXT);
		INSERT INTO settings VALUES ('student', 'some_key', 'some_value')`); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, table := range []string{"users", "settings"} {
		var n int
		if err := database.sql.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("table %s remains (count %d, err %v)", table, n, err)
		}
	}
}

func TestCookiesEncryptedAtRest(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetCookieKey(make([]byte, 32))

	// セッションを先に作る（起動直後の掃除で、セッションの無いユーザーの教務 Cookie が消されないように）
	id := database.NewSessionStore().Create("student", []*http.Cookie{{Name: "WCAC", Value: "secret-value"}})
	if err := database.SaveKyomuCookies("student", []*http.Cookie{{Name: "ASP.NET_SessionId", Value: "secret-value"}}); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		`SELECT cookies_json FROM kyomu_sessions WHERE user_id = 'student'`,
		`SELECT cookies_json FROM sessions WHERE id = '` + id + `'`,
	} {
		var stored string
		if err := database.sql.QueryRow(q).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stored, "secret-value") {
			t.Fatalf("cookie stored in plaintext: %s", stored)
		}
	}
	got, err := database.KyomuCookies("student")
	if err != nil || len(got) != 1 || got[0].Value != "secret-value" {
		t.Fatalf("cookies = %v, err %v", got, err)
	}
}

func TestDecodeCookiesReadsLegacyPlaintext(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetCookieKey(make([]byte, 32))

	if _, err := database.sql.Exec(`INSERT INTO kyomu_sessions (user_id, cookies_json, updated_at) VALUES ('student', '[{"name":"A","value":"v"}]', 0)`); err != nil {
		t.Fatal(err)
	}
	got, err := database.KyomuCookies("student")
	if err != nil || len(got) != 1 || got[0].Value != "v" {
		t.Fatalf("legacy cookies = %v, err %v", got, err)
	}
}

func TestDeleteExpiredSessionsRemovesOrphanKyomuCookies(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	store := database.NewSessionStore()
	store.Create("Active", nil)
	for _, user := range []string{"active", "gone"} {
		if err := database.SaveKyomuCookies(user, []*http.Cookie{{Name: "A", Value: "v"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.DeleteExpiredSessions(time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := database.KyomuCookies("active"); got == nil {
		t.Fatal("cookies of a user with a live session were deleted")
	}
	if got, _ := database.KyomuCookies("gone"); got != nil {
		t.Fatal("cookies of a user without a session were kept")
	}

	// セッションが期限切れになれば、そのユーザーの Cookie も消える
	if err := database.DeleteExpiredSessions(time.Now().Add(session.IdleTimeout + time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := database.KyomuCookies("active"); got != nil {
		t.Fatal("cookies were kept after the session expired")
	}
}

// 旧版で secure_delete なしに DROP した認証情報の残骸が、Open 後のファイルに残らないこと。
func TestOpenWipesDroppedCredentialRemnants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("ENCRYPTED-PASSWORD-REMNANT", 20)
	for _, q := range []string{
		"PRAGMA journal_mode=WAL",
		"CREATE TABLE users (id TEXT PRIMARY KEY, wc_username_enc TEXT, wc_password_enc TEXT)",
		// 起動時の CREATE TABLE が空きページを再利用して偶然上書きしないよう、多数のページにまたがらせる
		"WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 300) " +
			"INSERT INTO users SELECT 'student' || i, 'u', '" + secret + "' FROM n",
		"PRAGMA wal_checkpoint(TRUNCATE)",
		"DROP TABLE users",
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "ENCRYPTED-PASSWORD-REMNANT") {
			t.Fatalf("dropped credentials remain in %s", f)
		}
	}
}
