package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const sessionKeyPrefix = "comment:session:"
const oauthStateKeyPrefix = "oauth:state:"

type CommentSession struct {
	UserID    string `json:"userId"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatarUrl"`
	CreatedAt string `json:"createdAt"`
}

func saveSession(ctx context.Context, rdb *redis.Client, sessionID string, session *CommentSession, ttl time.Duration) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("session marshal: %w", err)
	}
	if err := rdb.Set(ctx, sessionKeyPrefix+sessionID, data, ttl).Err(); err != nil {
		return fmt.Errorf("session redis set: %w", err)
	}
	return nil
}

func saveOAuthState(ctx context.Context, rdb *redis.Client, state string, ttl time.Duration) error {
	if err := rdb.Set(ctx, oauthStateKeyPrefix+state, "1", ttl).Err(); err != nil {
		return fmt.Errorf("oauth state redis set: %w", err)
	}
	return nil
}

func validateAndDeleteOAuthState(ctx context.Context, rdb *redis.Client, state string) error {
	key := oauthStateKeyPrefix + state
	val, err := rdb.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("oauth state not found in redis: %w", err)
	}
	if val != "1" {
		return fmt.Errorf("oauth state value invalid")
	}
	if err := rdb.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("oauth state redis del: %w", err)
	}
	return nil
}
