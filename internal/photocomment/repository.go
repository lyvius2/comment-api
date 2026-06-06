package photocomment

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

const collectionName = "photoComments"

var ErrNotFound = errors.New("photo comment not found")

type Repository interface {
	FindByPhotoSeq(ctx context.Context, photoSeq int64) ([]*model.PhotoComment, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*model.PhotoComment, error)
	Create(ctx context.Context, comment *model.PhotoComment) error
	UpdateContent(ctx context.Context, id bson.ObjectID, content string, updatedAt time.Time) error
	SoftDelete(ctx context.Context, id bson.ObjectID, deletedAt time.Time) error
	CountByPhotoSeq(ctx context.Context, photoSeq int64) (int64, error)
}

type mongoRepository struct {
	col *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &mongoRepository{col: db.Collection(collectionName)}
}

// FindByPhotoSeq는 사진의 모든 댓글(삭제 포함)을 생성 순으로 반환합니다.
func (r *mongoRepository) FindByPhotoSeq(ctx context.Context, photoSeq int64) ([]*model.PhotoComment, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}})
	cursor, err := r.col.Find(ctx, bson.M{"photoSeq": photoSeq}, opts)
	if err != nil {
		return nil, fmt.Errorf("find by photoSeq: %w", err)
	}
	defer cursor.Close(ctx)

	var comments []*model.PhotoComment
	if err := cursor.All(ctx, &comments); err != nil {
		return nil, fmt.Errorf("decode photo comments: %w", err)
	}
	return comments, nil
}

func (r *mongoRepository) FindByID(ctx context.Context, id bson.ObjectID) (*model.PhotoComment, error) {
	var comment model.PhotoComment
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&comment)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find by id: %w", err)
	}
	return &comment, nil
}

func (r *mongoRepository) Create(ctx context.Context, comment *model.PhotoComment) error {
	_, err := r.col.InsertOne(ctx, comment)
	if err != nil {
		return fmt.Errorf("create photo comment: %w", err)
	}
	return nil
}

func (r *mongoRepository) UpdateContent(ctx context.Context, id bson.ObjectID, content string, updatedAt time.Time) error {
	filter := bson.M{"_id": id, "isDeleted": false}
	update := bson.M{"$set": bson.M{"content": content, "updatedAt": updatedAt}}

	result, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("update photo comment: %w", err)
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
		return fmt.Errorf("soft delete photo comment: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// CountByPhotoSeq는 삭제되지 않은 댓글 수를 반환합니다.
func (r *mongoRepository) CountByPhotoSeq(ctx context.Context, photoSeq int64) (int64, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"photoSeq": photoSeq, "isDeleted": false})
	if err != nil {
		return 0, fmt.Errorf("count by photoSeq: %w", err)
	}
	return count, nil
}
