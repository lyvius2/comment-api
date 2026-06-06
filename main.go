// Package main은 comment-api 서버의 진입점입니다.
//
// @title           Comment API
// @version         1.0
// @description     댓글 및 사진 댓글 CRUD REST API. 인증은 쿠키 기반 세션으로 처리됩니다.
// @host            localhost:8081
// @BasePath        /
//
// @securityDefinitions.apikey CommentSession
// @in              cookie
// @name            COMMENT_SESSION
// @description     GitHub SSO로 발급된 일반 사용자 세션 쿠키
//
// @securityDefinitions.apikey LifelogSession
// @in              cookie
// @name            LIFELOG_SESSION
// @description     lifelog(Java)에서 발급된 관리자 세션 쿠키 (읽기 전용)
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
