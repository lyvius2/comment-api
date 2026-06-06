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

// ListComments godoc
// @Summary      게시물 댓글 목록 조회
// @Description  postSeq에 해당하는 댓글을 트리 구조로 반환합니다.
// @Tags         comments
// @Produce      json
// @Param        postSeq  query   int64   true  "게시물 ID"
// @Success      200  {object}  response.Response{data=[]CommentResponse}
// @Failure      400  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/comments [get]
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

// CreateComment godoc
// @Summary      댓글 작성
// @Description  게시물에 새 댓글을 작성합니다. COMMENT_SESSION 쿠키가 필요합니다.
// @Tags         comments
// @Accept       json
// @Produce      json
// @Param        request  body  CreateCommentRequest  true  "댓글 작성 요청"
// @Security     CommentSession
// @Success      201  {object}  response.Response
// @Failure      400  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/comments [post]
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

// CreateReply godoc
// @Summary      답글 작성
// @Description  특정 댓글에 답글을 작성합니다. 최대 depth=2까지 허용됩니다. COMMENT_SESSION 쿠키가 필요합니다.
// @Tags         comments
// @Accept       json
// @Produce      json
// @Param        commentId  path  string              true  "부모 댓글 ID (ObjectID hex)"
// @Param        request    body  CreateReplyRequest  true  "답글 작성 요청"
// @Security     CommentSession
// @Success      201  {object}  response.Response
// @Failure      400  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Failure      404  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/comments/{commentId}/replies [post]
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

// UpdateComment godoc
// @Summary      댓글 수정
// @Description  댓글 내용을 수정합니다. 작성자(CommentSession) 또는 관리자(LifelogSession)만 수정 가능합니다.
// @Tags         comments
// @Accept       json
// @Produce      json
// @Param        commentId  path  string               true  "댓글 ID (ObjectID hex)"
// @Param        request    body  UpdateCommentRequest  true  "댓글 수정 요청"
// @Security     CommentSession
// @Security     LifelogSession
// @Success      200  {object}  response.Response
// @Failure      400  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Failure      404  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/comments/{commentId} [put]
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

// DeleteComment godoc
// @Summary      댓글 삭제
// @Description  댓글을 소프트 삭제합니다. 작성자(CommentSession) 또는 관리자(LifelogSession)만 삭제 가능합니다.
// @Tags         comments
// @Produce      json
// @Param        commentId  path  string  true  "댓글 ID (ObjectID hex)"
// @Security     CommentSession
// @Security     LifelogSession
// @Success      204
// @Failure      400  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Failure      404  {object}  response.Response
// @Failure      500  {object}  response.Response
// @Router       /api/comments/{commentId} [delete]
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

// Heartbeat godoc
// @Summary      세션 유지 (하트비트)
// @Description  COMMENT_SESSION 쿠키의 TTL을 갱신합니다.
// @Tags         comments
// @Security     CommentSession
// @Success      204
// @Failure      401  {object}  response.Response
// @Failure      403  {object}  response.Response
// @Router       /api/comments/user/session/heartbeat [post]
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
