package photocomment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"comment-api/internal/model"
	"comment-api/internal/photocomment"
)

// ── ListPhotoComments ─────────────────────────────────────────────────────────

func TestListPhotoComments_ReturnsFlatList(t *testing.T) {
	id1, id2 := bson.NewObjectID(), bson.NewObjectID()
	repo := &mockPhotoRepository{}
	repo.On("FindByPhotoSeq", mock.Anything, int64(10)).Return([]*model.PhotoComment{
		{ID: id1, PhotoSeq: 10, Content: "첫 번째"},
		{ID: id2, PhotoSeq: 10, Content: "두 번째"},
	}, nil)

	svc := photocomment.NewService(repo, nil)
	result, err := svc.ListPhotoComments(context.Background(), 10)

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "첫 번째", result[0].Content)
	assert.Equal(t, "두 번째", result[1].Content)
	repo.AssertExpectations(t)
}

func TestListPhotoComments_DeletedComment_ContentReplaced(t *testing.T) {
	id := bson.NewObjectID()
	repo := &mockPhotoRepository{}
	repo.On("FindByPhotoSeq", mock.Anything, int64(10)).Return([]*model.PhotoComment{
		{ID: id, PhotoSeq: 10, Content: "원본 내용", IsDeleted: true},
	}, nil)

	svc := photocomment.NewService(repo, nil)
	result, err := svc.ListPhotoComments(context.Background(), 10)

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "삭제된 댓글입니다.", result[0].Content)
}

func TestListPhotoComments_RepoError_Propagates(t *testing.T) {
	repo := &mockPhotoRepository{}
	repo.On("FindByPhotoSeq", mock.Anything, int64(10)).Return(nil, errors.New("db error"))

	svc := photocomment.NewService(repo, nil)
	_, err := svc.ListPhotoComments(context.Background(), 10)

	assert.Error(t, err)
}

// ── CreatePhotoComment ────────────────────────────────────────────────────────

