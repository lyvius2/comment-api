package auth_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"comment-api/internal/auth"
)

func TestSaveSession_StoresJsonInRedis(t *testing.T) {
	rdb, mr := newTestRedis(t)
	ctx := context.Background()

	session := &auth.CommentSession{
		UserID:    "12345",
		Email:     "test@example.com",
		Username:  "testuser",
		AvatarURL: "https://avatars.github.com/u/12345",
		CreatedAt: "2026-06-05T00:00:00Z",
	}

	err := auth.SaveSession(ctx, rdb, "sid", session, time.Minute)
	require.NoError(t, err)

	raw, err := mr.Get(auth.SessionKeyPrefix + "sid")
	require.NoError(t, err)
	var got auth.CommentSession
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	assert.Equal(t, *session, got)
}

func TestGetSession_Found_ReturnsSession(t *testing.T) {
	rdb, mr := newTestRedis(t)
	ctx := context.Background()

	session := &auth.CommentSession{UserID: "12345", Username: "testuser"}
	data, _ := json.Marshal(session)
	mr.Set(auth.SessionKeyPrefix+"sid", string(data))

	got, err := auth.GetSession(ctx, rdb, "sid")
	require.NoError(t, err)
	assert.Equal(t, session.UserID, got.UserID)
	assert.Equal(t, session.Username, got.Username)
}

func TestGetSession_NotFound_ReturnsError(t *testing.T) {
	rdb, _ := newTestRedis(t)

	_, err := auth.GetSession(context.Background(), rdb, "nonexistent")
	assert.Error(t, err)
}

func TestDeleteSession_RemovesKeyFromRedis(t *testing.T) {
	rdb, mr := newTestRedis(t)
	ctx := context.Background()
	mr.Set(auth.SessionKeyPrefix+"sid", `{"userId":"1"}`)

	err := auth.DeleteSession(ctx, rdb, "sid")
	require.NoError(t, err)
	assert.False(t, mr.Exists(auth.SessionKeyPrefix+"sid"))
}

func TestGetJavaSession_Found_ReturnsDeserialized(t *testing.T) {
	rdb, mr := newTestRedis(t)
	ctx := context.Background()

	member := auth.JavaSessionMember{UserID: "admin1", Username: "admin", IsAdmin: true}
	data, _ := json.Marshal(member)
	mr.HSet("spring:session:sessions:java-sid", "sessionAttr:loginMember", string(data))

	got, err := auth.GetJavaSession(ctx, rdb, "java-sid", "loginMember")
	require.NoError(t, err)
	assert.Equal(t, member.UserID, got.UserID)
	assert.True(t, got.IsAdmin)
}

func TestGetJavaSession_NotFound_ReturnsError(t *testing.T) {
	rdb, _ := newTestRedis(t)

	_, err := auth.GetJavaSession(context.Background(), rdb, "nonexistent", "loginMember")
	assert.Error(t, err)
}
