package comment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"comment-api/internal/model"
)

const collectionName = "comments"

var ErrNotFound = errors.New("comment not found")

type Repository interface {
	FindByPostSeq(ctx context.Context, postSeq int64) ([]*model.Comment, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*model.Comment, error)
	Create(ctx context.Context, comment *model.Comment) error
	UpdateContent(ctx context.Context, id bson.ObjectID, content string, updatedAt time.Time) error
	SoftDelete(ctx context.Context, id bson.ObjectID, deletedAt time.Time) error
	CountByPostSeq(ctx context.Context, postSeq int64) (int64, error)
}

type mongoRepository struct {
	col *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &mongoRepository{col: db.Collection(collectionName)}
}

// FindByPostSeq는 게시물의 모든 댓글(삭제 포함)을 생성 순으로 반환합니다.
// 삭제된 댓글도 포함해야 트리 구조를 유지할 수 있습니다.
func (r *mongoRepository) FindByPostSeq(ctx context.Context, postSeq int64) ([]*model.Comment, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}})
	cursor, err := r.col.Find(ctx, bson.M{"postSeq": postSeq}, opts)
	if err != nil {
		return nil, fmt.Errorf("find by postSeq: %w", err)
	}
	defer cursor.Close(ctx)

	var comments []*model.Comment
	if err := cursor.All(ctx, &comments); err != nil {
		return nil, fmt.Errorf("decode comments: %w", err)
	}
	return comments, nil
}

func (r *mongoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*model.Comment, error) {
	var comment model.Comment
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&comment)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find by id: %w", err)
	}
	return &comment, nil
}

func (r *mongoRepository) Create(ctx context.Context, comment *model.Comment) error {
	_, err := r.col.InsertOne(ctx, comment)
	if err != nil {
		return fmt.Errorf("create comment: %w", err)
	}
	return nil
}

func (r *mongoRepository) UpdateContent(ctx context.Context, id bson.ObjectID, content string, updatedAt time.Time) error {
	filter := bson.M{"_id": id, "isDeleted": false}
	update := bson.M{"$set": bson.M{"content": content, "updatedAt": updatedAt}}

	result, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("update comment: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *mongoRepository) SoftDelete(ctx context.Context, id bson.ObjectID, deletedAt time.Time) error {
	filter := bson.M{"_id": id, "isDeleted": false}
	update := bson.M{"$set": bson.M{
		"isDeleted": true,
		"deletedAt": deletedAt,
		"updatedAt": deletedAt,
	}}

	result, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("soft delete comment: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// CountByPostSeq는 삭제되지 않은 댓글 수를 반환합니다.
func (r *mongoRepository) CountByPostSeq(ctx context.Context, postSeq int64) (int64, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"postSeq": postSeq, "isDeleted": false})
	if err != nil {
		return 0, fmt.Errorf("count by postSeq: %w", err)
	}
	return count, nil
}
