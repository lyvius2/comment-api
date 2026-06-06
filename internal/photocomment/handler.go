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
