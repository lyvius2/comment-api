package photocomment

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
	countCachePrefix = "comment:count:photo:"
	countCacheTTL    = 10 * time.Minute
	maxContentLen    = 1000
)

var (
	ErrForbidden    = errors.New("permission denied")
	ErrInvalidInput = errors.New("invalid input")
)

type Service interface {
	ListPhotoComments(ctx context.Context, photoSeq int64) ([]PhotoCommentResponse, error)
	CreatePhotoComment(ctx context.Context, req CreatePhotoCommentRequest, authorID, authorEmail, authorName, authorAvatarURL string) error
	UpdatePhotoComment(ctx context.Context, id bson.ObjectID, req UpdatePhotoCommentRequest, requestorID string, isAdmin bool) error
	DeletePhotoComment(ctx context.Context, id bson.ObjectID, requestorID string, isAdmin bool) error
	GetCount(ctx context.Context, photoSeq int64) (int64, error)
}

type service struct {
	repo Repository
	rdb  *redis.Client
}

func NewService(repo Repository, rdb *redis.Client) Service {
	return &service{repo: repo, rdb: rdb}
}

func (s *service) ListPhotoComments(ctx context.Context, photoSeq int64) ([]PhotoCommentResponse, error) {
	comments, err := s.repo.FindByPhotoSeq(ctx, photoSeq)
	if err != nil {
		return nil, fmt.Errorf("list photo comments: %w", err)
	}

	result := make([]PhotoCommentResponse, 0, len(comments))
	for _, c := range comments {
		result = append(result, NewPhotoCommentResponse(c))
	}
	return result, nil
}

func (s *service) CreatePhotoComment(ctx context.Context, req CreatePhotoCommentRequest, authorID, authorEmail, authorName, authorAvatarURL string) error {
	if err := validateContent(req.Content); err != nil {
		return err
	}

	now := time.Now().UTC()
	comment := &model.PhotoComment{
		PhotoSeq:        req.PhotoSeq,
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
		return fmt.Errorf("create photo comment: %w", err)
	}

	s.invalidateCountCache(ctx, req.PhotoSeq)
	return nil
}

func (s *service) UpdatePhotoComment(ctx context.Context, id bson.ObjectID, req UpdatePhotoCommentRequest, requestorID string, isAdmin bool) error {
	if err := validateContent(req.Content); err != nil {
		return err
	}

	if !isAdmin {
		comment, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return fmt.Errorf("find photo comment: %w", err)
		}
		if comment.AuthorID != requestorID {
			return ErrForbidden
		}
	}

	if err := s.repo.UpdateContent(ctx, id, req.Content, time.Now().UTC()); err != nil {
		return fmt.Errorf("update photo comment: %w", err)
	}
	return nil
}

func (s *service) DeletePhotoComment(ctx context.Context, id bson.ObjectID, requestorID string, isAdmin bool) error {
	comment, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find photo comment: %w", err)
	}

	if !isAdmin && comment.AuthorID != requestorID {
		return ErrForbidden
	}

	if err := s.repo.SoftDelete(ctx, id, time.Now().UTC()); err != nil {
		return fmt.Errorf("delete photo comment: %w", err)
	}

	s.invalidateCountCache(ctx, comment.PhotoSeq)
	return nil
}

func (s *service) GetCount(ctx context.Context, photoSeq int64) (int64, error) {
	key := countCachePrefix + strconv.FormatInt(photoSeq, 10)

	if val, err := s.rdb.Get(ctx, key).Result(); err == nil {
		if count, err := strconv.ParseInt(val, 10, 64); err == nil {
			return count, nil
		}
	}

	count, err := s.repo.CountByPhotoSeq(ctx, photoSeq)
	if err != nil {
		return 0, fmt.Errorf("get count: %w", err)
	}

	s.rdb.Set(ctx, key, count, countCacheTTL)
	return count, nil
}

func (s *service) invalidateCountCache(ctx context.Context, photoSeq int64) {
	s.rdb.Del(ctx, countCachePrefix+strconv.FormatInt(photoSeq, 10))
}

func validateContent(content string) error {
	length := len([]rune(content))
	if length < 1 || length > maxContentLen {
		return fmt.Errorf("%w: 내용은 1자 이상 %d자 이하여야 합니다", ErrInvalidInput, maxContentLen)
	}
	return nil
}
