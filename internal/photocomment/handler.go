package photocomment

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"go.mongodb.org/mongo-driver/v2/bson"

	"comment-api/internal/auth"
	"comment-api/pkg/response"
)

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// ListPhotoComments godoc
// @Summary      사진 댓글 목록 조회
// @Description  photoSeq에 해당하는 사진 댓글 목록을 반환합니다.
// @Tags         photo-comments
// @Produce      json
// @Param        photoSeq  query   int64   true  "사진 ID"
// @Success      200  {object}  response.Response{data=[]PhotoCommentResponse}
// @Failure      400  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/photo-comments [get]
func (h *Handler) ListPhotoComments(w http.ResponseWriter, r *http.Request) {
	photoSeq, err := parsePositiveInt64(r.URL.Query().Get("photoSeq"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "photoSeq가 올바르지 않습니다.")
		return
	}

	comments, err := h.svc.ListPhotoComments(r.Context(), photoSeq)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "사진 댓글 조회에 실패했습니다.")
		return
	}

	response.Success(w, http.StatusOK, comments)
}

// CreatePhotoComment godoc
// @Summary      사진 댓글 작성
// @Description  사진에 새 댓글을 작성합니다. COMMENT_SESSION 쿠키가 필요합니다.
// @Tags         photo-comments
// @Accept       json
// @Produce      json
// @Param        request  body  CreatePhotoCommentRequest  true  "사진 댓글 작성 요청"
// @Security     CommentSession
// @Success      201  {object}  response.Response
// @Failure      400  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/photo-comments [post]
func (h *Handler) CreatePhotoComment(w http.ResponseWriter, r *http.Request) {
	session := auth.CommentSessionFromCtx(r.Context())
	if session == nil {
		response.Error(w, http.StatusForbidden, "댓글 작성은 회원만 가능합니다.")
		return
	}

	var req CreatePhotoCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "요청 본문이 올바르지 않습니다.")
		return
	}

	if err := h.svc.CreatePhotoComment(r.Context(), req, session.UserID, session.Email, session.Username, session.AvatarURL); err != nil {
		httpError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, nil)
}

// UpdatePhotoComment godoc
// @Summary      사진 댓글 수정
// @Description  사진 댓글 내용을 수정합니다. 작성자(CommentSession) 또는 관리자(LifelogSession)만 수정 가능합니다.
// @Tags         photo-comments
// @Accept       json
// @Produce      json
// @Param        commentId  path  string                    true  "댓글 ID (ObjectID hex)"
// @Param        request    body  UpdatePhotoCommentRequest  true  "사진 댓글 수정 요청"
// @Security     CommentSession
// @Security     LifelogSession
// @Success      200  {object}  response.Response
// @Failure      400  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Failure      404  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/photo-comments/{commentId} [put]
func (h *Handler) UpdatePhotoComment(w http.ResponseWriter, r *http.Request) {
	id, err := parseObjectID(r.PathValue("commentId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "commentId가 올바르지 않습니다.")
		return
	}

	requestorID, isAdmin := extractRequestor(r)

	var req UpdatePhotoCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "요청 본문이 올바르지 않습니다.")
		return
	}

	if err := h.svc.UpdatePhotoComment(r.Context(), id, req, requestorID, isAdmin); err != nil {
		httpError(w, err)
		return
	}

	response.Success(w, http.StatusOK, nil)
}

// DeletePhotoComment godoc
// @Summary      사진 댓글 삭제
// @Description  사진 댓글을 소프트 삭제합니다. 작성자(CommentSession) 또는 관리자(LifelogSession)만 삭제 가능합니다.
// @Tags         photo-comments
// @Produce      json
// @Param        commentId  path  string  true  "댓글 ID (ObjectID hex)"
// @Security     CommentSession
// @Security     LifelogSession
// @Success      204
// @Failure      400  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Failure      404  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/photo-comments/{commentId} [delete]
func (h *Handler) DeletePhotoComment(w http.ResponseWriter, r *http.Request) {
	id, err := parseObjectID(r.PathValue("commentId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "commentId가 올바르지 않습니다.")
		return
	}

	requestorID, isAdmin := extractRequestor(r)

	if err := h.svc.DeletePhotoComment(r.Context(), id, requestorID, isAdmin); err != nil {
		httpError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parsePositiveInt64(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, errors.New("must be a positive integer")
	}
	return v, nil
}

func parseObjectID(s string) (bson.ObjectID, error) {
	return bson.ObjectIDFromHex(s)
}

func extractRequestor(r *http.Request) (requestorID string, isAdmin bool) {
	if session := auth.CommentSessionFromCtx(r.Context()); session != nil {
		return session.UserID, false
	}
	if member := auth.JavaSessionFromCtx(r.Context()); member != nil {
		return member.UserID, member.IsAdmin
	}
	return "", false
}

func httpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "댓글을 찾을 수 없습니다.")
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "권한이 없습니다.")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, err.Error())
	default:
		response.Error(w, http.StatusInternalServerError, "서버 오류가 발생했습니다.")
	}
}
