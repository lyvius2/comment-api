# comment-api

댓글 및 사진 댓글 CRUD REST API 서버입니다.  
GitHub SSO 기반 세션 인증을 사용하며, 관리자 세션은 [lifelog](https://github.com/lyvius2/lifelog.git)(Java)의 Spring Session을 읽기 전용으로 공유합니다.

---

## 기술 스택

| 구분 | 내용 |
|------|------|
| 언어 | Go 1.26+ |
| HTTP | `net/http` 표준 라이브러리 (Go 1.22+ ServeMux, 외부 프레임워크 없음) |
| DB | MongoDB (`go.mongodb.org/mongo-driver/v2`) |
| 캐시/세션 | Redis (`github.com/redis/go-redis/v9`) |
| 인증 | GitHub OAuth2 (`golang.org/x/oauth2`) |
| API 문서 | swaggo/swag (Swagger UI) |
| 테스트 | testify, miniredis |

---

## 디렉터리 구조

```
comment-api/
├── main.go                          # 진입점: 초기화 → 라우터 등록 → 서버 기동
├── .env.example                     # 환경변수 템플릿
├── config/
│   └── config.go                    # 환경변수 로드
├── router/
│   ├── router.go                    # ServeMux 라우터 + CORS + Rate Limiting
│   └── ratelimit.go                 # IP별 분당 60회 제한 미들웨어
├── pkg/
│   ├── database/
│   │   ├── mongo.go
│   │   └── redis.go
│   └── response/
│       └── response.go              # 공통 JSON 응답 헬퍼
├── internal/
│   ├── model/
│   │   ├── comment.go               # Comment BSON 스키마
│   │   └── photo_comment.go         # PhotoComment BSON 스키마
│   ├── auth/
│   │   ├── github_oauth.go          # GitHub OAuth2 + 세션 발급
│   │   ├── session.go               # 세션 생성/조회/삭제
│   │   └── middleware.go            # 세션 검증 미들웨어
│   ├── comment/
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── dto.go
│   └── photocomment/
│       ├── handler.go
│       ├── service.go
│       ├── repository.go
│       └── dto.go
├── docs/                            # swag init으로 자동 생성 (swagger.json 등)
└── test/                            # 테스트 파일 (소스 경로와 동일하게 미러링)
    ├── internal/
    │   ├── auth/
    │   ├── comment/
    │   └── photocomment/
    └── router/
```

---

## 사전 요구사항

- Go 1.22 이상
- MongoDB
- Redis (lifelog와 동일한 인스턴스 공유)
- GitHub OAuth App 등록 ([GitHub Developer Settings](https://github.com/settings/developers))

---

## 환경 설정 및 실행

### 1. 환경변수 설정

```bash
cp .env.example .env
```

`.env` 파일을 열어 아래 항목을 채웁니다.

```dotenv
# Server
APP_PORT=8081
APP_ENV=development          # production 으로 변경 시 쿠키 Secure=true 적용

# GitHub OAuth
GITHUB_CLIENT_ID=your-github-client-id
GITHUB_CLIENT_SECRET=your-github-client-secret
GITHUB_CALLBACK_URL=http://localhost:8081/auth/github/callback
AUTH_SUCCESS_URL=            # 인증 완료 후 리다이렉트 URL (미설정 시 JSON 응답)

# Session
COMMENT_SESSION_COOKIE=COMMENT_SESSION
LIFELOG_SESSION_COOKIE=LIFELOG_SESSION
LIFELOG_SESSION_ATTR=loginMember
SESSION_TTL_SECONDS=600
SESSION_COOKIE_DOMAIN=       # 로컬 개발 시 비워 두세요

# MongoDB
MONGO_URI=mongodb://localhost:27017
MONGO_DB_NAME=comment_db

# Redis
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

# CORS (콤마로 여러 Origin 지정 가능)
CORS_ALLOWED_ORIGINS=http://localhost:3000
```

### 2. GitHub OAuth App 등록

GitHub → Settings → Developer settings → OAuth Apps → **New OAuth App**

| 항목 | 개발 환경 값 |
|------|-------------|
| Homepage URL | `http://localhost:8081` |
| Authorization callback URL | `http://localhost:8081/auth/github/callback` |

발급된 **Client ID** / **Client Secret**을 `.env`에 입력합니다.

### 3. API 문서 생성 (최초 1회, 소스 변경 시 재실행)

```bash
# swag CLI 설치 (최초 1회)
go install github.com/swaggo/swag/cmd/swag@latest

# docs/ 패키지 생성
swag init -g main.go --parseInternal
```

### 4. 실행

```bash
go run .
```

서버 기동 후 접속 가능한 주소:

| 용도 | URL |
|------|-----|
| API 서버 | `http://localhost:8081` |
| Swagger UI | `http://localhost:8081/swagger/index.html` |

---

## API 목록

인증이 필요한 엔드포인트는 `COMMENT_SESSION` 또는 `LIFELOG_SESSION` 쿠키가 필요합니다.  
모든 인증은 **쿠키 기반**이며 HTTP 헤더에 토큰을 넣지 않습니다.

### 인증 (Auth)

| Method | Path | 설명 | 인증 |
|--------|------|------|------|
| `GET` | `/auth/github` | GitHub OAuth2 로그인 시작 (리다이렉트) | 불필요 |
| `GET` | `/auth/github/callback` | OAuth2 콜백 처리, `COMMENT_SESSION` 쿠키 발급 | 불필요 |
| `POST` | `/auth/logout` | Go 세션 삭제, 쿠키 만료 | `COMMENT_SESSION` |

### 세션 (Session)

| Method | Path | 설명 | 인증 |
|--------|------|------|------|
| `POST` | `/api/comments/user/session/heartbeat` | 세션 TTL 10분 재연장 | `COMMENT_SESSION` |

### 댓글 (Comments)

| Method | Path | 설명 | 인증 |
|--------|------|------|------|
| `GET` | `/api/comments?postSeq={postSeq}` | 게시물 댓글 트리 조회 | 불필요 |
| `POST` | `/api/comments` | 댓글 작성 | `COMMENT_SESSION` |
| `POST` | `/api/comments/{commentId}/replies` | 답글 작성 (최대 depth=2) | `COMMENT_SESSION` |
| `PUT` | `/api/comments/{commentId}` | 댓글 수정 | 본인 또는 관리자 |
| `DELETE` | `/api/comments/{commentId}` | 댓글 소프트 삭제 | 본인 또는 관리자 |

### 사진 댓글 (Photo Comments)

답글 없이 단순 평면(flat) 목록 구조입니다.

| Method | Path | 설명 | 인증 |
|--------|------|------|------|
| `GET` | `/api/photo-comments?photoSeq={photoSeq}` | 사진 댓글 목록 조회 | 불필요 |
| `POST` | `/api/photo-comments` | 사진 댓글 작성 | `COMMENT_SESSION` |
| `PUT` | `/api/photo-comments/{commentId}` | 사진 댓글 수정 | 본인 또는 관리자 |
| `DELETE` | `/api/photo-comments/{commentId}` | 사진 댓글 소프트 삭제 | 본인 또는 관리자 |

### 요청/응답 예시

**댓글 작성**
```http
POST /api/comments
Cookie: COMMENT_SESSION=<session-id>
Content-Type: application/json

{
  "postSeq": 1001,
  "content": "좋은 글이네요!"
}
```
```json
{ "success": true }
```

**댓글 목록 조회 (트리 구조)**
```http
GET /api/comments?postSeq=1001
```
```json
{
  "success": true,
  "data": [
    {
      "id": "664a1b2c3d4e5f6a7b8c9d0e",
      "postSeq": 1001,
      "depth": 0,
      "content": "좋은 글이네요!",
      "authorName": "octocat",
      "authorEmail": "octocat@github.com",
      "authorAvatarUrl": "https://avatars.githubusercontent.com/u/583231",
      "createdAt": "2026-06-04T12:00:00Z",
      "replies": [
        {
          "id": "664a1b2c3d4e5f6a7b8c9d0f",
          "depth": 1,
          "content": "감사합니다!",
          "authorName": "monalisa",
          "authorEmail": "monalisa@github.com",
          "authorAvatarUrl": "https://avatars.githubusercontent.com/u/2",
          "createdAt": "2026-06-04T12:05:00Z",
          "replies": []
        }
      ]
    }
  ]
}
```

**공통 에러 응답**
```json
{ "success": false, "message": "댓글을 찾을 수 없습니다." }
```

---

## 인증 흐름

### GitHub SSO (COMMENT_SESSION 발급)

```
브라우저                         comment-api                    GitHub
   │                                  │                             │
   │  GET /auth/github                │                             │
   │─────────────────────────────────>│ state 생성 → Redis 저장     │
   │  302 + Set-Cookie: OAUTH_STATE   │                             │
   │<─────────────────────────────────│                             │
   │  302 → github.com/oauth/authorize│────────────────────────────>│
   │                                  │                    사용자 승인
   │  GET /auth/github/callback       │<────────────────────────────│
   │─────────────────────────────────>│ code, state 수신            │
   │                                  │ ① 쿠키 OAUTH_STATE 검증     │
   │                                  │ ② Redis state 삭제 (재사용 방지)
   │                                  │ ③ 액세스 토큰 교환          │
   │                                  │ ④ GitHub 사용자 정보 조회   │
   │  Set-Cookie: COMMENT_SESSION     │ 세션 생성 → Redis 저장      │
   │<─────────────────────────────────│                             │
```

### 관리자 인증 (LIFELOG_SESSION, 읽기 전용)

lifelog(Java)가 발급한 `LIFELOG_SESSION` 쿠키를 comment-api가 Redis에서 읽기 전용으로 검증합니다.  
댓글 작성은 불가하며, 수정/삭제에 한해 관리자 권한이 부여됩니다.

### 세션 미들웨어 동작

1. `COMMENT_SESSION` 쿠키 → `comment:session:{id}` (Redis String, JSON) 조회
2. 없으면 `LIFELOG_SESSION` 쿠키 → `spring:session:sessions:{id}` (Redis Hash) 조회
3. 둘 다 없거나 Redis에 해당 키가 없으면 → `401 Unauthorized`

---

## Redis 키 설계

| 키 패턴 | 타입 | TTL | 설명 |
|---------|------|-----|------|
| `comment:session:{sessionId}` | String (JSON) | 10분 | Go 사용자 세션 |
| `spring:session:sessions:{sessionId}` | Hash | Java 관리 | 관리자 세션 (lifelog) |
| `rate:limit:{ip}` | String (incr) | 60초 | IP별 Rate Limiting 카운터 |
| `comment:count:{postSeq}` | String | 10분 | 게시물 댓글 수 캐시 |
| `comment:count:photo:{photoSeq}` | String | 10분 | 사진 댓글 수 캐시 |

---

## 미들웨어 체인

```
요청
 └─ corsMiddleware           (CORS 헤더, OPTIONS preflight 처리)
     └─ RateLimitMiddleware  (IP별 분당 60회 제한)
         └─ SessionMiddleware (인증 필요 라우트에만 개별 적용)
             └─ Handler
```

CORS가 가장 바깥에 위치하므로 OPTIONS preflight는 Rate Limiting을 거치지 않습니다.

---

## 테스트

```bash
# 전체 테스트
go test ./...

# 패키지 단위 실행
go test ./test/internal/auth/...
go test ./test/internal/comment/...
go test ./test/internal/photocomment/...
go test ./test/router/...

# 커버리지 확인
go test -cover ./...
```

테스트 파일은 `test/` 디렉터리 아래 소스 경로와 동일하게 구성되어 있으며,  
Redis는 [miniredis](https://github.com/alicebob/miniredis)를 사용해 외부 의존 없이 단위 테스트를 수행합니다.

| 테스트 파일 | 주요 내용 |
|-------------|-----------|
| `test/internal/auth/` | GitHub OAuth 콜백, 세션 미들웨어, 세션 CRUD |
| `test/internal/comment/` | 댓글 핸들러 21개, 서비스 로직 26개 |
| `test/internal/photocomment/` | 사진 댓글 핸들러, 서비스 로직 18개 |
| `test/router/` | Rate Limiting 미들웨어 7개 |

---

## 파일 관리

| 파일 | 설명 | git 커밋 |
|------|------|----------|
| `.env.example` | 변수 목록 템플릿 (실제 값 없음) | O |
| `.env` | 실제 비밀값이 담긴 설정 파일 | X (`.gitignore` 제외) |
| `docs/` | swag init으로 생성된 Swagger 파일 | O |