package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/ieyoukan/daigaku-hack/internal/config"
	"github.com/ieyoukan/daigaku-hack/internal/provider/webclass"
	"github.com/ieyoukan/daigaku-hack/internal/session"
)

type AuthHandler struct {
	Config       *config.Config
	SessionStore session.Store
	// Academic があればログイン時に教務システムにもログインし、ログアウトでそのセッションを破棄する。
	Academic AcademicSessions
}

// AcademicSessions は教務システムのセッション管理（academic.Service が実装）。
// パスワードは保存しないので、ログインのときに受け取ったパスワードでだけ教務システムにログインできる。
type AcademicSessions interface {
	StartSession(username, password string) error
	Forget(username string)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	SessionID string `json:"session_id"`
	Username  string `json:"username"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *AuthHandler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
		return
	}

	if req.Username == "" || req.Password == "" {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "username and password are required"})
		return
	}

	client, err := webclass.New(h.Config.WebClassBaseURL)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "failed to create scraper client"})
		return
	}

	cookies, err := client.Login(req.Username, req.Password)
	if err != nil {
		h.writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "login failed: " + err.Error()})
		return
	}

	if h.Academic != nil {
		if err := h.Academic.StartSession(req.Username, req.Password); err != nil {
			log.Printf("教務システムへのログイン失敗 (%s): %v", req.Username, err)
		}
	}

	sessionID := h.SessionStore.Create(req.Username, cookies)

	h.writeJSON(w, http.StatusOK, loginResponse{
		SessionID: sessionID,
		Username:  req.Username,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "missing X-Session-ID header"})
		return
	}

	if entry, ok := h.SessionStore.Get(sessionID); ok && h.Academic != nil {
		h.Academic.Forget(entry.Username)
	}
	h.SessionStore.Delete(sessionID)
	h.writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *AuthHandler) Session(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		h.writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "missing X-Session-ID header"})
		return
	}

	entry, ok := h.SessionStore.Get(sessionID)
	if !ok {
		h.writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "invalid or expired session"})
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"session_id": entry.SessionID,
		"username":   entry.Username,
		"created_at": entry.CreatedAt,
	})
}
