// Package main is the entry point for the comment-api server.
//
// @title           Comment API
// @version         1.0
// @description     Comment and photo comment CRUD REST API. Authentication is handled via cookie-based sessions.
// @host            localhost:8081
// @BasePath        /
//
// @securityDefinitions.apikey CommentSession
// @in              cookie
// @name            COMMENT_SESSION
// @description     General user session cookie issued via GitHub SSO
//
// @securityDefinitions.apikey LifelogSession
// @in              cookie
// @name            LIFELOG_SESSION
// @description     Admin session cookie issued by lifelog (Java), read-only
package main

import (
	"context"
	"log/slog"
	"net/http"

	"comment-api/config"
	_ "comment-api/docs"
	"comment-api/internal/auth"
	"comment-api/internal/comment"
	"comment-api/internal/photocomment"
	"comment-api/pkg/database"
	"comment-api/pkg/metrics"
	"comment-api/router"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return
	}

	mongoClient, err := database.NewMongoClient(context.Background(), cfg.MongoURI)
	if err != nil {
		slog.Error("failed to connect to MongoDB", "error", err)
		return
	}
	db := mongoClient.Database(cfg.MongoDBName)

	rdb := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)

	commentRepo := comment.NewRepository(db)
	commentSvc := comment.NewService(commentRepo, rdb)
	commentHandler := comment.NewHandler(commentSvc, rdb, cfg)

	photoRepo := photocomment.NewRepository(db)
	photoSvc := photocomment.NewService(photoRepo, rdb)
	photoHandler := photocomment.NewHandler(photoSvc)

	githubHandler := auth.NewGitHubHandler(cfg, rdb)

	metrics.StartCPUCollector()

	mux := router.New(cfg, rdb, githubHandler, commentHandler, photoHandler)

	slog.Info("starting server", "port", cfg.AppPort, "env", cfg.AppEnv)
	if err := http.ListenAndServe(":"+cfg.AppPort, mux); err != nil {
		slog.Error("server error", "error", err)
	}
}
