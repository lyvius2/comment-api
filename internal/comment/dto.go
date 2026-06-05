package comment

import (
	"time"

	"comment-api/internal/model"
)

// ── 요청 DTO ────────────────────────────────────────────────────────────────

type CreateCommentRequest struct {
	PostSeq int64  `json:"postSeq"`
	Content string `json:"content"`
}

type CreateReplyRequest struct {
	Content string `json:"content"`
}

type UpdateCommentRequest struct {
	Content string `json:"content"`
}

// ── 응답 DTO ────────────────────────────────────────────────────────────────

type CommentResponse struct {
	ID              string            `json:"id"`
	PostSeq         int64             `json:"postSeq,omitempty"`
	Depth           int               `json:"depth"`
	Content         string            `json:"content"`
	AuthorName      string            `json:"authorName"`
	AuthorEmail     string            `json:"authorEmail"`
	AuthorAvatarURL string            `json:"authorAvatarUrl"`
	CreatedAt       time.Time         `json:"createdAt"`
	Replies         []CommentResponse `json:"replies"`
}

const deletedContent = "삭제된 댓글입니다."

// NewCommentResponse는 model.Comment를 CommentResponse로 변환합니다.
// isDeleted=true인 경우 content를 대체 문구로 치환합니다.
func NewCommentResponse(c *model.Comment) CommentResponse {
	content := c.Content
	if c.IsDeleted {
		content = deletedContent
	}

	return CommentResponse{
		ID:              c.ID.Hex(),
		PostSeq:         c.PostSeq,
		Depth:           c.Depth,
		Content:         content,
		AuthorName:      c.AuthorName,
		AuthorEmail:     c.AuthorEmail,
		AuthorAvatarURL: c.AuthorAvatarURL,
		CreatedAt:       c.CreatedAt,
		Replies:         []CommentResponse{},
	}
}
