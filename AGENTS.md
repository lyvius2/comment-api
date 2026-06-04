# comment-api — AGENTS.md

> 이 문서는 Claude Code, OpenAI Codex 등 AI 에이전트가 프로젝트를 이해하고 작업할 수 있도록 설계 명세를 기술합니다.

---

## 1. 프로젝트 개요

| 항목 | 내용 |
|------|------|
| 언어 | Go (Golang) 1.26+ |
| 아키텍처 | REST API (Layered: Handler → Service → Repository) |
| 주요 기능 | 댓글/답글 CRUD, GitHub SSO 기반 JWT 인증 |
| HTTP | `net/http` 표준 라이브러리 (Go 1.22+ ServeMux, 별도 프레임워크 없음) |
| MongoDB | `go.mongodb.org/mongo-driver/v2` 공식 드라이버 |
| Redis | `github.com/redis/go-redis/v9` 클라이언트 |
| User API | gRPC (`google.golang.org/grpc`) — 사용자 정보 조회 전용 외부 서비스 |

### 프레임워크를 사용하지 않는 이유

> Go 1.22부터 `net/http` 표준 라이브러리가 **메서드 라우팅**과 **경로 파라미터**를 지원합니다.  
> 이 프로젝트의 API 규모에서는 Gin·Echo 같은 외부 프레임워크 없이 충분히 구현 가능하며,  
> 의존성을 최소화하면 빌드 속도·보안 취약점 관리가 용이합니다.

```go
mux := http.NewServeMux()

// Go 1.22+ — 메서드 + 경로 파라미터 동시 지정
mux.HandleFunc("GET /api/v1/comments", handler.ListComments)
mux.HandleFunc("POST /api/v1/comments", handler.CreateComment)
mux.HandleFunc("POST /api/v1/comments/{commentId}/replies", handler.CreateReply)
mux.HandleFunc("PUT /api/v1/comments/{commentId}", handler.UpdateComment)
mux.HandleFunc("DELETE /api/v1/comments/{commentId}", handler.DeleteComment)

// 경로 파라미터 추출
commentId := r.PathValue("commentId")
```

---

## 2. 의존성 (go.mod)

외부 라이브러리는 **최소 필수**만 사용합니다.

```
go.mongodb.org/mongo-driver/v2          # MongoDB 공식 드라이버
github.com/redis/go-redis/v9            # Redis 클라이언트
github.com/golang-jwt/jwt/v5            # JWT 생성/복호화
golang.org/x/oauth2                     # GitHub OAuth2
github.com/joho/godotenv                # .env 파일 로드
github.com/google/uuid                  # JWT jti 생성용 UUID
google.golang.org/grpc                  # gRPC 클라이언트 (User 서비스 호출)
google.golang.org/protobuf              # Protobuf 직렬화
```

| Java 생태계 | Go 대응 | 비고 |
|-------------|---------|------|
| Spring MVC | `net/http` (표준) | Go 1.22+ 라우팅 내장 |
| Spring Security Filter | `http.Handler` 미들웨어 체인 | 함수 래핑 패턴 |
| jjwt | `golang-jwt/jwt/v5` | HS256 동일 지원 |
| Spring Data JPA / Hibernate | — (제거) | MySQL 직접 연결 없음, gRPC 대체 |
| Spring Data MongoDB | `mongo-driver/v2` | 공식 드라이버 직접 사용 |
| gRPC Stub | `google.golang.org/grpc` | User 서비스 gRPC 클라이언트 |

---

## 3. 디렉터리 구조

