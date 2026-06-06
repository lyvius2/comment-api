package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"comment-api/config"
	"comment-api/pkg/response"
)

const OAuthStateCookie = "OAUTH_STATE"
const oauthStateTTL = 5 * time.Minute

type GitHubHandler struct {
	cfg          *config.Config
	oauthConfig  *oauth2.Config
	rdb          *redis.Client
	githubAPIURL string // 테스트에서 오버라이드 가능
}

func NewGitHubHandler(cfg *config.Config, rdb *redis.Client) *GitHubHandler {
	return &GitHubHandler{
		cfg: cfg,
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			RedirectURL:  cfg.GitHubCallbackURL,
			Scopes:       []string{"user:email"},
			Endpoint:     github.Endpoint,
		},
		rdb:          rdb,
		githubAPIURL: "https://api.github.com/user",
	}
}

// Login godoc
// @Summary      GitHub OAuth 로그인 시작
// @Description  GitHub OAuth 인증 페이지로 리다이렉트합니다. OAUTH_STATE 쿠키가 발급됩니다.
// @Tags         auth
// @Success      302
// @Failure      500 {object} response.Response
// @Router       /auth/github [get]
func (h *GitHubHandler) Login(w http.ResponseWriter, r *http.Request) {
	state := uuid.NewString()

	if err := saveOAuthState(r.Context(), h.rdb, state, oauthStateTTL); err != nil {
		slog.Error("failed to save oauth state", "error", err)
		response.Error(w, http.StatusInternalServerError, "인증 요청 처리 중 오류가 발생했습니다.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookie,
		Value:    state,
		HttpOnly: true,
		Secure:   h.isProduction(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
		Path:     "/",
	})

	http.Redirect(w, r, h.oauthConfig.AuthCodeURL(state), http.StatusFound)
}

// Callback godoc
// @Summary      GitHub OAuth 콜백 처리
// @Description  GitHub에서 받은 code와 state로 토큰 교환 후 COMMENT_SESSION 쿠키를 발급합니다. AUTH_SUCCESS_URL 설정 시 해당 URL로 리다이렉트합니다.
// @Tags         auth
// @Param        code   query  string  true  "GitHub OAuth authorization code"
// @Param        state  query  string  true  "CSRF state 값"
// @Success      302
// @Success      200  {object}  response.Response
// @Failure      400  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /auth/github/callback [get]
func (h *GitHubHandler) Callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if state == "" || code == "" {
		response.Error(w, http.StatusBadRequest, "잘못된 요청입니다.")
		return
	}

	// ① 쿠키 OAUTH_STATE == state 검증 (클라이언트 바인딩)
	stateCookie, err := r.Cookie(OAuthStateCookie)
	if err != nil || stateCookie.Value != state {
		response.Error(w, http.StatusBadRequest, "state 검증 실패.")
		return
	}

	// ② Redis state 검증 및 삭제 (재사용 방지)
	if err := validateAndDeleteOAuthState(r.Context(), h.rdb, state); err != nil {
		slog.Error("oauth state validation failed", "error", err)
		response.Error(w, http.StatusBadRequest, "state 검증 실패.")
		return
	}

	// GitHub 액세스 토큰 교환
	token, err := h.oauthConfig.Exchange(r.Context(), code)
	if err != nil {
		slog.Error("oauth token exchange failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "인증 처리 중 오류가 발생했습니다.")
		return
	}

	// GitHub 사용자 정보 조회
	user, err := h.fetchGitHubUser(r.Context(), token.AccessToken)
	if err != nil {
		slog.Error("failed to fetch github user", "error", err)
		response.Error(w, http.StatusInternalServerError, "사용자 정보 조회 중 오류가 발생했습니다.")
		return
	}

	// 세션 생성 및 Redis 저장
	sessionID := uuid.NewString()
	session := &CommentSession{
		UserID:    fmt.Sprintf("%d", user.ID),
		Email:     user.Email,
		Username:  user.Login,
		AvatarURL: user.AvatarURL,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	ttl := time.Duration(h.cfg.SessionTTLSeconds) * time.Second
	if err := SaveSession(r.Context(), h.rdb, sessionID, session, ttl); err != nil {
		slog.Error("failed to save session", "error", err)
		response.Error(w, http.StatusInternalServerError, "세션 생성 중 오류가 발생했습니다.")
		return
	}

	// COMMENT_SESSION 쿠키 발급
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.CommentSessionCookie,
		Value:    sessionID,
		HttpOnly: true,
		Secure:   h.isProduction(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   h.cfg.SessionTTLSeconds,
		Path:     "/",
		Domain:   h.cfg.SessionCookieDomain,
	})

	// OAUTH_STATE 임시 쿠키 삭제 (MaxAge: -1 → Set-Cookie: Max-Age=0)
	http.SetCookie(w, &http.Cookie{
		Name:   OAuthStateCookie,
		Value:  "",
		MaxAge: -1,
		Path:   "/",
	})

	if h.cfg.AuthSuccessURL != "" {
		http.Redirect(w, r, h.cfg.AuthSuccessURL, http.StatusFound)
		return
	}

	response.Success(w, http.StatusOK, map[string]string{
		"username":  user.Login,
		"avatarUrl": user.AvatarURL,
	})
}

type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (h *GitHubHandler) fetchGitHubUser(ctx context.Context, accessToken string) (*githubUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.githubAPIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode github user: %w", err)
	}
	return &user, nil
}

// Logout godoc
// @Summary      로그아웃
// @Description  COMMENT_SESSION 쿠키를 만료시키고 Redis에서 세션을 삭제합니다. 관리자 세션(LIFELOG_SESSION)은 대상이 아닙니다.
// @Tags         auth
// @Security     CommentSession
// @Success      204
// @Failure      403  {object}  response.Response
// @Router       /auth/logout [post]
func (h *GitHubHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(h.cfg.CommentSessionCookie)
	if err != nil {
		// 관리자 세션(LIFELOG_SESSION)은 Go 로그아웃 대상이 아님
		response.Error(w, http.StatusForbidden, "관리자 세션은 로그아웃할 수 없습니다.")
		return
	}

	if err := DeleteSession(r.Context(), h.rdb, cookie.Value); err != nil {
		slog.Error("failed to delete session", "error", err)
		// 삭제 실패해도 클라이언트 쿠키는 반드시 만료 처리
	}

	// COMMENT_SESSION 쿠키 삭제 (MaxAge: -1 → Set-Cookie: Max-Age=0)
	http.SetCookie(w, &http.Cookie{
		Name:   h.cfg.CommentSessionCookie,
		Value:  "",
		MaxAge: -1,
		Path:   "/",
	})

	w.WriteHeader(http.StatusNoContent)
}

// SetGitHubAPIURL은 테스트에서 GitHub API URL을 목서버로 교체할 때 사용합니다.
func (h *GitHubHandler) SetGitHubAPIURL(url string) { h.githubAPIURL = url }

// SetOAuthEndpoint는 테스트에서 OAuth 토큰 엔드포인트를 목서버로 교체할 때 사용합니다.
func (h *GitHubHandler) SetOAuthEndpoint(endpoint oauth2.Endpoint) {
	h.oauthConfig.Endpoint = endpoint
}

func (h *GitHubHandler) isProduction() bool {
	return h.cfg.AppEnv == "production"
}
