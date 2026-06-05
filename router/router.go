package router

import (
	"net/http"

	"comment-api/internal/auth"
)

func New(githubHandler *auth.GitHubHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /auth/github", githubHandler.Login)
	mux.HandleFunc("GET /auth/github/callback", githubHandler.Callback)

	return mux
}
