package main

import (
	"context"
	"log/slog"
	"net/http"

	"comment-api/config"
	"comment-api/internal/auth"
	"comment-api/internal/comment"
	"comment-api/internal/photocomment"
	"comment-api/pkg/database"
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

	mux := router.New(cfg, rdb, githubHandler, commentHandler, photoHandler)

	slog.Info("starting server", "port", cfg.AppPort, "env", cfg.AppEnv)
	if err := http.ListenAndServe(":"+cfg.AppPort, mux); err != nil {
		slog.Error("server error", "error", err)
	}
}
