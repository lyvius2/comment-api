package router

import (
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"comment-api/config"
	"comment-api/internal/auth"
	"comment-api/internal/comment"
	"comment-api/internal/photocomment"
	"comment-api/pkg/metrics"
)

func New(
	cfg *config.Config,
	rdb *redis.Client,
	githubHandler *auth.GitHubHandler,
	commentHandler *comment.Handler,
	photoHandler *photocomment.Handler,
) http.Handler {
	mux := http.NewServeMux()
	sessionMW := auth.SessionMiddleware(cfg, rdb)

	// Prometheus metrics endpoint (no auth required)
	mux.Handle("GET /metrics", metrics.Handler())

	// Swagger UI
	mux.Handle("/swagger/", httpSwagger.WrapHandler)

	// Auth
	mux.HandleFunc("GET /auth/github", githubHandler.Login)
	mux.HandleFunc("GET /auth/github/callback", githubHandler.Callback)
	mux.Handle("POST /auth/logout", sessionMW(http.HandlerFunc(githubHandler.Logout)))

	// Session heartbeat
	mux.Handle("POST /api/comments/user/session/heartbeat", sessionMW(http.HandlerFunc(commentHandler.Heartbeat)))

	// Comments
	mux.HandleFunc("GET /api/comments", commentHandler.ListComments)
	mux.Handle("POST /api/comments", sessionMW(http.HandlerFunc(commentHandler.CreateComment)))
	mux.Handle("POST /api/comments/{commentId}/replies", sessionMW(http.HandlerFunc(commentHandler.CreateReply)))
	mux.Handle("PUT /api/comments/{commentId}", sessionMW(http.HandlerFunc(commentHandler.UpdateComment)))
	mux.Handle("DELETE /api/comments/{commentId}", sessionMW(http.HandlerFunc(commentHandler.DeleteComment)))

	// Photo comments
	mux.HandleFunc("GET /api/photo-comments", photoHandler.ListPhotoComments)
	mux.Handle("POST /api/photo-comments", sessionMW(http.HandlerFunc(photoHandler.CreatePhotoComment)))
	mux.Handle("PUT /api/photo-comments/{commentId}", sessionMW(http.HandlerFunc(photoHandler.UpdatePhotoComment)))
	mux.Handle("DELETE /api/photo-comments/{commentId}", sessionMW(http.HandlerFunc(photoHandler.DeletePhotoComment)))

	return metrics.InstrumentHandler(corsMiddleware(cfg)(RateLimitMiddleware(rdb)(mux)))
}

func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{})
	for _, origin := range strings.Split(cfg.CORSAllowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
