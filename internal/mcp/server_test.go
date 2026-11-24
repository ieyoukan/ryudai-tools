package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
)

func testServer() *Server {
	s := &Server{
		Name: "test", Version: "0", AuthHint: "要認証",
		Authenticate: func(r *http.Request) (string, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer ok":
				return "student", nil
			case "":
				return "", nil
			}
			return "", errors.New("invalid")
		},
		Challenge: func(*http.Request) string {
			return `Bearer resource_metadata="https://x/.well-known/oauth-protected-resource"`
		},
	}
	s.AddTool(Tool{
		Name:        "echo",
		InputSchema: objectSchema(map[string]any{"msg": str("")}),
		Handler: func(_ context.Context, user string, raw json.RawMessage) (any, error) {
			var a struct{ Msg string }
			json.Unmarshal(raw, &a)
			return map[string]string{"msg": a.Msg, "user": user}, nil
		},
	})
	s.AddTool(Tool{
		Name: "private", RequiresUser: true, InputSchema: objectSchema(map[string]any{}),
		Handler: func(_ context.Context, user string, _ json.RawMessage) (any, error) {
			return map[string]string{"user": user}, nil
		},
	})
	return s
}

func call(t *testing.T, s *Server, body, auth string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("応答が JSON でない: %s", rec.Body.String())
		}
	}
	return rec.Code, out
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	s := testServer()
	_, out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`, "")
	res := out["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" {
		t.Errorf("protocolVersion = %v", res["protocolVersion"])
	}
	_, out = call(t, s, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`, "")
	if out["result"].(map[string]any)["protocolVersion"] != LatestProtocolVersion {
		t.Error("未知のバージョンには最新を返すべき")
	}
}

func TestNotificationReturns202(t *testing.T) {
	code, out := call(t, testServer(), `{"jsonrpc":"2.0","method":"notifications/initialized"}`, "")
	if code != http.StatusAccepted || out != nil {
		t.Errorf("code = %d, body = %v", code, out)
	}
}

func TestToolsListAndCall(t *testing.T) {
	s := testServer()
	_, out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "")
	tools := out["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v", tools)
	}
	_, out = call(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"こんにちは"}}}`, "")
	res := out["result"].(map[string]any)
	if res["isError"] != nil {
		t.Fatalf("isError: %v", res)
	}
	sc := res["structuredContent"].(map[string]any)
	if sc["msg"] != "こんにちは" || sc["user"] != "" {
		t.Errorf("structuredContent = %v", sc)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "こんにちは") {
		t.Errorf("text = %q", text)
	}
}

func TestPrivateToolRequiresAuth(t *testing.T) {
	s := testServer()
	_, out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"private"}}`, "")
	res := out["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "要認証") {
		t.Errorf("未認証で %v", res)
	}
	_, out = call(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"private"}}`, "Bearer ok")
	if out["result"].(map[string]any)["structuredContent"].(map[string]any)["user"] != "student" {
		t.Errorf("認証済みで %v", out)
	}
}

func TestUnknownMethodAndTool(t *testing.T) {
	s := testServer()
	_, out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, "")
	if out["error"].(map[string]any)["code"].(float64) != codeMethodNotFound {
		t.Errorf("out = %v", out)
	}
	_, out = call(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope"}}`, "")
	if out["error"] == nil {
		t.Errorf("未知のツールでエラーにならない: %v", out)
	}
}

func TestBatchAndGet(t *testing.T) {
	s := testServer()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/x"}]`))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 1 {
		t.Errorf("batch = %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d", rec.Code)
	}
}

func TestAcademicServerTools(t *testing.T) {
	s := NewAcademicServer(academic.New("", nil), "test", func(*http.Request) (string, error) { return "", nil }, nil)
	names := map[string]bool{}
	for _, tl := range s.tools {
		names[tl.Name] = true
		if tl.InputSchema["type"] != "object" {
			t.Errorf("%s の inputSchema が object でない", tl.Name)
		}
	}
	for _, tl := range s.tools {
		if tl.Annotations["readOnlyHint"] != true || tl.Annotations["destructiveHint"] != false {
			t.Errorf("%s の安全アノテーションが無い: %v", tl.Name, tl.Annotations)
		}
	}
	for _, want := range []string{"search_syllabus", "get_syllabus", "list_syllabus_options", "get_my_registration", "get_my_schedule", "get_my_lecture_schedule", "find_available_lectures", "describe_lectures", "get_graduation_status", "check_registration_plan", "get_academic_calendar", "get_course_timetable", "get_study_rules", "get_required_courses", "check_my_required_courses"} {
		if !names[want] {
			t.Errorf("ツール %s が無い", want)
		}
	}
	// 条件なし検索は教務システムに問い合わせずエラーになる
	_, scheduleOut := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_my_schedule","arguments":{}}}`, "")
	if scheduleOut["result"].(map[string]any)["isError"] != true {
		t.Fatal("personal schedule must need authentication", scheduleOut)
	}
	_, requiredOut := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"check_my_required_courses","arguments":{}}}`, "")
	if requiredOut["result"].(map[string]any)["isError"] != true {
		t.Fatal("personal required subjects must need authentication", requiredOut)
	}
	_, out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_syllabus","arguments":{}}}`, "")
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("空条件で %v", out)
	}
}

func TestInvalidTokenReturns401WithChallenge(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer expired")
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
	}
}

func TestPrivateToolAdvertisesOAuthChallenge(t *testing.T) {
	_, out := call(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"private"}}`, "")
	meta, _ := out["result"].(map[string]any)["_meta"].(map[string]any)
	if meta == nil || meta["mcp/www_authenticate"] == nil {
		t.Errorf("_meta に mcp/www_authenticate が無い: %v", out)
	}
}
