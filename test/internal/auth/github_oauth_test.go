package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"comment-api/internal/auth"
)

// ── Login ──────────────────────────────────────────────────────────────────

func TestLogin_SetsStateCookieAndRedirectsToGitHub(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := auth.NewGitHubHandler(baseConfig(), rdb)

	req := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	assert.Equal(t, http.StatusFound, rr.Code)

	location := rr.Header().Get("Location")
	assert.Contains(t, location, "github.com/login/oauth/authorize")
	assert.Contains(t, location, "client_id=test-client-id")
	assert.Contains(t, location, "state=")

	cookie := findCookie(rr, auth.OAuthStateCookie)
	require.NotNil(t, cookie)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, 300, cookie.MaxAge)
	assert.NotEmpty(t, cookie.Value)

	// state 쿼리파라미터와 쿠키값이 일치해야 함
	stateInURL := extractQueryParam(t, location, "state")
	assert.Equal(t, cookie.Value, stateInURL)
}

func TestLogin_SavesStateToRedis(t *testing.T) {
	rdb, mr := newTestRedis(t)
	h := auth.NewGitHubHandler(baseConfig(), rdb)

	req := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	cookie := findCookie(rr, auth.OAuthStateCookie)
	require.NotNil(t, cookie)
	assert.True(t, mr.Exists(auth.OAuthStateKeyPrefix+cookie.Value))
}

// ── Callback — 실패 케이스 ──────────────────────────────────────────────────

func TestCallback_MissingParams_Returns400(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := auth.NewGitHubHandler(baseConfig(), rdb)

	tests := []struct {
		name string
		url  string
	}{
		{"state만 없음", "/auth/github/callback?code=xxx"},
		{"code만 없음", "/auth/github/callback?state=abc"},
		{"둘 다 없음", "/auth/github/callback"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rr := httptest.NewRecorder()
			h.Callback(rr, req)
			assert.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
}

func TestCallback_StateCookieMismatch_Returns400(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := auth.NewGitHubHandler(baseConfig(), rdb)

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state=url-state&code=xxx", nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthStateCookie, Value: "different-state"})
	rr := httptest.NewRecorder()

	h.Callback(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallback_NoCookieAtAll_Returns400(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := auth.NewGitHubHandler(baseConfig(), rdb)

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state=some-state&code=xxx", nil)
	rr := httptest.NewRecorder()

	h.Callback(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallback_StateNotInRedis_Returns400(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := auth.NewGitHubHandler(baseConfig(), rdb)

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state=orphan-state&code=xxx", nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthStateCookie, Value: "orphan-state"})
	rr := httptest.NewRecorder()

	h.Callback(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── Callback — 정상 케이스 ─────────────────────────────────────────────────

func TestCallback_HappyPath_SetsSessionCookieAndClearsState(t *testing.T) {
	rdb, mr := newTestRedis(t)

	// GitHub 사용자 API 목서버
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": 99999, "login": "octocat",
			"email": "octocat@github.com", "avatar_url": "https://avatars.github.com/u/99999",
		})
	}))
	defer apiServer.Close()

	// GitHub OAuth 토큰 목서버
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token", "token_type": "bearer"})
	}))
	defer tokenServer.Close()

	const state = "test-state-uuid"
	mr.Set(auth.OAuthStateKeyPrefix+state, "1")

	h := auth.NewGitHubHandler(baseConfig(), rdb)
	h.SetGitHubAPIURL(apiServer.URL)
	h.SetOAuthEndpoint(oauth2.Endpoint{TokenURL: tokenServer.URL})

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+state+"&code=test-code", nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthStateCookie, Value: state})
	rr := httptest.NewRecorder()

	h.Callback(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// COMMENT_SESSION 쿠키 발급 확인
	sessionCookie := findCookie(rr, "COMMENT_SESSION")
	require.NotNil(t, sessionCookie, "COMMENT_SESSION 쿠키가 발급되어야 함")
	assert.NotEmpty(t, sessionCookie.Value)
	assert.True(t, sessionCookie.HttpOnly)
	assert.Equal(t, 600, sessionCookie.MaxAge)

	// OAUTH_STATE 쿠키 삭제 확인 (Set-Cookie 헤더에 Max-Age=0 포함)
	rawHeaders := strings.Join(rr.Header()["Set-Cookie"], "\n")
	assert.Contains(t, rawHeaders, "OAUTH_STATE=")
	assert.Contains(t, rawHeaders, "Max-Age=0")

	// Redis 세션 저장 확인
	assert.True(t, mr.Exists(auth.SessionKeyPrefix+sessionCookie.Value))

	// OAuth state Redis 키 삭제 확인
	assert.False(t, mr.Exists(auth.OAuthStateKeyPrefix+state))
}

func TestCallback_HappyPath_WithAuthSuccessURL_Redirects(t *testing.T) {
	rdb, mr := newTestRedis(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "login": "user"})
	}))
	defer apiServer.Close()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok", "token_type": "bearer"})
	}))
	defer tokenServer.Close()

	const state = "redir-state"
	mr.Set(auth.OAuthStateKeyPrefix+state, "1")

	cfg := baseConfig()
	cfg.AuthSuccessURL = "https://example.com/home"

	h := auth.NewGitHubHandler(cfg, rdb)
	h.SetGitHubAPIURL(apiServer.URL)
	h.SetOAuthEndpoint(oauth2.Endpoint{TokenURL: tokenServer.URL})

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+state+"&code=code", nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthStateCookie, Value: state})
	rr := httptest.NewRecorder()

	h.Callback(rr, req)

	assert.Equal(t, http.StatusFound, rr.Code)
	assert.Equal(t, "https://example.com/home", rr.Header().Get("Location"))
}

// ── Logout ─────────────────────────────────────────────────────────────────

func TestLogout_WithCommentSession_DeletesSessionAndExpiresCookie(t *testing.T) {
	rdb, mr := newTestRedis(t)
	mr.Set(auth.SessionKeyPrefix+"sid", `{"userId":"1"}`)

	h := auth.NewGitHubHandler(baseConfig(), rdb)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "sid"})
	rr := httptest.NewRecorder()

	h.Logout(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.False(t, mr.Exists(auth.SessionKeyPrefix+"sid"), "Redis 세션이 삭제되어야 함")

	rawHeaders := strings.Join(rr.Header()["Set-Cookie"], "\n")
	assert.Contains(t, rawHeaders, "COMMENT_SESSION=")
	assert.Contains(t, rawHeaders, "Max-Age=0")
}

func TestLogout_AdminSession_Returns403(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := auth.NewGitHubHandler(baseConfig(), rdb)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rr := httptest.NewRecorder()

	h.Logout(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// ── 헬퍼 ───────────────────────────────────────────────────────────────────

func extractQueryParam(t *testing.T, rawURL, key string) string {
	t.Helper()
	idx := strings.Index(rawURL, "?")
	if idx == -1 {
		return ""
	}
	for _, param := range strings.Split(rawURL[idx+1:], "&") {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) == 2 && parts[0] == key {
			return parts[1]
		}
	}
	return ""
}
