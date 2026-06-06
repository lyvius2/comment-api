package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"

	"comment-api/router"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func doRequest(handler http.Handler, ip string) int {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip + ":12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Code
}

// ── 기본 동작 ─────────────────────────────────────────────────────────────────

func TestRateLimitMiddleware_UnderLimit_PassesThrough(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := router.RateLimitMiddleware(rdb)(okHandler())

	for i := 0; i < 60; i++ {
		assert.Equal(t, http.StatusOK, doRequest(h, "1.2.3.4"),
			"60번째 요청까지 통과해야 함 (요청 %d)", i+1)
	}
}

func TestRateLimitMiddleware_OverLimit_Returns429(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := router.RateLimitMiddleware(rdb)(okHandler())

	for i := 0; i < 60; i++ {
		doRequest(h, "1.2.3.4")
	}

	assert.Equal(t, http.StatusTooManyRequests, doRequest(h, "1.2.3.4"),
		"61번째 요청은 429여야 함")
}

func TestRateLimitMiddleware_DifferentIPs_SeparateCounters(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := router.RateLimitMiddleware(rdb)(okHandler())

	for i := 0; i < 60; i++ {
		doRequest(h, "1.1.1.1")
	}

	// 1.1.1.1 은 한도 초과
	assert.Equal(t, http.StatusTooManyRequests, doRequest(h, "1.1.1.1"))
	// 2.2.2.2 는 독립적이므로 통과
	assert.Equal(t, http.StatusOK, doRequest(h, "2.2.2.2"))
}

func TestRateLimitMiddleware_CounterResetsAfterTTL(t *testing.T) {
	rdb, mr := newTestRedis(t)
	h := router.RateLimitMiddleware(rdb)(okHandler())

	for i := 0; i < 60; i++ {
		doRequest(h, "1.2.3.4")
	}
	assert.Equal(t, http.StatusTooManyRequests, doRequest(h, "1.2.3.4"))

	// TTL 만료 시뮬레이션
	mr.Del("rate:limit:1.2.3.4")

	assert.Equal(t, http.StatusOK, doRequest(h, "1.2.3.4"),
		"TTL 만료 후 카운터가 초기화되어야 함")
}

// ── 엣지 케이스 ───────────────────────────────────────────────────────────────

func TestRateLimitMiddleware_RedisError_FailOpen(t *testing.T) {
	rdb, mr := newTestRedis(t)
	h := router.RateLimitMiddleware(rdb)(okHandler())

	// Redis 종료 → fail-open (차단하지 않음)
	mr.Close()

	assert.Equal(t, http.StatusOK, doRequest(h, "1.2.3.4"),
		"Redis 장애 시 요청을 차단하면 안 됨")
}

func TestRateLimitMiddleware_IPv6_Handled(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := router.RateLimitMiddleware(rdb)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[::1]:12345"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "IPv6 주소도 정상 처리되어야 함")
}

func TestRateLimitMiddleware_ExactLimit_LastRequestPasses(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := router.RateLimitMiddleware(rdb)(okHandler())

	for i := 0; i < 59; i++ {
		doRequest(h, "5.5.5.5")
	}

	assert.Equal(t, http.StatusOK, doRequest(h, "5.5.5.5"),
		"정확히 60번째 요청은 통과해야 함")
	assert.Equal(t, http.StatusTooManyRequests, doRequest(h, "5.5.5.5"),
		"61번째 요청은 차단되어야 함")
}
