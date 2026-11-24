package oauth

import (
	"errors"
	"net/http"

	"github.com/ieyoukan/ryudai-tools/internal/db"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// handleToken は認可コード・リフレッシュトークンをアクセストークンに交換する。
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "フォームを読み取れません")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r)
	case "refresh_token":
		s.refresh(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code と refresh_token のみ対応しています")
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	codeHash := hashToken(f.Get("code"))
	code, reused, err := s.Store.ConsumeOAuthCode(codeHash)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "認可コードが無効です")
		return
	}
	if reused {
		// 同じコードが 2 回使われた = 盗まれた可能性。発行済みの連携ごと失効させる。
		if code.GrantID != "" {
			s.Store.RevokeOAuthGrant("", code.GrantID)
		}
		logf("認可コードの再利用を検知（client=%s user=%s）", code.ClientID, code.UserID)
		oauthError(w, http.StatusBadRequest, "invalid_grant", "認可コードはすでに使われています")
		return
	}
	switch {
	case !s.now().Before(code.ExpiresAt):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "認可コードの期限が切れています")
		return
	case f.Get("client_id") != code.ClientID:
		oauthError(w, http.StatusBadRequest, "invalid_grant", "client_id が一致しません")
		return
	case f.Get("redirect_uri") != "" && f.Get("redirect_uri") != code.RedirectURI:
		oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri が一致しません")
		return
	case !verifyPKCE(f.Get("code_verifier"), code.CodeChallenge):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier が一致しません")
		return
	case f.Get("resource") != "" && !sameResource(f.Get("resource"), code.Resource):
		oauthError(w, http.StatusBadRequest, "invalid_target", "resource が認可時と一致しません")
		return
	}

	grantID, err := s.Store.ActiveOAuthGrant(code.UserID, code.ClientID)
	if errors.Is(err, db.ErrOAuthNotFound) {
		grantID = randomToken("g_")
		err = s.Store.CreateOAuthGrant(grantID, code.UserID, code.ClientID, code.Scope)
	}
	if err != nil {
		logf("連携の作成に失敗: %v", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "連携を保存できませんでした")
		return
	}
	s.Store.SetOAuthCodeGrant(codeHash, grantID)
	s.issueTokens(w, grantID, code.Resource, code.Scope)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	hash := hashToken(f.Get("refresh_token"))
	t, err := s.Store.GetOAuthToken(hash)
	if err != nil || t.Kind != "refresh" || t.Revoked {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "リフレッシュトークンが無効です")
		return
	}
	if f.Get("client_id") != "" && f.Get("client_id") != t.ClientID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "client_id が一致しません")
		return
	}
	if !s.now().Before(t.ExpiresAt) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "リフレッシュトークンの期限が切れています")
		return
	}
	if f.Get("resource") != "" && !sameResource(f.Get("resource"), t.Resource) {
		oauthError(w, http.StatusBadRequest, "invalid_target", "resource が一致しません")
		return
	}
	first, err := s.Store.MarkOAuthTokenUsed(hash)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "トークンを更新できませんでした")
		return
	}
	if !first {
		// 使用済みのリフレッシュトークンが再度使われた = 漏えいの疑い。連携ごと失効させる。
		s.Store.RevokeOAuthGrant("", t.GrantID)
		logf("リフレッシュトークンの再利用を検知（client=%s user=%s）", t.ClientID, t.UserID)
		oauthError(w, http.StatusBadRequest, "invalid_grant", "リフレッシュトークンはすでに使われています")
		return
	}
	s.issueTokens(w, t.GrantID, t.Resource, t.Scope)
}

func (s *Server) issueTokens(w http.ResponseWriter, grantID, resource, scope string) {
	access := randomToken(accessPrefix)
	refresh := randomToken(refreshPrefix)
	now := s.now()
	if err := s.Store.SaveOAuthToken(hashToken(access), grantID, "access", resource, now.Add(accessTTL)); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "トークンを保存できませんでした")
		return
	}
	if err := s.Store.SaveOAuthToken(hashToken(refresh), grantID, "refresh", resource, now.Add(refreshTTL)); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "トークンを保存できませんでした")
		return
	}
	s.Store.TouchOAuthGrant(grantID)
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(accessTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        scope,
	})
}

// handleRevoke はトークンを失効させる（RFC 7009）。存在しなくても 200 を返す。
// リフレッシュトークンなら連携ごと解除する。
func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "フォームを読み取れません")
		return
	}
	hash := hashToken(r.PostForm.Get("token"))
	if t, err := s.Store.GetOAuthToken(hash); err == nil {
		if t.Kind == "refresh" {
			s.Store.RevokeOAuthGrant("", t.GrantID)
		} else {
			s.Store.DeleteOAuthToken(hash)
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
}