```
comment-api/
├── AGENTS.md
├── main.go                          # 진입점: DB 연결 초기화 → 라우터 등록 → 서버 기동
├── go.mod / go.sum
├── .env.example                     # 환경변수 템플릿
├── proto/
│   └── user/
│       ├── user.proto               # UserService Protobuf 정의
│       └── user_grpc.pb.go          # protoc-gen-go-grpc 자동 생성 코드
├── config/
│   └── config.go                    # 환경변수 로드 (godotenv + os.Getenv)
├── router/
│   └── router.go                    # net/http ServeMux 라우터 + 미들웨어 체인
├── pkg/
│   ├── database/
│   │   ├── mongo.go                 # MongoDB 클라이언트 초기화
│   │   └── redis.go                 # Redis 클라이언트 초기화
│   └── response/
│       └── response.go              # 공통 JSON 응답 헬퍼
├── internal/
│   ├── model/
│   │   └── comment.go               # MongoDB BSON 스키마 (Comment)
│   ├── user/
│   │   ├── model.go                 # User 응답 구조체 (gRPC 응답 매핑용)
│   │   └── grpc_client.go           # gRPC UserService 클라이언트 + Cache Aside
│   ├── auth/
│   │   ├── jwt.go                   # JWT 생성 / 복호화
│   │   ├── github_oauth.go          # GitHub OAuth2 처리
│   │   └── middleware.go            # JWT 검증 미들웨어 (http.Handler 래퍼)
│   └── comment/
│       ├── handler.go               # HTTP 핸들러
│       ├── service.go               # 비즈니스 로직
│       └── repository.go            # MongoDB CRUD
```

---

## 4. 데이터베이스 설계

### 4-1. 공통 접속 정보 (localhost 기본값)

| 구분 | 기본 주소 | 환경변수 |
|------|-----------|----------|
| MongoDB | `mongodb://localhost:27017` | `MONGO_URI` |
| Redis | `localhost:6379` | `REDIS_ADDR` |
| User gRPC | `localhost:8080` | `USER_SERVICE_ADDR` |

### 4-2. MongoDB — Comment 컬렉션 BSON 스키마

```json
{
  "_id":          ObjectId,          // MongoDB 자동 생성
  "postSeq":      NumberLong,        // 게시물 식별자 (조회키, 인덱스)
  "parentId":     ObjectId | null,   // 부모 댓글 ID (최상위 댓글은 null)
  "rootId":       ObjectId | null,   // 댓글 최상단 ID (스레드 그룹화)
  "depth":        NumberInt,         // 0=댓글, 1=1단 답글, 2=2단 답글, 3=3단 답글(최대)
  "content":      String,            // 댓글 본문
  "authorId":     String,            // GitHub 사용자 ID
  "authorEmail":  String,            // GitHub 이메일
  "authorName":   String,            // GitHub 표시 이름
  "isDeleted":    Boolean,           // 소프트 삭제 여부
  "deletedAt":    Date | null,       // 삭제 시각
  "createdAt":    Date,
  "updatedAt":    Date
}
```

#### 인덱스
- `{ postSeq: 1 }` — 게시물별 댓글 전체 조회
- `{ postSeq: 1, depth: 1 }` — 게시물별 depth 필터 조회
- `{ parentId: 1 }` — 답글 목록 조회
- `{ rootId: 1 }` — 스레드 전체 조회

#### depth 규칙
> 3단계(depth=2, 0-indexed)를 초과하여 답글을 달 경우,  
> 대상 댓글의 **depth=2 조상(ancestor)**을 `parentId`로 설정하여  
> 3단계 하위에 계속 추가됩니다.

```
depth 0 : 댓글 A
depth 1 :   └─ 답글 B
depth 2 :       └─ 답글 C
depth 2 :           └─ 답글 D  ← D는 C의 형제로 C의 parentId(=B)를 공유
```

### 4-3. User gRPC 서비스 — Proto 정의

외부 User 서비스에 gRPC로 이메일을 전송하여 사용자 정보(관리자 여부 포함)를 조회합니다.

```protobuf
syntax = "proto3";
package user;
option go_package = "comment-api/proto/user";

service UserService {
  // 이메일로 사용자 정보 조회 (권한 검증용)
  rpc GetUserByEmail(GetUserByEmailRequest) returns (UserResponse);
}

message GetUserByEmailRequest {
  string email = 1;
}

message UserResponse {
  string user_id  = 1;
  string email    = 2;
  string username = 3;
  string avatar_url = 4;
  bool   is_admin = 5;
}
```

- gRPC 서버 주소: `localhost:8080` (환경변수 `USER_SERVICE_ADDR`)
- 호출 결과는 Redis에 **Cache Aside Pattern**으로 10분 캐시 (섹션 4-4 참고)

### 4-4. Redis — 키 설계

