package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type JavaSessionMember struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
}

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

func getSession(ctx context.Context, rdb *redis.Client, sessionID string) (*CommentSession, error) {
	val, err := rdb.Get(ctx, sessionKeyPrefix+sessionID).Result()
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	var session CommentSession
	if err := json.Unmarshal([]byte(val), &session); err != nil {
		return nil, fmt.Errorf("session deserialize: %w", err)
	}
	return &session, nil
}

func deleteSession(ctx context.Context, rdb *redis.Client, sessionID string) error {
	if err := rdb.Del(ctx, sessionKeyPrefix+sessionID).Err(); err != nil {
		return fmt.Errorf("session redis del: %w", err)
	}
	return nil
}

func getJavaSession(ctx context.Context, rdb *redis.Client, sessionID, attrKey string) (*JavaSessionMember, error) {
	key := "spring:session:sessions:" + sessionID
	val, err := rdb.HGet(ctx, key, "sessionAttr:"+attrKey).Result()
	if err != nil {
		return nil, fmt.Errorf("java session not found: %w", err)
	}
	var member JavaSessionMember
	if err := json.Unmarshal([]byte(val), &member); err != nil {
		return nil, fmt.Errorf("java session deserialize: %w", err)
	}
	return &member, nil
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
