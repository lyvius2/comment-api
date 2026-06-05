package main

import (
	"log/slog"
	"net/http"

	"comment-api/config"
	"comment-api/internal/auth"
	"comment-api/pkg/database"
	"comment-api/router"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return
	}

	rdb := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)

	githubHandler := auth.NewGitHubHandler(cfg, rdb)

	mux := router.New(githubHandler)

	slog.Info("starting server", "port", cfg.AppPort, "env", cfg.AppEnv)
	if err := http.ListenAndServe(":"+cfg.AppPort, mux); err != nil {
		slog.Error("server error", "error", err)
	}
}