| 키 패턴 | 타입 | TTL | 설명 |
|---------|------|-----|------|
| `auth:token:{jti}` | String | 24h | JWT ID → userId 매핑 (유효 토큰 목록) |
| `auth:session:{userId}` | Hash | 24h | userId → {email, username, isAdmin} |
| `user:info:{email}` | String (JSON) | 10m | gRPC UserResponse 캐시 (Cache Aside) |
| `rate:limit:{ip}` | String (incr) | 60s | IP별 분당 API 요청 수 (Rate Limiting) |
| `comment:count:{postSeq}` | String | 10m | 게시물별 댓글 수 캐시 |

#### Cache Aside Pattern — `user:info:{email}`

```
권한 검증 요청 (email)
  │
  ├─ 1. Redis GET user:info:{email}
  │       ├─ HIT  → JSON 역직렬화 → UserResponse 반환 (gRPC 호출 생략)
  │       └─ MISS
  │             ├─ 2. gRPC UserService.GetUserByEmail(email) 호출
  │             ├─ 3. Redis SET user:info:{email} <JSON> EX 600 (10분)
  │             └─ 4. UserResponse 반환
  │
  └─ (캐시 무효화) 별도 관리자 API 또는 TTL 만료로 자동 제거
```

---

## 5. 인증 설계 (GitHub SSO + JWT)

> Golang에서 JWT는 `github.com/golang-jwt/jwt/v5` 패키지로 완전히 구현 가능합니다.  
> Java의 `io.jsonwebtoken:jjwt`와 동일한 개념(HS256 서명, Claims 구조)을 지원합니다.

### 5-1. GitHub OAuth2 플로우

```
클라이언트                  서버                        GitHub
   │                         │                             │
   │ GET /auth/github         │                             │
   │────────────────────────>│                             │
   │                         │ 302 Redirect                │
   │<────────────────────────│────────────────────────────>│
   │                         │                             │ 사용자 승인
   │                         │<────────────────────────────│
   │                         │ code=xxx                    │
   │                         │ GitHub AccessToken 교환      │
   │                         │ GitHub 사용자 정보 조회       │
   │                         │ JWT 발급 (HS256)             │
   │                         │ Redis에 token TTL 저장       │
   │ {token: "eyJ..."}       │                             │
   │<────────────────────────│                             │
```

### 5-2. JWT Claims 구조

```go
type Claims struct {
    UserID    string `json:"userId"`
    Email     string `json:"email"`
    Username  string `json:"username"`
    AvatarURL string `json:"avatarUrl"`
    IsAdmin   bool   `json:"isAdmin"`
    jwt.RegisteredClaims  // jti, iat, exp
}
```

- `exp` = 발급 시각 + 24시간
- `jti` = UUID (Redis 키로 활용)
- 댓글/답글 **작성·수정** 시마다 Redis TTL을 24시간 재연장 (Sliding Expiration)

### 5-3. 미들웨어 동작

```
요청 Authorization: Bearer <token>
  │
  ├─ 1. JWT 서명 검증 (Secret Key)
  ├─ 2. exp 만료 검증
  ├─ 3. Redis auth:token:{jti} 존재 여부 확인 (강제 로그아웃 지원)
  ├─ 4. r.Context()에 Claims 저장 (context.WithValue)
  └─ 5. 통과 → 다음 핸들러 실행

// 미들웨어 구현 패턴 (http.Handler 래핑)
func JWTMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 검증 로직 ...
        ctx := context.WithValue(r.Context(), claimsKey, claims)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

---

## 6. API 명세

### 인증

| Method | Path | 설명 | 인증 필요 |
|--------|------|------|-----------|
| GET | `/auth/github` | GitHub OAuth2 인증 시작 (Redirect) | ✗ |
| GET | `/auth/github/callback` | OAuth2 콜백, JWT 반환 | ✗ |
| POST | `/auth/logout` | Redis에서 토큰 삭제 | ✓ |

### 댓글

| Method | Path | 설명 | 인증 필요 |
|--------|------|------|-----------|
| GET | `/api/v1/comments?postSeq={postSeq}` | 게시물 댓글 트리 조회 | ✗ |
| POST | `/api/v1/comments` | 댓글 생성 | ✓ |
| POST | `/api/v1/comments/{commentId}/replies` | 답글 생성 | ✓ |
| PUT | `/api/v1/comments/{commentId}` | 댓글/답글 수정 | ✓ (본인 or 관리자) |
| DELETE | `/api/v1/comments/{commentId}` | 댓글/답글 소프트 삭제 | ✓ (본인 or 관리자) |

### 요청/응답 예시

**댓글 생성 요청**
```json
POST /api/v1/comments
Authorization: Bearer eyJ...
Content-Type: application/json

