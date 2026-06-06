package photocomment_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"
	"go.mongodb.org/mongo-driver/v2/bson"

	"comment-api/config"
	"comment-api/internal/auth"
	"comment-api/internal/model"
	"comment-api/internal/photocomment"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

func baseConfig() *config.Config {
	return &config.Config{
		AppEnv:               "development",
		CommentSessionCookie: "COMMENT_SESSION",
		LifelogSessionCookie: "LIFELOG_SESSION",
		LifelogSessionAttr:   "loginMember",
		SessionTTLSeconds:    600,
	}
}

func setupCommentSession(t *testing.T, mr *miniredis.Miniredis, sessionID string, session *auth.CommentSession) {
	t.Helper()
	data, _ := json.Marshal(session)
	mr.Set(auth.SessionKeyPrefix+sessionID, string(data))
}

func setupAdminSession(t *testing.T, mr *miniredis.Miniredis, sessionID string, member *auth.JavaSessionMember) {
	t.Helper()
	data, _ := json.Marshal(member)
	mr.HSet("spring:session:sessions:"+sessionID, "sessionAttr:loginMember", string(data))
}

// mockPhotoService는 photocomment.Service의 testify mock 구현체입니다.
type mockPhotoService struct {
	mock.Mock
}

func (m *mockPhotoService) ListPhotoComments(ctx context.Context, photoSeq int64) ([]photocomment.PhotoCommentResponse, error) {
	args := m.Called(ctx, photoSeq)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]photocomment.PhotoCommentResponse), args.Error(1)
}

func (m *mockPhotoService) CreatePhotoComment(ctx context.Context, req photocomment.CreatePhotoCommentRequest, authorID, authorEmail, authorName, authorAvatarURL string) error {
	return m.Called(ctx, req, authorID, authorEmail, authorName, authorAvatarURL).Error(0)
}

func (m *mockPhotoService) UpdatePhotoComment(ctx context.Context, id bson.ObjectID, req photocomment.UpdatePhotoCommentRequest, requestorID string, isAdmin bool) error {
	return m.Called(ctx, id, req, requestorID, isAdmin).Error(0)
}

func (m *mockPhotoService) DeletePhotoComment(ctx context.Context, id bson.ObjectID, requestorID string, isAdmin bool) error {
	return m.Called(ctx, id, requestorID, isAdmin).Error(0)
}

func (m *mockPhotoService) GetCount(ctx context.Context, photoSeq int64) (int64, error) {
	args := m.Called(ctx, photoSeq)
	return args.Get(0).(int64), args.Error(1)
}

func commentSession() *auth.CommentSession {
	return &auth.CommentSession{
		UserID:    "user-123",
		Email:     "user@example.com",
		Username:  "testuser",
		AvatarURL: "https://avatars.example.com/u/1",
	}
}

func adminMember() *auth.JavaSessionMember {
	return &auth.JavaSessionMember{
		UserID:   "admin-456",
		Email:    "admin@example.com",
		Username: "adminuser",
		IsAdmin:  true,
	}
}

func validObjectID() (bson.ObjectID, string) {
	id := bson.NewObjectID()
	return id, id.Hex()
}

// mockPhotoRepository는 photocomment.Repository의 testify mock 구현체입니다.
type mockPhotoRepository struct {
	mock.Mock
}

func (m *mockPhotoRepository) FindByPhotoSeq(ctx context.Context, photoSeq int64) ([]*model.PhotoComment, error) {
	args := m.Called(ctx, photoSeq)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*model.PhotoComment), args.Error(1)
}

func (m *mockPhotoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*model.PhotoComment, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.PhotoComment), args.Error(1)
}

func (m *mockPhotoRepository) Create(ctx context.Context, c *model.PhotoComment) error {
	return m.Called(ctx, c).Error(0)
}

func (m *mockPhotoRepository) UpdateContent(ctx context.Context, id bson.ObjectID, content string, updatedAt time.Time) error {
	return m.Called(ctx, id, content, updatedAt).Error(0)
}

func (m *mockPhotoRepository) SoftDelete(ctx context.Context, id bson.ObjectID, deletedAt time.Time) error {
	return m.Called(ctx, id, deletedAt).Error(0)
}

func (m *mockPhotoRepository) CountByPhotoSeq(ctx context.Context, photoSeq int64) (int64, error) {
	args := m.Called(ctx, photoSeq)
	return args.Get(0).(int64), args.Error(1)
}
