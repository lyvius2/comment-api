package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort string
	AppEnv  string

	GitHubClientID     string
	GitHubClientSecret string
	GitHubCallbackURL  string
	AuthSuccessURL     string

	CommentSessionCookie string
	LifelogSessionCookie string
	LifelogSessionAttr   string
	SessionTTLSeconds    int
	SessionCookieDomain  string

	MongoURI    string
	MongoDBName string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	CORSAllowedOrigins string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	sessionTTL, err := strconv.Atoi(getEnv("SESSION_TTL_SECONDS", "600"))
	if err != nil {
		return nil, fmt.Errorf("SESSION_TTL_SECONDS must be an integer: %w", err)
	}

	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		return nil, fmt.Errorf("REDIS_DB must be an integer: %w", err)
	}

	return &Config{
		AppPort: getEnv("APP_PORT", "8081"),
		AppEnv:  getEnv("APP_ENV", "development"),

		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GitHubCallbackURL:  os.Getenv("GITHUB_CALLBACK_URL"),
		AuthSuccessURL:     os.Getenv("AUTH_SUCCESS_URL"),

		CommentSessionCookie: getEnv("COMMENT_SESSION_COOKIE", "COMMENT_SESSION"),
		LifelogSessionCookie: getEnv("LIFELOG_SESSION_COOKIE", "LIFELOG_SESSION"),
		LifelogSessionAttr:   getEnv("LIFELOG_SESSION_ATTR", "loginMember"),
		SessionTTLSeconds:    sessionTTL,
		SessionCookieDomain:  os.Getenv("SESSION_COOKIE_DOMAIN"),

		MongoURI:    os.Getenv("MONGO_URI"),
		MongoDBName: getEnv("MONGO_DB_NAME", "comment_db"),

		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       redisDB,

		CORSAllowedOrigins: os.Getenv("CORS_ALLOWED_ORIGINS"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
