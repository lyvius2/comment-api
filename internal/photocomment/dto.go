package photocomment

import (
	"time"

	"comment-api/internal/model"
)

// ── 요청 DTO ────────────────────────────────────────────────────────────────

type CreatePhotoCommentRequest struct {
	PhotoSeq int64  `json:"photoSeq"`
	Content  string `json:"content"`
}

type UpdatePhotoCommentRequest struct {
	Content string `json:"content"`
}

// ── 응답 DTO ────────────────────────────────────────────────────────────────

type PhotoCommentResponse struct {
	ID              string    `json:"id"`
	PhotoSeq        int64     `json:"photoSeq"`
	Content         string    `json:"content"`
	AuthorName      string    `json:"authorName"`
	AuthorEmail     string    `json:"authorEmail"`
	AuthorAvatarURL string    `json:"authorAvatarUrl"`
	CreatedAt       time.Time `json:"createdAt"`
}

const deletedContent = "삭제된 댓글입니다."

// NewPhotoCommentResponse는 model.PhotoComment를 PhotoCommentResponse로 변환합니다.
// isDeleted=true인 경우 content를 대체 문구로 치환합니다.
func NewPhotoCommentResponse(c *model.PhotoComment) PhotoCommentResponse {
	content := c.Content
	if c.IsDeleted {
		content = deletedContent
	}

	return PhotoCommentResponse{
		ID:              c.ID.Hex(),
		PhotoSeq:        c.PhotoSeq,
		Content:         content,
		AuthorName:      c.AuthorName,
		AuthorEmail:     c.AuthorEmail,
		AuthorAvatarURL: c.AuthorAvatarURL,
		CreatedAt:       c.CreatedAt,
	}
}
