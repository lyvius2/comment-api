package comment_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"comment-api/internal/auth"
	"comment-api/internal/comment"
)

// ── 응답 파싱 헬퍼 ────────────────────────────────────────────────────────────

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

func decodeResponse(t *testing.T, rr *httptest.ResponseRecorder) apiResponse {
	t.Helper()
	var resp apiResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	return resp
}

// ── ListComments ─────────────────────────────────────────────────────────────

func TestListComments_ValidPostSeq_Returns200(t *testing.T) {
	svc := &mockCommentService{}
	svc.On("ListComments", mock.Anything, int64(42)).Return([]comment.CommentResponse{
		{ID: "abc", PostSeq: 42, Content: "테스트 댓글", Replies: []comment.CommentResponse{}},
	}, nil)

	h := comment.NewHandler(svc, nil, baseConfig())
	req := httptest.NewRequest(http.MethodGet, "/api/comments?postSeq=42", nil)
	rr := httptest.NewRecorder()

	h.ListComments(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestListComments_MissingPostSeq_Returns400(t *testing.T) {
	h := comment.NewHandler(&mockCommentService{}, nil, baseConfig())
	req := httptest.NewRequest(http.MethodGet, "/api/comments", nil)
	rr := httptest.NewRecorder()

	h.ListComments(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, decodeResponse(t, rr).Success)
}

func TestListComments_NegativePostSeq_Returns400(t *testing.T) {
	h := comment.NewHandler(&mockCommentService{}, nil, baseConfig())
	req := httptest.NewRequest(http.MethodGet, "/api/comments?postSeq=-1", nil)
	rr := httptest.NewRecorder()

	h.ListComments(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestListComments_ServiceError_Returns500(t *testing.T) {
	svc := &mockCommentService{}
	svc.On("ListComments", mock.Anything, int64(1)).Return(nil, errors.New("db error"))

	h := comment.NewHandler(svc, nil, baseConfig())
	req := httptest.NewRequest(http.MethodGet, "/api/comments?postSeq=1", nil)
	rr := httptest.NewRecorder()

	h.ListComments(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ── CreateComment ─────────────────────────────────────────────────────────────

func TestCreateComment_WithCommentSession_Returns201(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	svc := &mockCommentService{}
	svc.On("CreateComment", mock.Anything,
		comment.CreateCommentRequest{PostSeq: 1, Content: "Hello"},
		session.UserID, session.Email, session.Username, session.AvatarURL,
	).Return(nil)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateComment))

	body := `{"postSeq": 1, "content": "Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/comments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	svc.AssertExpectations(t)
}

func TestCreateComment_WithAdminSession_Returns403(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupAdminSession(t, mr, "a-sid", adminMember())

	cfg := baseConfig()
	h := comment.NewHandler(&mockCommentService{}, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateComment))

	body := `{"postSeq": 1, "content": "Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/comments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestCreateComment_InvalidBody_Returns400(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupCommentSession(t, mr, "c-sid", commentSession())

	cfg := baseConfig()
	h := comment.NewHandler(&mockCommentService{}, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateComment))

	req := httptest.NewRequest(http.MethodPost, "/api/comments", strings.NewReader("not-json"))
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateComment_ServiceValidationError_Returns400(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	svc := &mockCommentService{}
	svc.On("CreateComment", mock.Anything, mock.Anything,
		session.UserID, session.Email, session.Username, session.AvatarURL,
	).Return(comment.ErrInvalidInput)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateComment))

	body := `{"postSeq": 1, "content": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/comments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── CreateReply ───────────────────────────────────────────────────────────────

func TestCreateReply_WithCommentSession_Returns201(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	parentID, parentHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("CreateReply", mock.Anything, parentID,
		comment.CreateReplyRequest{Content: "답글"},
		session.UserID, session.Email, session.Username, session.AvatarURL,
	).Return(nil)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateReply))

	body := `{"content": "답글"}`
	req := httptest.NewRequest(http.MethodPost, "/api/comments/"+parentHex+"/replies", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", parentHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	svc.AssertExpectations(t)
}

func TestCreateReply_WithAdminSession_Returns403(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupAdminSession(t, mr, "a-sid", adminMember())

	_, parentHex := validObjectID()
	cfg := baseConfig()
	h := comment.NewHandler(&mockCommentService{}, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateReply))

	req := httptest.NewRequest(http.MethodPost, "/api/comments/"+parentHex+"/replies", strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	req.SetPathValue("commentId", parentHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestCreateReply_InvalidCommentId_Returns400(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupCommentSession(t, mr, "c-sid", commentSession())

	cfg := baseConfig()
	h := comment.NewHandler(&mockCommentService{}, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateReply))

	req := httptest.NewRequest(http.MethodPost, "/api/comments/not-valid-id/replies", strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", "not-valid-id")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateReply_ParentNotFound_Returns404(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	parentID, parentHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("CreateReply", mock.Anything, parentID, mock.Anything,
		session.UserID, session.Email, session.Username, session.AvatarURL,
	).Return(comment.ErrNotFound)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreateReply))

	req := httptest.NewRequest(http.MethodPost, "/api/comments/"+parentHex+"/replies", strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", parentHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// ── UpdateComment ─────────────────────────────────────────────────────────────

func TestUpdateComment_AsAuthor_Returns200(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("UpdateComment", mock.Anything, id,
		comment.UpdateCommentRequest{Content: "수정된 댓글"},
		session.UserID, false,
	).Return(nil)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdateComment))

	body := `{"content": "수정된 댓글"}`
	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+idHex, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	svc.AssertExpectations(t)
}

func TestUpdateComment_AsAdmin_Returns200(t *testing.T) {
	rdb, mr := newTestRedis(t)
	admin := adminMember()
	setupAdminSession(t, mr, "a-sid", admin)

	id, idHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("UpdateComment", mock.Anything, id,
		comment.UpdateCommentRequest{Content: "관리자 수정"},
		admin.UserID, true,
	).Return(nil)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdateComment))

	body := `{"content": "관리자 수정"}`
	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+idHex, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	svc.AssertExpectations(t)
}

func TestUpdateComment_NotFound_Returns404(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("UpdateComment", mock.Anything, id, mock.Anything, session.UserID, false).
		Return(comment.ErrNotFound)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdateComment))

	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+idHex, strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpdateComment_Forbidden_Returns403(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("UpdateComment", mock.Anything, id, mock.Anything, session.UserID, false).
		Return(comment.ErrForbidden)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdateComment))

	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+idHex, strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// ── DeleteComment ─────────────────────────────────────────────────────────────

func TestDeleteComment_AsAuthor_Returns204(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("DeleteComment", mock.Anything, id, session.UserID, false).Return(nil)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.DeleteComment))

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+idHex, nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	svc.AssertExpectations(t)
}

func TestDeleteComment_AsAdmin_Returns204(t *testing.T) {
	rdb, mr := newTestRedis(t)
	admin := adminMember()
	setupAdminSession(t, mr, "a-sid", admin)

	id, idHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("DeleteComment", mock.Anything, id, admin.UserID, true).Return(nil)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.DeleteComment))

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+idHex, nil)
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	svc.AssertExpectations(t)
}

func TestDeleteComment_NotFound_Returns404(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockCommentService{}
	svc.On("DeleteComment", mock.Anything, id, session.UserID, false).Return(comment.ErrNotFound)

	cfg := baseConfig()
	h := comment.NewHandler(svc, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.DeleteComment))

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+idHex, nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// ── Heartbeat ─────────────────────────────────────────────────────────────────

func TestHeartbeat_WithCommentSession_ExtendsTTL_Returns204(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupCommentSession(t, mr, "c-sid", commentSession())

	cfg := baseConfig()
	h := comment.NewHandler(&mockCommentService{}, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.Heartbeat))

	req := httptest.NewRequest(http.MethodPost, "/api/comments/user/session/heartbeat", nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, time.Duration(600)*time.Second, mr.TTL(auth.SessionKeyPrefix+"c-sid"))
}

func TestHeartbeat_WithAdminSession_Returns403(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupAdminSession(t, mr, "a-sid", adminMember())

	cfg := baseConfig()
	h := comment.NewHandler(&mockCommentService{}, rdb, cfg)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.Heartbeat))

	req := httptest.NewRequest(http.MethodPost, "/api/comments/user/session/heartbeat", nil)
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}
