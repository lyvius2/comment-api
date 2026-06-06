package comment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"comment-api/internal/comment"
	"comment-api/internal/model"
)

// ── ListComments ──────────────────────────────────────────────────────────────

func TestListComments_ReturnsBuiltTree(t *testing.T) {
	parentID := bson.NewObjectID()
	childID := bson.NewObjectID()

	rootIDPtr := &parentID
	parentIDPtr := &parentID

	repo := &mockCommentRepository{}
	repo.On("FindByPostSeq", mock.Anything, int64(1)).Return([]*model.Comment{
		{ID: parentID, PostSeq: 1, Depth: 0, Content: "부모 댓글"},
		{ID: childID, PostSeq: 1, Depth: 1, ParentID: parentIDPtr, RootID: rootIDPtr, Content: "답글"},
	}, nil)

	svc := comment.NewService(repo, nil)
	result, err := svc.ListComments(context.Background(), 1)

	require.NoError(t, err)
	require.Len(t, result, 1, "루트 댓글은 1개여야 함")
	assert.Equal(t, "부모 댓글", result[0].Content)
	require.Len(t, result[0].Replies, 1, "답글이 트리에 포함되어야 함")
	assert.Equal(t, "답글", result[0].Replies[0].Content)
	repo.AssertExpectations(t)
}

func TestListComments_DeletedComment_ContentReplaced(t *testing.T) {
	id := bson.NewObjectID()
	repo := &mockCommentRepository{}
	repo.On("FindByPostSeq", mock.Anything, int64(1)).Return([]*model.Comment{
		{ID: id, PostSeq: 1, Depth: 0, Content: "원본 내용", IsDeleted: true},
	}, nil)

	svc := comment.NewService(repo, nil)
	result, err := svc.ListComments(context.Background(), 1)

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "삭제된 댓글입니다.", result[0].Content)
}

func TestListComments_RepoError_Propagates(t *testing.T) {
	repo := &mockCommentRepository{}
	repo.On("FindByPostSeq", mock.Anything, int64(1)).Return(nil, errors.New("db error"))

	svc := comment.NewService(repo, nil)
	_, err := svc.ListComments(context.Background(), 1)

	assert.Error(t, err)
}

// ── CreateComment ─────────────────────────────────────────────────────────────

