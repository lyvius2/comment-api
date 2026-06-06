package comment_test

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
	"comment-api/internal/comment"
	"comment-api/internal/model"
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

// mockCommentService는 comment.Service의 testify mock 구현체입니다.
type mockCommentService struct {
	mock.Mock
}

func (m *mockCommentService) ListComments(ctx context.Context, postSeq int64) ([]comment.CommentResponse, error) {
	args := m.Called(ctx, postSeq)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]comment.CommentResponse), args.Error(1)
}

func (m *mockCommentService) CreateComment(ctx context.Context, req comment.CreateCommentRequest, authorID, authorEmail, authorName, authorAvatarURL string) error {
	return m.Called(ctx, req, authorID, authorEmail, authorName, authorAvatarURL).Error(0)
}

func (m *mockCommentService) CreateReply(ctx context.Context, parentID bson.ObjectID, req comment.CreateReplyRequest, authorID, authorEmail, authorName, authorAvatarURL string) error {
	return m.Called(ctx, parentID, req, authorID, authorEmail, authorName, authorAvatarURL).Error(0)
}

func (m *mockCommentService) UpdateComment(ctx context.Context, id bson.ObjectID, req comment.UpdateCommentRequest, requestorID string, isAdmin bool) error {
	return m.Called(ctx, id, req, requestorID, isAdmin).Error(0)
}

func (m *mockCommentService) DeleteComment(ctx context.Context, id bson.ObjectID, requestorID string, isAdmin bool) error {
	return m.Called(ctx, id, requestorID, isAdmin).Error(0)
}

func (m *mockCommentService) GetCount(ctx context.Context, postSeq int64) (int64, error) {
	args := m.Called(ctx, postSeq)
	return args.Get(0).(int64), args.Error(1)
}

// commentSession은 테스트용 기본 CommentSession을 반환합니다.
func commentSession() *auth.CommentSession {
	return &auth.CommentSession{
		UserID:    "user-123",
		Email:     "user@example.com",
		Username:  "testuser",
		AvatarURL: "https://avatars.example.com/u/1",
	}
}

// adminMember는 테스트용 기본 JavaSessionMember(관리자)를 반환합니다.
func adminMember() *auth.JavaSessionMember {
	return &auth.JavaSessionMember{
		UserID:   "admin-456",
		Email:    "admin@example.com",
		Username: "adminuser",
		IsAdmin:  true,
	}
}

// validObjectID는 테스트용 유효한 ObjectID 문자열을 반환합니다.
func validObjectID() (bson.ObjectID, string) {
	id := bson.NewObjectID()
	return id, id.Hex()
}

// mockCommentRepository는 comment.Repository의 testify mock 구현체입니다.
type mockCommentRepository struct {
	mock.Mock
}

func (m *mockCommentRepository) FindByPostSeq(ctx context.Context, postSeq int64) ([]*model.Comment, error) {
	args := m.Called(ctx, postSeq)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*model.Comment), args.Error(1)
}

func (m *mockCommentRepository) FindByID(ctx context.Context, id bson.ObjectID) (*model.Comment, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Comment), args.Error(1)
}

func (m *mockCommentRepository) Create(ctx context.Context, c *model.Comment) error {
	return m.Called(ctx, c).Error(0)
}

func (m *mockCommentRepository) UpdateContent(ctx context.Context, id bson.ObjectID, content string, updatedAt time.Time) error {
	return m.Called(ctx, id, content, updatedAt).Error(0)
}

func (m *mockCommentRepository) SoftDelete(ctx context.Context, id bson.ObjectID, deletedAt time.Time) error {
	return m.Called(ctx, id, deletedAt).Error(0)
}

func (m *mockCommentRepository) CountByPostSeq(ctx context.Context, postSeq int64) (int64, error) {
	args := m.Called(ctx, postSeq)
	return args.Get(0).(int64), args.Error(1)
}
