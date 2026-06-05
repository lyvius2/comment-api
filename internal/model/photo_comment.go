package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type PhotoComment struct {
	ID              bson.ObjectID `bson:"_id,omitempty"`
	PhotoSeq        int64         `bson:"photoSeq"`
	Content         string        `bson:"content"`
	AuthorID        string        `bson:"authorId"`
	AuthorEmail     string        `bson:"authorEmail"`
	AuthorName      string        `bson:"authorName"`
	AuthorAvatarURL string        `bson:"authorAvatarUrl"`
	IsDeleted       bool          `bson:"isDeleted"`
	DeletedAt       *time.Time    `bson:"deletedAt"`
	CreatedAt       time.Time     `bson:"createdAt"`
	UpdatedAt       time.Time     `bson:"updatedAt"`
}
