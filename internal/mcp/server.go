// Package mcp は Model Context Protocol の最小サーバー実装（Streamable HTTP / JSON 応答 / ステートレス）。
//
// 対応メソッド: initialize, ping, tools/list, tools/call, notifications/*。
// SSE ストリームやサーバー起点の通知は使わないので GET は 405 を返す。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
)

// LatestProtocolVersion はこのサーバーが名乗る最新の MCP プロトコルバージョン。
const LatestProtocolVersion = "2025-06-18"

var supportedVersions = map[string]bool{
	"2025-06-18": true,
	"2025-03-26": true,
	"2024-11-05": true,
}

// Tool は AI から呼べる 1 つの機能。
type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema map[string]any
	// RequiresUser が true なら認証済みユーザーがいないと呼べない。
	RequiresUser bool
	// Annotations は安全性のヒント（readOnlyHint / destructiveHint / openWorldHint）。
	Annotations map[string]any
	// SecuritySchemes は ChatGPT 等に伝える認証方式（例: [{"type":"noauth"},{"type":"oauth2","scopes":[...]}]）。
	SecuritySchemes []map[string]any
	// Handler は引数 JSON を受け取り、JSON にできる結果を返す。user は未認証なら ""。
	Handler func(ctx context.Context, user string, args json.RawMessage) (any, error)
}

// Server は MCP の HTTP ハンドラ。
type Server struct {
	Name         string
	Version      string
	Instructions string
	// Authenticate はリクエストからユーザー名を返す。トークンが無ければ ("", nil)、
	// トークンがあるのに無効・期限切れならエラー（HTTP 401 を返してクライアントに再認可を促す）。
	Authenticate func(r *http.Request) (string, error)
	// Challenge は WWW-Authenticate ヘッダーの値を返す（OAuth 対応時のみ。nil なら付けない）。
	Challenge func(r *http.Request) string
	// AuthHint は未認証で個人向けツールを呼んだときに返す案内文。
	AuthHint string

	tools []Tool
	index map[string]*Tool
}

// AddTool はツールを登録する。
func (s *Server) AddTool(t Tool) {
	s.tools = append(s.tools, t)
	// append でスライスが再確保されるとポインタがずれるので毎回作り直す
	s.index = make(map[string]*Tool, len(s.tools))
	for i := range s.tools {
		s.index[s.tools[i].Name] = &s.tools[i]
	}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	// ブラウザからの呼び出しは同一オリジンのみ許可する（DNS リバインディング対策）。
	if origin := r.Header.Get("Origin"); origin != "" {
		if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
	}
	switch r.Method {
	case http.MethodPost:
	case http.MethodDelete:
		// ステートレスなので終了すべきセッションは無い
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "this MCP server only accepts POST (no SSE stream)", http.StatusMethodNotAllowed)
		return
	}

	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeParseError, "invalid JSON"}})
		return
	}
	user := ""
	challenge := ""
	if s.Challenge != nil {
		challenge = s.Challenge(r)
	}
	if s.Authenticate != nil {
		u, err := s.Authenticate(r)
		if err != nil {
			if challenge != "" {
				w.Header().Set("WWW-Authenticate", challenge)
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token", "error_description": err.Error()})
			return
		}
		user = u
	}

	// バッチ（2025-03-26 まで）にも対応する
	if len(raw) > 0 && raw[0] == '[' {
		var reqs []request
		if err := json.Unmarshal(raw, &reqs); err != nil {
			writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeParseError, "invalid batch"}})
			return
		}
		var out []response
		for _, req := range reqs {
			if resp, ok := s.handle(r.Context(), user, challenge, req); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeInvalidRequest, "invalid request"}})
		return
	}
	resp, ok := s.handle(r.Context(), user, challenge, req)
	if !ok {
		w.WriteHeader(http.StatusAccepted) // 通知・レスポンスには返事をしない
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handle は 1 メッセージを処理する。返事が不要（通知）なら ok=false。
func (s *Server) handle(ctx context.Context, user, challenge string, req request) (response, bool) {
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return response{}, false
	}
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		version := LatestProtocolVersion
		if supportedVersions[p.ProtocolVersion] {
			version = p.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
			"instructions":    s.Instructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		tools := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			m := map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema}
			if t.Title != "" {
				m["title"] = t.Title
			}
			if t.SecuritySchemes != nil {
				m["securitySchemes"] = t.SecuritySchemes
			}
			if t.Annotations != nil {
				m["annotations"] = t.Annotations
			}
			tools = append(tools, m)
		}
		resp.Result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{codeInvalidParams, "invalid params"}
			break
		}
		t, ok := s.index[p.Name]
		if !ok {
			resp.Error = &rpcError{codeInvalidParams, "unknown tool: " + p.Name}
			break
		}
		resp.Result = s.callTool(ctx, t, user, challenge, p.Arguments)
	default:
		resp.Error = &rpcError{codeMethodNotFound, "method not found: " + req.Method}
	}
	return resp, true
}

func (s *Server) callTool(ctx context.Context, t *Tool, user, challenge string, args json.RawMessage) map[string]any {
	if t.RequiresUser && user == "" {
		res := toolError(s.AuthHint)
		if challenge != "" {
			// ChatGPT はこれを見てアカウント連携（OAuth）を案内する
			res["_meta"] = map[string]any{"mcp/www_authenticate": []string{challenge}}
		}
		return res
	}
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	result, err := func() (res any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("MCP ツール %s で panic: %v", t.Name, r)
				err = fmt.Errorf("内部エラー")
			}
		}()
		return t.Handler(ctx, user, args)
	}()
	if err != nil {
		return toolError(err.Error())
	}
	b, err := json.Marshal(result)
	if err != nil {
		return toolError("結果の JSON 化に失敗: " + err.Error())
	}
	out := map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(b)}},
	}
	// structuredContent はオブジェクトである必要がある
	if len(b) > 0 && b[0] == '{' {
		out["structuredContent"] = json.RawMessage(b)
	}
	return out
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}
