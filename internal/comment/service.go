package comment

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"

	"comment-api/internal/model"
)

const (
	countCachePrefix = "comment:count:"
	countCacheTTL    = 10 * time.Minute
	maxContentLen    = 1000
)

var (
	ErrForbidden    = errors.New("permission denied")
	ErrInvalidInput = errors.New("invalid input")
)

type Service interface {
	ListComments(ctx context.Context, postSeq int64) ([]CommentResponse, error)
	CreateComment(ctx context.Context, req CreateCommentRequest, authorID, authorEmail, authorName, authorAvatarURL string) error
	CreateReply(ctx context.Context, parentID bson.ObjectID, req CreateReplyRequest, authorID, authorEmail, authorName, authorAvatarURL string) error
	UpdateComment(ctx context.Context, id bson.ObjectID, req UpdateCommentRequest, requestorID string, isAdmin bool) error
	DeleteComment(ctx context.Context, id bson.ObjectID, requestorID string, isAdmin bool) error
	GetCount(ctx context.Context, postSeq int64) (int64, error)
}

type service struct {
	repo Repository
	rdb  *redis.Client
}

func NewService(repo Repository, rdb *redis.Client) Service {
	return &service{repo: repo, rdb: rdb}
}

func (s *service) ListComments(ctx context.Context, postSeq int64) ([]CommentResponse, error) {
	comments, err := s.repo.FindByPostSeq(ctx, postSeq)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return buildCommentTree(comments), nil
}

func (s *service) CreateComment(ctx context.Context, req CreateCommentRequest, authorID, authorEmail, authorName, authorAvatarURL string) error {
	if err := validateContent(req.Content); err != nil {
		return err
	}

	now := time.Now().UTC()
	comment := &model.Comment{
		PostSeq:         req.PostSeq,
		Depth:           0,
		Content:         req.Content,
		AuthorID:        authorID,
		AuthorEmail:     authorEmail,
		AuthorName:      authorName,
		AuthorAvatarURL: authorAvatarURL,
		IsDeleted:       false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.Create(ctx, comment); err != nil {
		return fmt.Errorf("create comment: %w", err)
	}

	s.invalidateCountCache(ctx, req.PostSeq)
	return nil
}

func (s *service) CreateReply(ctx context.Context, parentID bson.ObjectID, req CreateReplyRequest, authorID, authorEmail, authorName, authorAvatarURL string) error {
	if err := validateContent(req.Content); err != nil {
		return err
	}

	parent, err := s.repo.FindByID(ctx, parentID)
	if err != nil {
		return fmt.Errorf("find parent comment: %w", err)
	}

	depth, replyParentID, rootID := computeReplyFields(parent)

	now := time.Now().UTC()
	reply := &model.Comment{
		PostSeq:         parent.PostSeq,
		ParentID:        replyParentID,
		RootID:          rootID,
		Depth:           depth,
		Content:         req.Content,
		AuthorID:        authorID,
		AuthorEmail:     authorEmail,
		AuthorName:      authorName,
		AuthorAvatarURL: authorAvatarURL,
		IsDeleted:       false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.Create(ctx, reply); err != nil {
		return fmt.Errorf("create reply: %w", err)
	}

	s.invalidateCountCache(ctx, parent.PostSeq)
	return nil
}

func (s *service) UpdateComment(ctx context.Context, id bson.ObjectID, req UpdateCommentRequest, requestorID string, isAdmin bool) error {
	if err := validateContent(req.Content); err != nil {
		return err
	}

	if !isAdmin {
		comment, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return fmt.Errorf("find comment: %w", err)
		}
		if comment.AuthorID != requestorID {
			return ErrForbidden
		}
	}

	if err := s.repo.UpdateContent(ctx, id, req.Content, time.Now().UTC()); err != nil {
		return fmt.Errorf("update comment: %w", err)
	}
	return nil
}

func (s *service) DeleteComment(ctx context.Context, id bson.ObjectID, requestorID string, isAdmin bool) error {
	comment, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find comment: %w", err)
	}

	if !isAdmin && comment.AuthorID != requestorID {
		return ErrForbidden
	}

	if err := s.repo.SoftDelete(ctx, id, time.Now().UTC()); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	s.invalidateCountCache(ctx, comment.PostSeq)
	return nil
}

func (s *service) GetCount(ctx context.Context, postSeq int64) (int64, error) {
	key := countCachePrefix + strconv.FormatInt(postSeq, 10)

	if val, err := s.rdb.Get(ctx, key).Result(); err == nil {
		if count, err := strconv.ParseInt(val, 10, 64); err == nil {
			return count, nil
		}
	}

	count, err := s.repo.CountByPostSeq(ctx, postSeq)
	if err != nil {
		return 0, fmt.Errorf("get count: %w", err)
	}

	s.rdb.Set(ctx, key, count, countCacheTTL)
	return count, nil
}

func (s *service) invalidateCountCache(ctx context.Context, postSeq int64) {
	s.rdb.Del(ctx, countCachePrefix+strconv.FormatInt(postSeq, 10))
}

// buildCommentTree는 평면 댓글 목록을 parentId 기반 트리 구조로 변환합니다.
// FindByPostSeq는 createdAt 오름차순으로 반환하므로 부모가 항상 자식보다 먼저 처리됩니다.
func buildCommentTree(comments []*model.Comment) []CommentResponse {
	type node struct {
		resp     CommentResponse
		children []*node
	}

	byID := make(map[bson.ObjectID]*node, len(comments))
	var roots []*node

	for _, c := range comments {
		n := &node{resp: NewCommentResponse(c)}
		byID[c.ID] = n

		if c.Depth == 0 || c.ParentID == nil {
			roots = append(roots, n)
		} else if parent, ok := byID[*c.ParentID]; ok {
			parent.children = append(parent.children, n)
		}
	}

	var convert func(n *node) CommentResponse
	convert = func(n *node) CommentResponse {
		resp := n.resp
		resp.Replies = make([]CommentResponse, 0, len(n.children))
		for _, child := range n.children {
			resp.Replies = append(resp.Replies, convert(child))
		}
		return resp
	}

	result := make([]CommentResponse, 0, len(roots))
	for _, r := range roots {
		result = append(result, convert(r))
	}
	return result
}

// computeReplyFields는 부모 댓글의 depth를 기반으로 답글의 depth, parentId, rootId를 결정합니다.
//
// depth 제한 규칙 (AGENTS.md §4-2):
//   - depth=0 부모 → 답글 depth=1,  rootId=부모ID
//   - depth=1 부모 → 답글 depth=2,  rootId 상속
//   - depth>=2 부모 → 답글 depth=2, parentId=부모의 parentId (형제로 삽입)
func computeReplyFields(parent *model.Comment) (depth int, parentID *bson.ObjectID, rootID *bson.ObjectID) {
	id := parent.ID
	switch {
	case parent.Depth == 0:
		return 1, &id, &id
	case parent.Depth == 1:
		return 2, &id, parent.RootID
	default:
		return 2, parent.ParentID, parent.RootID
	}
}

// validateContent는 content 길이를 Unicode 문자 단위로 검증합니다.
func validateContent(content string) error {
	length := len([]rune(content))
	if length < 1 || length > maxContentLen {
		return fmt.Errorf("%w: 내용은 1자 이상 %d자 이하여야 합니다", ErrInvalidInput, maxContentLen)
	}
	return nil
}
