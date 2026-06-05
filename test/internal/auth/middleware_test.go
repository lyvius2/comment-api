package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"comment-api/internal/auth"
)

func TestSessionMiddleware_ValidCommentSession_CallsNext(t *testing.T) {
	rdb, mr := newTestRedis(t)

	session := &auth.CommentSession{UserID: "123", Username: "testuser"}
	data, _ := json.Marshal(session)
	mr.Set(auth.SessionKeyPrefix+"valid-sid", string(data))

	var capturedSession *auth.CommentSession
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSession = auth.CommentSessionFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "valid-sid"})
	rr := httptest.NewRecorder()

	auth.SessionMiddleware(baseConfig(), rdb)(next).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedSession)
	assert.Equal(t, "123", capturedSession.UserID)
	assert.Equal(t, "testuser", capturedSession.Username)
}

func TestSessionMiddleware_InvalidCommentSession_Returns401(t *testing.T) {
	rdb, _ := newTestRedis(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "nonexistent-sid"})
	rr := httptest.NewRecorder()

	auth.SessionMiddleware(baseConfig(), rdb)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestSessionMiddleware_ValidJavaSession_CallsNext(t *testing.T) {
	rdb, mr := newTestRedis(t)

	member := auth.JavaSessionMember{UserID: "admin1", IsAdmin: true}
	data, _ := json.Marshal(member)
	mr.HSet("spring:session:sessions:java-sid", "sessionAttr:loginMember", string(data))

	var capturedMember *auth.JavaSessionMember
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMember = auth.JavaSessionFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "java-sid"})
	rr := httptest.NewRecorder()

	auth.SessionMiddleware(baseConfig(), rdb)(next).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedMember)
	assert.True(t, capturedMember.IsAdmin)
}

func TestSessionMiddleware_InvalidJavaSession_Returns401(t *testing.T) {
	rdb, _ := newTestRedis(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "nonexistent-sid"})
	rr := httptest.NewRecorder()

	auth.SessionMiddleware(baseConfig(), rdb)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestSessionMiddleware_CommentSessionTakesPriorityOverJava(t *testing.T) {
	rdb, mr := newTestRedis(t)

	commentSession := &auth.CommentSession{UserID: "user1"}
	cData, _ := json.Marshal(commentSession)
	mr.Set(auth.SessionKeyPrefix+"comment-sid", string(cData))

	javaSession := auth.JavaSessionMember{UserID: "admin1", IsAdmin: true}
	jData, _ := json.Marshal(javaSession)
	mr.HSet("spring:session:sessions:java-sid", "sessionAttr:loginMember", string(jData))

	var gotComment *auth.CommentSession
	var gotJava *auth.JavaSessionMember
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotComment = auth.CommentSessionFromCtx(r.Context())
		gotJava = auth.JavaSessionFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "COMMENT_SESSION", Value: "comment-sid"})
	req.AddCookie(&http.Cookie{Name: "LIFELOG_SESSION", Value: "java-sid"})
	rr := httptest.NewRecorder()

	auth.SessionMiddleware(baseConfig(), rdb)(next).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, gotComment)
	assert.Equal(t, "user1", gotComment.UserID)
	assert.Nil(t, gotJava, "LIFELOG_SESSION은 무시되어야 함")
}

func TestSessionMiddleware_NoCookies_Returns401(t *testing.T) {
	rdb, _ := newTestRedis(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	auth.SessionMiddleware(baseConfig(), rdb)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}
