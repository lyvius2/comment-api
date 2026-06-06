package photocomment_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"comment-api/internal/auth"
	"comment-api/internal/photocomment"
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

// ── ListPhotoComments ─────────────────────────────────────────────────────────

func TestListPhotoComments_ValidPhotoSeq_Returns200(t *testing.T) {
	svc := &mockPhotoService{}
	svc.On("ListPhotoComments", mock.Anything, int64(99)).Return([]photocomment.PhotoCommentResponse{
		{ID: "abc", PhotoSeq: 99, Content: "사진 댓글"},
	}, nil)

	h := photocomment.NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/photo-comments?photoSeq=99", nil)
	rr := httptest.NewRecorder()

	h.ListPhotoComments(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	resp := decodeResponse(t, rr)
	assert.True(t, resp.Success)
	svc.AssertExpectations(t)
}

func TestListPhotoComments_MissingPhotoSeq_Returns400(t *testing.T) {
	h := photocomment.NewHandler(&mockPhotoService{})
	req := httptest.NewRequest(http.MethodGet, "/api/photo-comments", nil)
	rr := httptest.NewRecorder()

	h.ListPhotoComments(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, decodeResponse(t, rr).Success)
}

func TestListPhotoComments_ServiceError_Returns500(t *testing.T) {
	svc := &mockPhotoService{}
	svc.On("ListPhotoComments", mock.Anything, int64(1)).Return(nil, errors.New("db error"))

	h := photocomment.NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/photo-comments?photoSeq=1", nil)
	rr := httptest.NewRecorder()

	h.ListPhotoComments(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ── CreatePhotoComment ────────────────────────────────────────────────────────

func TestCreatePhotoComment_WithCommentSession_Returns201(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	svc := &mockPhotoService{}
	svc.On("CreatePhotoComment", mock.Anything,
		photocomment.CreatePhotoCommentRequest{PhotoSeq: 10, Content: "멋진 사진"},
		session.UserID, session.Email, session.Username, session.AvatarURL,
	).Return(nil)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreatePhotoComment))

	body := `{"photoSeq": 10, "content": "멋진 사진"}`
	req := httptest.NewRequest(http.MethodPost, "/api/photo-comments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	svc.AssertExpectations(t)
}

func TestCreatePhotoComment_WithAdminSession_Returns403(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupAdminSession(t, mr, "a-sid", adminMember())

	cfg := baseConfig()
	h := photocomment.NewHandler(&mockPhotoService{})
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreatePhotoComment))

	body := `{"photoSeq": 10, "content": "멋진 사진"}`
	req := httptest.NewRequest(http.MethodPost, "/api/photo-comments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestCreatePhotoComment_InvalidBody_Returns400(t *testing.T) {
	rdb, mr := newTestRedis(t)
	setupCommentSession(t, mr, "c-sid", commentSession())

	cfg := baseConfig()
	h := photocomment.NewHandler(&mockPhotoService{})
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreatePhotoComment))

	req := httptest.NewRequest(http.MethodPost, "/api/photo-comments", strings.NewReader("not-json"))
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreatePhotoComment_ServiceValidationError_Returns400(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	svc := &mockPhotoService{}
	svc.On("CreatePhotoComment", mock.Anything, mock.Anything,
		session.UserID, session.Email, session.Username, session.AvatarURL,
	).Return(photocomment.ErrInvalidInput)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.CreatePhotoComment))

	body := `{"photoSeq": 10, "content": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/photo-comments", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ── UpdatePhotoComment ────────────────────────────────────────────────────────

func TestUpdatePhotoComment_AsAuthor_Returns200(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockPhotoService{}
	svc.On("UpdatePhotoComment", mock.Anything, id,
		photocomment.UpdatePhotoCommentRequest{Content: "수정된 내용"},
		session.UserID, false,
	).Return(nil)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdatePhotoComment))

	body := `{"content": "수정된 내용"}`
	req := httptest.NewRequest(http.MethodPut, "/api/photo-comments/"+idHex, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	svc.AssertExpectations(t)
}

func TestUpdatePhotoComment_AsAdmin_Returns200(t *testing.T) {
	rdb, mr := newTestRedis(t)
	admin := adminMember()
	setupAdminSession(t, mr, "a-sid", admin)

	id, idHex := validObjectID()
	svc := &mockPhotoService{}
	svc.On("UpdatePhotoComment", mock.Anything, id,
		photocomment.UpdatePhotoCommentRequest{Content: "관리자 수정"},
		admin.UserID, true,
	).Return(nil)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdatePhotoComment))

	body := `{"content": "관리자 수정"}`
	req := httptest.NewRequest(http.MethodPut, "/api/photo-comments/"+idHex, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	svc.AssertExpectations(t)
}

func TestUpdatePhotoComment_NotFound_Returns404(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockPhotoService{}
	svc.On("UpdatePhotoComment", mock.Anything, id, mock.Anything, session.UserID, false).
		Return(photocomment.ErrNotFound)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdatePhotoComment))

	req := httptest.NewRequest(http.MethodPut, "/api/photo-comments/"+idHex, strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpdatePhotoComment_Forbidden_Returns403(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockPhotoService{}
	svc.On("UpdatePhotoComment", mock.Anything, id, mock.Anything, session.UserID, false).
		Return(photocomment.ErrForbidden)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.UpdatePhotoComment))

	req := httptest.NewRequest(http.MethodPut, "/api/photo-comments/"+idHex, strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// ── DeletePhotoComment ────────────────────────────────────────────────────────

func TestDeletePhotoComment_AsAuthor_Returns204(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockPhotoService{}
	svc.On("DeletePhotoComment", mock.Anything, id, session.UserID, false).Return(nil)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.DeletePhotoComment))

	req := httptest.NewRequest(http.MethodDelete, "/api/photo-comments/"+idHex, nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	svc.AssertExpectations(t)
}

func TestDeletePhotoComment_AsAdmin_Returns204(t *testing.T) {
	rdb, mr := newTestRedis(t)
	admin := adminMember()
	setupAdminSession(t, mr, "a-sid", admin)

	id, idHex := validObjectID()
	svc := &mockPhotoService{}
	svc.On("DeletePhotoComment", mock.Anything, id, admin.UserID, true).Return(nil)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.DeletePhotoComment))

	req := httptest.NewRequest(http.MethodDelete, "/api/photo-comments/"+idHex, nil)
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "a-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	svc.AssertExpectations(t)
}

func TestDeletePhotoComment_NotFound_Returns404(t *testing.T) {
	rdb, mr := newTestRedis(t)
	session := commentSession()
	setupCommentSession(t, mr, "c-sid", session)

	id, idHex := validObjectID()
	svc := &mockPhotoService{}
	svc.On("DeletePhotoComment", mock.Anything, id, session.UserID, false).Return(photocomment.ErrNotFound)

	cfg := baseConfig()
	h := photocomment.NewHandler(svc)
	wrapped := auth.SessionMiddleware(cfg, rdb)(http.HandlerFunc(h.DeletePhotoComment))

	req := httptest.NewRequest(http.MethodDelete, "/api/photo-comments/"+idHex, nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "c-sid"})
	req.SetPathValue("commentId", idHex)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
