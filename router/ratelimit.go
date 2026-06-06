package router

import (
	"net"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"comment-api/pkg/response"
)

const (
	rateLimitMax    = 60
	rateLimitWindow = time.Minute
	rateLimitPrefix = "rate:limit:"
)

// RateLimitMiddleware는 IP별로 분당 요청 수를 제한합니다.
// Redis가 응답하지 않으면 요청을 차단하지 않고 통과시킵니다 (fail-open).
func RateLimitMiddleware(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r.RemoteAddr)
			key := rateLimitPrefix + ip

			count, err := rdb.Incr(r.Context(), key).Result()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			if count == 1 {
				rdb.Expire(r.Context(), key, rateLimitWindow)
			}

			if count > rateLimitMax {
				response.Error(w, http.StatusTooManyRequests, "요청이 너무 많습니다. 잠시 후 다시 시도해주세요.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return ip
}
