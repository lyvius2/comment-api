package router

import (
	"net/http"

	"comment-api/config"
	"comment-api/internal/auth"
	"github.com/redis/go-redis/v9"
)

func New(cfg *config.Config, rdb *redis.Client, githubHandler *auth.GitHubHandler) http.Handler {
	mux := http.NewServeMux()
	sessionMW := auth.SessionMiddleware(cfg, rdb)

	// 인증 불필요
	mux.HandleFunc("GET /auth/github", githubHandler.Login)
	mux.HandleFunc("GET /auth/github/callback", githubHandler.Callback)

	// 인증 필요
	mux.Handle("POST /auth/logout", sessionMW(http.HandlerFunc(githubHandler.Logout)))

	return mux
}
