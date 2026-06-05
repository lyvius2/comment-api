package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"comment-api/config"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

func baseConfig() *config.Config {
	return &config.Config{
		AppEnv:               "development",
		CommentSessionCookie: "COMMENT_SESSION",
		LifelogSessionCookie: "LIFELOG_SESSION",
		LifelogSessionAttr:   "loginMember",
		SessionTTLSeconds:    600,
		GitHubClientID:       "test-client-id",
		GitHubClientSecret:   "test-secret",
		GitHubCallbackURL:    "http://localhost/auth/github/callback",
	}
}

// findCookie는 ResponseRecorder의 Set-Cookie 헤더에서 쿠키를 찾습니다.
func findCookie(rr *httptest.ResponseRecorder, name string) *http.Cookie {
	resp := &http.Response{Header: rr.Header()}
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
