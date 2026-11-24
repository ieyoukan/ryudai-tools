package db

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAPITokenLifecycle(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	token, err := database.CreateAPIToken("student", "Claude Desktop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "dh_") || len(token) < 40 {
		t.Errorf("token = %q", token)
	}
	user, err := database.UserForAPIToken(token)
	if err != nil || user != "student" {
		t.Fatalf("UserForAPIToken = %q, %v", user, err)
	}
	if _, err := database.UserForAPIToken(token + "x"); err != ErrTokenNotFound {
		t.Errorf("不正トークンで err = %v", err)
	}
	list, err := database.ListAPITokens("student")
	if err != nil || len(list) != 1 || list[0].Name != "Claude Desktop" || list[0].LastUsedAt.IsZero() {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if strings.Contains(list[0].Prefix, token[10:]) {
		t.Error("一覧に平文全体が含まれている")
	}
	if err := database.DeleteAPIToken("other", list[0].ID); err != ErrTokenNotFound {
		t.Errorf("他人のトークンを削除できてしまう: %v", err)
	}
	if err := database.DeleteAPIToken("student", list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UserForAPIToken(token); err != ErrTokenNotFound {
		t.Errorf("失効後も使える: %v", err)
	}
}
