package auth

import (
	"context"
	"net/http"

	"github.com/redis/go-redis/v9"

	"comment-api/config"
	"comment-api/pkg/response"
)

type contextKey int

const (
	commentSessionCtxKey contextKey = iota
	javaSessionCtxKey
)

// SessionMiddleware COMMENT_SESSION → LIFELOG_SESSION 순서로 세션을 검증하고 Context에 저장합니다.
func SessionMiddleware(cfg *config.Config, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cookie, err := r.Cookie(cfg.CommentSessionCookie); err == nil {
				session, err := GetSession(r.Context(), rdb, cookie.Value)
				if err != nil {
					response.Error(w, http.StatusUnauthorized, "인증이 필요합니다.")
					return
				}
				ctx := context.WithValue(r.Context(), commentSessionCtxKey, session)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if cookie, err := r.Cookie(cfg.LifelogSessionCookie); err == nil {
				member, err := GetJavaSession(r.Context(), rdb, cookie.Value, cfg.LifelogSessionAttr)
				if err != nil {
					response.Error(w, http.StatusUnauthorized, "인증이 필요합니다.")
					return
				}
				ctx := context.WithValue(r.Context(), javaSessionCtxKey, member)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			response.Error(w, http.StatusUnauthorized, "인증이 필요합니다.")
		})
	}
}

// CommentSessionFromCtx Context에서 Go 세션을 꺼냅니다. 없으면 nil을 반환합니다.
func CommentSessionFromCtx(ctx context.Context) *CommentSession {
	v, _ := ctx.Value(commentSessionCtxKey).(*CommentSession)
	return v
}

// JavaSessionFromCtx Context에서 Java 세션을 꺼냅니다. 없으면 nil을 반환합니다.
func JavaSessionFromCtx(ctx context.Context) *JavaSessionMember {
	v, _ := ctx.Value(javaSessionCtxKey).(*JavaSessionMember)
	return v
}