func TestCreateComment_ValidContent_CreatesComment(t *testing.T) {
	rdb, _ := newTestRedis(t)

	repo := &mockCommentRepository{}
	repo.On("Create", mock.Anything, mock.MatchedBy(func(c *model.Comment) bool {
		return c.PostSeq == 1 &&
			c.Content == "안녕하세요" &&
			c.Depth == 0 &&
			c.AuthorID == "user-1" &&
			!c.IsDeleted
	})).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.CreateComment(context.Background(),
		comment.CreateCommentRequest{PostSeq: 1, Content: "안녕하세요"},
		"user-1", "user@example.com", "testuser", "https://avatar.url",
	)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestCreateComment_EmptyContent_ReturnsErrInvalidInput(t *testing.T) {
	svc := comment.NewService(&mockCommentRepository{}, nil)
	err := svc.CreateComment(context.Background(),
		comment.CreateCommentRequest{PostSeq: 1, Content: ""},
		"user-1", "", "", "",
	)

	assert.ErrorIs(t, err, comment.ErrInvalidInput)
}

func TestCreateComment_TooLongContent_ReturnsErrInvalidInput(t *testing.T) {
	svc := comment.NewService(&mockCommentRepository{}, nil)
	err := svc.CreateComment(context.Background(),
		comment.CreateCommentRequest{PostSeq: 1, Content: strings.Repeat("가", 1001)},
		"user-1", "", "", "",
	)

	assert.ErrorIs(t, err, comment.ErrInvalidInput)
}

func TestCreateComment_BoundaryContent_1000Chars_Success(t *testing.T) {
	rdb, _ := newTestRedis(t)

	repo := &mockCommentRepository{}
	repo.On("Create", mock.Anything, mock.Anything).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.CreateComment(context.Background(),
		comment.CreateCommentRequest{PostSeq: 1, Content: strings.Repeat("가", 1000)},
		"user-1", "", "", "",
	)

	require.NoError(t, err)
}

func TestCreateComment_InvalidatesCountCache(t *testing.T) {
	rdb, mr := newTestRedis(t)
	mr.Set("comment:count:1", "42")

	repo := &mockCommentRepository{}
	repo.On("Create", mock.Anything, mock.Anything).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.CreateComment(context.Background(),
		comment.CreateCommentRequest{PostSeq: 1, Content: "새 댓글"},
		"user-1", "", "", "",
	)

	require.NoError(t, err)
	n, _ := rdb.Exists(context.Background(), "comment:count:1").Result()
	assert.Equal(t, int64(0), n, "캐시가 삭제되어야 함")
}

// ── CreateReply ───────────────────────────────────────────────────────────────

func TestCreateReply_DepthZeroParent_CreatesDepth1(t *testing.T) {
	rdb, _ := newTestRedis(t)

	parentID := bson.NewObjectID()
	parent := &model.Comment{ID: parentID, PostSeq: 1, Depth: 0}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, parentID).Return(parent, nil)
	repo.On("Create", mock.Anything, mock.MatchedBy(func(c *model.Comment) bool {
		return c.Depth == 1 &&
			c.ParentID != nil && *c.ParentID == parentID &&
			c.RootID != nil && *c.RootID == parentID
	})).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.CreateReply(context.Background(),
		parentID,
		comment.CreateReplyRequest{Content: "depth1 답글"},
		"user-1", "", "", "",
	)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestCreateReply_DepthOneParent_CreatesDepth2(t *testing.T) {
	rdb, _ := newTestRedis(t)

	rootID := bson.NewObjectID()
	parentID := bson.NewObjectID()
	parent := &model.Comment{
		ID: parentID, PostSeq: 1, Depth: 1,
		ParentID: &rootID, RootID: &rootID,
	}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, parentID).Return(parent, nil)
	repo.On("Create", mock.Anything, mock.MatchedBy(func(c *model.Comment) bool {
		return c.Depth == 2 &&
			c.ParentID != nil && *c.ParentID == parentID &&
			c.RootID != nil && *c.RootID == rootID
	})).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.CreateReply(context.Background(),
		parentID,
		comment.CreateReplyRequest{Content: "depth2 답글"},
		"user-1", "", "", "",
	)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestCreateReply_DepthTwoParent_CreatesSibling(t *testing.T) {
	rdb, _ := newTestRedis(t)

	rootID := bson.NewObjectID()
	grandParentID := bson.NewObjectID()
	parentID := bson.NewObjectID()
	parent := &model.Comment{
		ID: parentID, PostSeq: 1, Depth: 2,
		ParentID: &grandParentID, RootID: &rootID,
	}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, parentID).Return(parent, nil)
	repo.On("Create", mock.Anything, mock.MatchedBy(func(c *model.Comment) bool {
		// depth=2 형제: parentId = 부모의 parentId(grandParentID), rootId 유지
		return c.Depth == 2 &&
			c.ParentID != nil && *c.ParentID == grandParentID &&
			c.RootID != nil && *c.RootID == rootID
	})).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.CreateReply(context.Background(),
		parentID,
		comment.CreateReplyRequest{Content: "형제 답글"},
		"user-1", "", "", "",
	)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestCreateReply_ParentNotFound_ReturnsErrNotFound(t *testing.T) {
	parentID := bson.NewObjectID()

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, parentID).Return(nil, comment.ErrNotFound)

	svc := comment.NewService(repo, nil)
	err := svc.CreateReply(context.Background(),
		parentID,
		comment.CreateReplyRequest{Content: "답글"},
		"user-1", "", "", "",
	)

	assert.ErrorIs(t, err, comment.ErrNotFound)
}

func TestCreateReply_InvalidContent_ReturnsErrInvalidInput(t *testing.T) {
	svc := comment.NewService(&mockCommentRepository{}, nil)
	err := svc.CreateReply(context.Background(),
		bson.NewObjectID(),
		comment.CreateReplyRequest{Content: ""},
		"user-1", "", "", "",
	)

	assert.ErrorIs(t, err, comment.ErrInvalidInput)
}

// ── UpdateComment ─────────────────────────────────────────────────────────────

func TestUpdateComment_AsAuthor_Success(t *testing.T) {
	id := bson.NewObjectID()
	existing := &model.Comment{ID: id, AuthorID: "user-1"}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("UpdateContent", mock.Anything, id, "수정 내용", mock.Anything).Return(nil)

	svc := comment.NewService(repo, nil)
	err := svc.UpdateComment(context.Background(), id,
		comment.UpdateCommentRequest{Content: "수정 내용"},
		"user-1", false,
	)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestUpdateComment_AsAdmin_SkipsAuthorCheck(t *testing.T) {
	id := bson.NewObjectID()

	repo := &mockCommentRepository{}
	// 관리자는 FindByID 호출 없이 바로 UpdateContent
	repo.On("UpdateContent", mock.Anything, id, "관리자 수정", mock.Anything).Return(nil)

	svc := comment.NewService(repo, nil)
	err := svc.UpdateComment(context.Background(), id,
		comment.UpdateCommentRequest{Content: "관리자 수정"},
		"admin-1", true,
	)

	require.NoError(t, err)
	repo.AssertNotCalled(t, "FindByID")
	repo.AssertExpectations(t)
}

func TestUpdateComment_WrongAuthor_ReturnsErrForbidden(t *testing.T) {
	id := bson.NewObjectID()
	existing := &model.Comment{ID: id, AuthorID: "owner"}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)

	svc := comment.NewService(repo, nil)
	err := svc.UpdateComment(context.Background(), id,
		comment.UpdateCommentRequest{Content: "수정 시도"},
		"other-user", false,
	)

	assert.ErrorIs(t, err, comment.ErrForbidden)
}