func TestCreatePhotoComment_ValidContent_CreatesComment(t *testing.T) {
	rdb, _ := newTestRedis(t)

	repo := &mockPhotoRepository{}
	repo.On("Create", mock.Anything, mock.MatchedBy(func(c *model.PhotoComment) bool {
		return c.PhotoSeq == 10 &&
			c.Content == "멋진 사진" &&
			c.AuthorID == "user-1" &&
			!c.IsDeleted
	})).Return(nil)

	svc := photocomment.NewService(repo, rdb)
	err := svc.CreatePhotoComment(context.Background(),
		photocomment.CreatePhotoCommentRequest{PhotoSeq: 10, Content: "멋진 사진"},
		"user-1", "user@example.com", "testuser", "https://avatar.url",
	)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestCreatePhotoComment_EmptyContent_ReturnsErrInvalidInput(t *testing.T) {
	svc := photocomment.NewService(&mockPhotoRepository{}, nil)
	err := svc.CreatePhotoComment(context.Background(),
		photocomment.CreatePhotoCommentRequest{PhotoSeq: 10, Content: ""},
		"user-1", "", "", "",
	)

	assert.ErrorIs(t, err, photocomment.ErrInvalidInput)
}

func TestCreatePhotoComment_TooLongContent_ReturnsErrInvalidInput(t *testing.T) {
	svc := photocomment.NewService(&mockPhotoRepository{}, nil)
	err := svc.CreatePhotoComment(context.Background(),
		photocomment.CreatePhotoCommentRequest{PhotoSeq: 10, Content: strings.Repeat("가", 1001)},
		"user-1", "", "", "",
	)

	assert.ErrorIs(t, err, photocomment.ErrInvalidInput)
}

func TestCreatePhotoComment_InvalidatesCountCache(t *testing.T) {
	rdb, mr := newTestRedis(t)
	mr.Set("comment:count:photo:10", "5")

	repo := &mockPhotoRepository{}
	repo.On("Create", mock.Anything, mock.Anything).Return(nil)

	svc := photocomment.NewService(repo, rdb)
	err := svc.CreatePhotoComment(context.Background(),
		photocomment.CreatePhotoCommentRequest{PhotoSeq: 10, Content: "새 댓글"},
		"user-1", "", "", "",
	)

	require.NoError(t, err)
	n, _ := rdb.Exists(context.Background(), "comment:count:photo:10").Result()
	assert.Equal(t, int64(0), n, "캐시가 삭제되어야 함")
}

// ── UpdatePhotoComment ────────────────────────────────────────────────────────

func TestUpdatePhotoComment_AsAuthor_Success(t *testing.T) {
	id := bson.NewObjectID()
	existing := &model.PhotoComment{ID: id, AuthorID: "user-1"}

	repo := &mockPhotoRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("UpdateContent", mock.Anything, id, "수정 내용", mock.Anything).Return(nil)

	svc := photocomment.NewService(repo, nil)
	err := svc.UpdatePhotoComment(context.Background(), id,
		photocomment.UpdatePhotoCommentRequest{Content: "수정 내용"},
		"user-1", false,
	)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestUpdatePhotoComment_AsAdmin_SkipsAuthorCheck(t *testing.T) {
	id := bson.NewObjectID()

	repo := &mockPhotoRepository{}
	repo.On("UpdateContent", mock.Anything, id, "관리자 수정", mock.Anything).Return(nil)

	svc := photocomment.NewService(repo, nil)
	err := svc.UpdatePhotoComment(context.Background(), id,
		photocomment.UpdatePhotoCommentRequest{Content: "관리자 수정"},
		"admin-1", true,
	)

	require.NoError(t, err)
	repo.AssertNotCalled(t, "FindByID")
	repo.AssertExpectations(t)
}

func TestUpdatePhotoComment_WrongAuthor_ReturnsErrForbidden(t *testing.T) {
	id := bson.NewObjectID()
	existing := &model.PhotoComment{ID: id, AuthorID: "owner"}

	repo := &mockPhotoRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)

	svc := photocomment.NewService(repo, nil)
	err := svc.UpdatePhotoComment(context.Background(), id,
		photocomment.UpdatePhotoCommentRequest{Content: "수정 시도"},
		"other", false,
	)

	assert.ErrorIs(t, err, photocomment.ErrForbidden)
}

func TestUpdatePhotoComment_InvalidContent_ReturnsErrInvalidInput(t *testing.T) {
	svc := photocomment.NewService(&mockPhotoRepository{}, nil)
	err := svc.UpdatePhotoComment(context.Background(), bson.NewObjectID(),
		photocomment.UpdatePhotoCommentRequest{Content: ""},
		"user-1", false,
	)

	assert.ErrorIs(t, err, photocomment.ErrInvalidInput)
}

// ── DeletePhotoComment ────────────────────────────────────────────────────────

func TestDeletePhotoComment_AsAuthor_Success(t *testing.T) {
	rdb, _ := newTestRedis(t)

	id := bson.NewObjectID()
	existing := &model.PhotoComment{ID: id, PhotoSeq: 10, AuthorID: "user-1"}

	repo := &mockPhotoRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("SoftDelete", mock.Anything, id, mock.Anything).Return(nil)

	svc := photocomment.NewService(repo, rdb)
	err := svc.DeletePhotoComment(context.Background(), id, "user-1", false)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDeletePhotoComment_AsAdmin_SkipsOwnerCheck(t *testing.T) {
	rdb, _ := newTestRedis(t)

	id := bson.NewObjectID()
	existing := &model.PhotoComment{ID: id, PhotoSeq: 10, AuthorID: "owner"}

	repo := &mockPhotoRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("SoftDelete", mock.Anything, id, mock.Anything).Return(nil)

	svc := photocomment.NewService(repo, rdb)
	err := svc.DeletePhotoComment(context.Background(), id, "admin-1", true)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDeletePhotoComment_WrongAuthor_ReturnsErrForbidden(t *testing.T) {
	id := bson.NewObjectID()
	existing := &model.PhotoComment{ID: id, PhotoSeq: 10, AuthorID: "owner"}

	repo := &mockPhotoRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)

	svc := photocomment.NewService(repo, nil)
	err := svc.DeletePhotoComment(context.Background(), id, "other", false)

	assert.ErrorIs(t, err, photocomment.ErrForbidden)
}

func TestDeletePhotoComment_NotFound_ReturnsErrNotFound(t *testing.T) {
	id := bson.NewObjectID()

	repo := &mockPhotoRepository{}
	repo.On("FindByID", mock.Anything, id).Return(nil, photocomment.ErrNotFound)

	svc := photocomment.NewService(repo, nil)
	err := svc.DeletePhotoComment(context.Background(), id, "user-1", false)

	assert.ErrorIs(t, err, photocomment.ErrNotFound)
}

func TestDeletePhotoComment_InvalidatesCountCache(t *testing.T) {
	rdb, mr := newTestRedis(t)
	mr.Set("comment:count:photo:10", "3")

	id := bson.NewObjectID()
	existing := &model.PhotoComment{ID: id, PhotoSeq: 10, AuthorID: "user-1"}

	repo := &mockPhotoRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("SoftDelete", mock.Anything, id, mock.Anything).Return(nil)

	svc := photocomment.NewService(repo, rdb)
	err := svc.DeletePhotoComment(context.Background(), id, "user-1", false)

	require.NoError(t, err)
	n, _ := rdb.Exists(context.Background(), "comment:count:photo:10").Result()
	assert.Equal(t, int64(0), n, "삭제 후 캐시가 무효화되어야 함")
}

// ── GetCount ──────────────────────────────────────────────────────────────────

func TestGetCount_CacheHit_ReturnsFromCache(t *testing.T) {
	rdb, mr := newTestRedis(t)
	mr.Set("comment:count:photo:10", "99")

	repo := &mockPhotoRepository{}
	svc := photocomment.NewService(repo, rdb)

	count, err := svc.GetCount(context.Background(), 10)

	require.NoError(t, err)
	assert.Equal(t, int64(99), count)
	repo.AssertNotCalled(t, "CountByPhotoSeq")
}

func TestGetCount_CacheMiss_QueriesRepoAndCaches(t *testing.T) {
	rdb, _ := newTestRedis(t)

	repo := &mockPhotoRepository{}
	repo.On("CountByPhotoSeq", mock.Anything, int64(10)).Return(int64(3), nil)

	svc := photocomment.NewService(repo, rdb)
	count, err := svc.GetCount(context.Background(), 10)

	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	cached, _ := rdb.Get(context.Background(), "comment:count:photo:10").Result()
	assert.Equal(t, "3", cached, "결과가 Redis에 캐시되어야 함")
	repo.AssertExpectations(t)
}

func TestGetCount_RepoError_Propagates(t *testing.T) {
	rdb, _ := newTestRedis(t)

	repo := &mockPhotoRepository{}
	repo.On("CountByPhotoSeq", mock.Anything, int64(10)).Return(int64(0), errors.New("db error"))

	svc := photocomment.NewService(repo, rdb)
	_, err := svc.GetCount(context.Background(), 10)

	assert.Error(t, err)
}
