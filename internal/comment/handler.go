package comment

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"

	"comment-api/config"
	"comment-api/internal/auth"
	"comment-api/pkg/response"
)

type Handler struct {
	svc Service
	rdb *redis.Client
	cfg *config.Config
}

func NewHandler(svc Service, rdb *redis.Client, cfg *config.Config) *Handler {
	return &Handler{svc: svc, rdb: rdb, cfg: cfg}
}

func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	postSeq, err := parsePositiveInt64(r.URL.Query().Get("postSeq"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "postSeq가 올바르지 않습니다.")
		return
	}

	comments, err := h.svc.ListComments(r.Context(), postSeq)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "댓글 조회에 실패했습니다.")
		return
	}

	response.Success(w, http.StatusOK, comments)
}

func (h *Handler) CreateComment(w http.ResponseWriter, r *http.Request) {
	session := auth.CommentSessionFromCtx(r.Context())
	if session == nil {
		response.Error(w, http.StatusForbidden, "댓글 작성은 회원만 가능합니다.")
		return
	}

	var req CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "요청 본문이 올바르지 않습니다.")
		return
	}

	if err := h.svc.CreateComment(r.Context(), req, session.UserID, session.Email, session.Username, session.AvatarURL); err != nil {
		httpError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, nil)
}

func (h *Handler) CreateReply(w http.ResponseWriter, r *http.Request) {
	session := auth.CommentSessionFromCtx(r.Context())
	if session == nil {
		response.Error(w, http.StatusForbidden, "답글 작성은 회원만 가능합니다.")
		return
	}

	id, err := parseObjectID(r.PathValue("commentId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "commentId가 올바르지 않습니다.")
		return
	}

	var req CreateReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "요청 본문이 올바르지 않습니다.")
		return
	}

	if err := h.svc.CreateReply(r.Context(), id, req, session.UserID, session.Email, session.Username, session.AvatarURL); err != nil {
		httpError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, nil)
}

func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	id, err := parseObjectID(r.PathValue("commentId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "commentId가 올바르지 않습니다.")
		return
	}

	requestorID, isAdmin := extractRequestor(r)

	var req UpdateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "요청 본문이 올바르지 않습니다.")
		return
	}

	if err := h.svc.UpdateComment(r.Context(), id, req, requestorID, isAdmin); err != nil {
		httpError(w, err)
		return
	}

	response.Success(w, http.StatusOK, nil)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := parseObjectID(r.PathValue("commentId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "commentId가 올바르지 않습니다.")
		return
	}

	requestorID, isAdmin := extractRequestor(r)

	if err := h.svc.DeleteComment(r.Context(), id, requestorID, isAdmin); err != nil {
		httpError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	session := auth.CommentSessionFromCtx(r.Context())
	if session == nil {
		response.Error(w, http.StatusForbidden, "세션 갱신은 회원만 가능합니다.")
		return
	}

	cookie, err := r.Cookie(h.cfg.CommentSessionCookie)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "세션이 유효하지 않습니다.")
		return
	}

	ttl := time.Duration(h.cfg.SessionTTLSeconds) * time.Second
	h.rdb.Expire(r.Context(), auth.SessionKeyPrefix+cookie.Value, ttl)

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