func TestUpdateComment_NotFound_ReturnsErrNotFound(t *testing.T) {
	id := bson.NewObjectID()

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(nil, comment.ErrNotFound)

	svc := comment.NewService(repo, nil)
	err := svc.UpdateComment(context.Background(), id,
		comment.UpdateCommentRequest{Content: "내용"},
		"user-1", false,
	)

	assert.ErrorIs(t, err, comment.ErrNotFound)
}

func TestUpdateComment_InvalidContent_ReturnsErrInvalidInput(t *testing.T) {
	svc := comment.NewService(&mockCommentRepository{}, nil)
	err := svc.UpdateComment(context.Background(), bson.NewObjectID(),
		comment.UpdateCommentRequest{Content: ""},
		"user-1", false,
	)

	assert.ErrorIs(t, err, comment.ErrInvalidInput)
}

// ── DeleteComment ─────────────────────────────────────────────────────────────

func TestDeleteComment_AsAuthor_Success(t *testing.T) {
	rdb, _ := newTestRedis(t)

	id := bson.NewObjectID()
	existing := &model.Comment{ID: id, PostSeq: 1, AuthorID: "user-1"}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("SoftDelete", mock.Anything, id, mock.Anything).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.DeleteComment(context.Background(), id, "user-1", false)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDeleteComment_AsAdmin_SkipsOwnerCheck(t *testing.T) {
	rdb, _ := newTestRedis(t)

	id := bson.NewObjectID()
	existing := &model.Comment{ID: id, PostSeq: 1, AuthorID: "owner"}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("SoftDelete", mock.Anything, id, mock.Anything).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.DeleteComment(context.Background(), id, "admin-1", true)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDeleteComment_WrongAuthor_ReturnsErrForbidden(t *testing.T) {
	id := bson.NewObjectID()
	existing := &model.Comment{ID: id, PostSeq: 1, AuthorID: "owner"}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)

	svc := comment.NewService(repo, nil)
	err := svc.DeleteComment(context.Background(), id, "other", false)

	assert.ErrorIs(t, err, comment.ErrForbidden)
}

func TestDeleteComment_NotFound_ReturnsErrNotFound(t *testing.T) {
	id := bson.NewObjectID()

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(nil, comment.ErrNotFound)

	svc := comment.NewService(repo, nil)
	err := svc.DeleteComment(context.Background(), id, "user-1", false)

	assert.ErrorIs(t, err, comment.ErrNotFound)
}

func TestDeleteComment_InvalidatesCountCache(t *testing.T) {
	rdb, mr := newTestRedis(t)
	mr.Set("comment:count:5", "10")

	id := bson.NewObjectID()
	existing := &model.Comment{ID: id, PostSeq: 5, AuthorID: "user-1"}

	repo := &mockCommentRepository{}
	repo.On("FindByID", mock.Anything, id).Return(existing, nil)
	repo.On("SoftDelete", mock.Anything, id, mock.Anything).Return(nil)

	svc := comment.NewService(repo, rdb)
	err := svc.DeleteComment(context.Background(), id, "user-1", false)

	require.NoError(t, err)
	n, _ := rdb.Exists(context.Background(), "comment:count:5").Result()
	assert.Equal(t, int64(0), n, "삭제 후 캐시가 무효화되어야 함")
}

// ── GetCount ──────────────────────────────────────────────────────────────────

func TestGetCount_CacheHit_ReturnsFromCache(t *testing.T) {
	rdb, mr := newTestRedis(t)
	mr.Set("comment:count:1", "42")

	repo := &mockCommentRepository{}
	svc := comment.NewService(repo, rdb)

	count, err := svc.GetCount(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
	repo.AssertNotCalled(t, "CountByPostSeq")
}

func TestGetCount_CacheMiss_QueriesRepoAndCaches(t *testing.T) {
	rdb, _ := newTestRedis(t)

	repo := &mockCommentRepository{}
	repo.On("CountByPostSeq", mock.Anything, int64(1)).Return(int64(7), nil)

	svc := comment.NewService(repo, rdb)
	count, err := svc.GetCount(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, int64(7), count)

	cached, _ := rdb.Get(context.Background(), "comment:count:1").Result()
	assert.Equal(t, "7", cached, "결과가 Redis에 캐시되어야 함")
	repo.AssertExpectations(t)
}

func TestGetCount_RepoError_Propagates(t *testing.T) {
	rdb, _ := newTestRedis(t)

	repo := &mockCommentRepository{}
	repo.On("CountByPostSeq", mock.Anything, int64(1)).Return(int64(0), errors.New("db error"))

	svc := comment.NewService(repo, rdb)
	_, err := svc.GetCount(context.Background(), 1)

	assert.Error(t, err)
}