{
  "postSeq": 1001,
  "content": "좋은 글이네요!"
}
```

**댓글 조회 응답**
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
      "createdAt": "2026-06-04T12:00:00Z",
      "replies": [
        {
          "id": "664a1b2c3d4e5f6a7b8c9d0f",
          "depth": 1,
          "content": "감사합니다!",
          "replies": []
        }
      ]
    }
  ]
}
```

---

## 7. 권한 검증 로직

```
댓글 수정/삭제 요청
  │
  ├─ JWT 복호화 → email 추출
  │
  ├─ [Cache Aside] Redis GET user:info:{email}
  │       ├─ HIT  → UserResponse 역직렬화
  │       └─ MISS → gRPC UserService.GetUserByEmail(email)
  │                     → Redis SET user:info:{email} EX 600
  │
  ├─ UserResponse.is_admin = true  → 허용
  └─ UserResponse.is_admin = false
        ├─ MongoDB: 댓글 authorEmail == 요청자 email → 허용
        └─ 불일치 → 403 Forbidden
```

---

## 8. 환경변수 목록 (.env)

```dotenv
# Server
APP_PORT=8080
APP_ENV=development

# JWT
JWT_SECRET=your-super-secret-key-change-in-production

# GitHub OAuth
GITHUB_CLIENT_ID=your-github-client-id
GITHUB_CLIENT_SECRET=your-github-client-secret
GITHUB_CALLBACK_URL=http://localhost:8080/auth/github/callback

# MongoDB
MONGO_URI=mongodb://localhost:27017
MONGO_DB_NAME=comment_db

# Redis
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

# User gRPC Service
USER_SERVICE_ADDR=localhost:8080
```

---

## 9. AI 에이전트 작업 지침

### 코딩 컨벤션
- 패키지명: 소문자 단수 (`comment`, `auth`, `user`)
- 에러 처리: `fmt.Errorf("context: %w", err)` wrapping 사용
- 로깅: `log/slog` (구조화 로그)
- 컨텍스트: 모든 DB·gRPC 호출에 `context.Context` 전달
- JSON 응답: `encoding/json` 표준 라이브러리 사용 (`json.NewEncoder(w).Encode(v)`)
- HTTP 상태 코드: `http.StatusOK`, `http.StatusCreated` 등 상수 사용
- gRPC 클라이언트: 애플리케이션 시작 시 1회 연결 후 재사용 (의존성 주입)

### 금지 사항
- `panic()` 직접 사용 금지 (main 초기화 제외)
- 전역 변수로 DB·gRPC 클라이언트 노출 금지 (의존성 주입 사용)
- 하드코딩된 비밀값 소스 코드 삽입 금지
- Gin, Echo 등 외부 HTTP 프레임워크 추가 금지
- GORM 등 ORM 라이브러리 추가 금지
- MySQL 직접 연결 금지 (User 정보는 반드시 gRPC 경유)

### proto 코드 생성
```bash
# 의존 도구 설치
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 코드 생성
protoc --go_out=. --go-grpc_out=. proto/user/user.proto
```

### 테스트
- `_test.go` 파일은 각 패키지 디렉터리에 위치
- Mock은 `testify/mock` 사용
- gRPC 클라이언트 Mock: `google.golang.org/grpc` 인터페이스 기반 Mock 구현
- 통합 테스트는 `testcontainers-go`로 실제 DB 구동 권장

### 추가 구현 시 참고
- Rate Limiting: `rate:limit:{ip}` Redis 키 사용 (분당 60회 제한)
- 댓글 수 캐시: 조회 시 Redis `comment:count:{postSeq}` 우선 반환, 없으면 MongoDB count 후 10분 캐시
- 강제 로그아웃: Redis에서 `auth:token:{jti}` 키 삭제로 구현
- User 캐시 무효화: `user:info:{email}` 키 직접 삭제 또는 TTL 만료 자동 제거

