package apidoc

import (
	"os"
	"path/filepath"
	"testing"
)

// docs/api.md が経路の表と一致しているか。UPDATE_API_DOC=1 で書き直す（mise run gen-api-doc）。
func TestAPIDocIsUpToDate(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "api.md")
	want := Markdown()
	if os.Getenv("UPDATE_API_DOC") == "1" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("docs/api.md が経路の表とずれています。mise run gen-api-doc で生成し直してコミットしてください（err=%v）", err)
	}
}
