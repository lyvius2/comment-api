# Comment-API — AGENTS.md

> 이 문서는 Claude Code, OpenAI Codex 등 AI 에이전트가 프로젝트를 이해하고 작업할 수 있도록 설계 명세를 기술합니다.

---

## 1. 프로젝트 개요

| 항목 | 내용 |
|------|------|
| 언어 | Go (Golang) 1.26+ |
| 아키텍처 | REST API (Layered: Handler → Service → Repository) |
| 주요 기능 | 댓글/답글 CRUD, GitHub SSO + 세션 기반 인증  |
| HTTP | `net/http` 표준 라이브러리 (Go 1.22+ ServeMux, 별도 프레임워크 없음) |
| MongoDB | `go.mongodb.org/mongo-driver/v2` 공식 드라이버 |
| Redis | `github.com/redis/go-redis/v9` 클라이언트 |

### 프레임워크를 사용하지 않는 이유

> Go 1.22부터 `net/http` 표준 라이브러리가 **메서드 라우팅**과 **경로 파라미터**를 지원합니다.  
> 이 프로젝트의 API 규모에서는 Gin·Echo 같은 외부 프레임워크 없이 충분히 구현 가능하며,  
> 의존성을 최소화하면 빌드 속도·보안 취약점 관리가 용이합니다.

```go
mux := http.NewServeMux()

// Go 1.22+ — 메서드 + 경로 파라미터 동시 지정
mux.HandleFunc("GET /api/comments", handler.ListComments)
mux.HandleFunc("POST /api/comments", handler.CreateComment)
mux.HandleFunc("POST /api/comments/{commentId}/replies", handler.CreateReply)
mux.HandleFunc("PUT /api/comments/{commentId}", handler.UpdateComment)
mux.HandleFunc("DELETE /api/comments/{commentId}", handler.DeleteComment)

// 경로 파라미터 추출
commentId := r.PathValue("commentId")
```

---

## 2. 의존성 (go.mod)

외부 라이브러리는 **최소 필수**만 사용합니다.

```
go.mongodb.org/mongo-driver/v2          # MongoDB 공식 드라이버
github.com/redis/go-redis/v9            # Redis 클라이언트
golang.org/x/oauth2                     # GitHub OAuth2
github.com/joho/godotenv                # .env 파일 로드
github.com/google/uuid                  # 세션 ID 생성용 UUID
```

| Java 생태계 | Go 대응 | 비고 |
|-------------|---------|------|
| Spring MVC | `net/http` (표준) | Go 1.22+ 라우팅 내장 |
| Spring Security Filter | `http.Handler` 미들웨어 체인 | 함수 래핑 패턴 |
| Spring Session (Redis) | `redis/go-redis/v9` 직접 조회 | 세션 키/직렬화 포맷 공유 |
| Spring Data MongoDB | `mongo-driver/v2` | 공식 드라이버 직접 사용 |

---

## 3. 디렉터리 구조

```
comment-api/
├── AGENTS.md
├── main.go                          # 진입점: DB 연결 초기화 → 라우터 등록 → 서버 기동
├── go.mod / go.sum
├── .env.example                     # 환경변수 템플릿
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
│   │   ├── comment.go               # MongoDB BSON 스키마 (Comment)
│   │   └── photo_comment.go         # MongoDB BSON 스키마 (PhotoComment)
│   ├── auth/
│   │   ├── github_oauth.go          # GitHub OAuth2 처리 + Go 세션 생성
│   │   ├── session.go               # 세션 생성/조회/삭제 (Go 세션 + Java 세션 읽기)
│   │   └── middleware.go            # 세션 검증 미들웨어 (http.Handler 래퍼)
│   ├── comment/
│   │   ├── handler.go               # HTTP 핸들러
│   │   ├── service.go               # 비즈니스 로직
│   │   └── repository.go            # MongoDB CRUD
│   └── photocomment/
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

### 4-2. MongoDB — Comment 컬렉션 BSON 스키마

```json
{
  "_id":          ObjectId,          // MongoDB 자동 생성
  "postSeq":      NumberLong,        // 게시물 식별자 (조회키, 인덱스)
  "parentId":     ObjectId | null,   // 부모 댓글 ID (최상위 댓글은 null)
  "rootId":       ObjectId | null,   // 댓글 최상단 ID (스레드 그룹화)
  "depth":        NumberInt,         // 0=댓글, 1=1단 답글, 2=2단 답글, 3=3단 답글(최대)
  "content":      String,            // 댓글 본문
  "authorId":        String,          // GitHub 사용자 ID
  "authorEmail":     String,          // GitHub 이메일
  "authorName":      String,          // GitHub 표시 이름
  "authorAvatarUrl": String,          // GitHub 프로필 이미지 URL
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

### 4-3. MongoDB — PhotoComment 컬렉션 BSON 스키마

photo 댓글은 답글 기능이 없으므로 `parentId`, `rootId`, `depth` 필드를 갖지 않습니다.

```json
{
  "_id":          ObjectId,          // MongoDB 자동 생성
  "photoSeq":     NumberLong,        // 사진 식별자 (조회키, 인덱스)
  "content":      String,            // 댓글 본문
  "authorId":        String,          // GitHub 사용자 ID
  "authorEmail":     String,          // GitHub 이메일
  "authorName":      String,          // GitHub 표시 이름
  "authorAvatarUrl": String,          // GitHub 프로필 이미지 URL
  "isDeleted":    Boolean,           // 소프트 삭제 여부
  "deletedAt":    Date | null,       // 삭제 시각
  "createdAt":    Date,
  "updatedAt":    Date
}
```

#### 인덱스
- `{ photoSeq: 1 }` — 사진별 댓글 전체 조회 (단일 평면 목록)

### 4-4. Redis — 키 설계

세션은 두 출처에서 관리됩니다.

| 키 패턴 | 타입 | TTL | 출처 | 설명 |
|---------|------|-----|------|------|
| `spring:session:sessions:{sessionId}` | Hash | Java 관리 | lifelog (Java) | 관리자 인증용 Java 세션 |
| `comment:session:{sessionId}` | String (JSON) | 10m | comment-api (Go) | GitHub SSO 일반 사용자 세션 |
| `rate:limit:{ip}` | String (incr) | 60s | comment-api (Go) | IP별 분당 API 요청 수 (Rate Limiting) |
| `comment:count:{postSeq}` | String | 10m | comment-api (Go) | 게시물별 댓글 수 캐시 |
| `comment:count:photo:{photoSeq}` | String | 10m | comment-api (Go) | 사진별 댓글 수 캐시 |

#### Java 세션 구조 (spring:session:sessions:{sessionId})

lifelog(Java)는 Spring Session + Redis를 사용합니다.  
Go에서 읽으려면 **Java 측이 반드시 JSON 직렬화**를 사용해야 합니다.

```
// Redis Hash 구조
HGETALL spring:session:sessions:{sessionId}
  lastAccessedTime       → 숫자 (epoch ms)
  maxInactiveInterval    → 숫자 (초)
  creationTime           → 숫자 (epoch ms)
  sessionAttr:loginMember → JSON 문자열  ← Go가 읽는 필드
```

```go
// Go — Java 세션에서 관리자 정보 추출 예시
type JavaSessionMember struct {
    UserID   string `json:"userId"`
    Email    string `json:"email"`
    Username string `json:"username"`
    IsAdmin  bool   `json:"isAdmin"`
}

func getJavaSession(ctx context.Context, rdb *redis.Client, sessionID string) (*JavaSessionMember, error) {
    key := "spring:session:sessions:" + sessionID
    val, err := rdb.HGet(ctx, key, "sessionAttr:loginMember").Result()
    if err != nil {
        return nil, err  // redis.Nil 포함 — 세션 없음 → 401
    }
    var member JavaSessionMember
    if err := json.Unmarshal([]byte(val), &member); err != nil {
        // 역직렬화 실패 시 기본값(IsAdmin=false)을 신뢰하지 말고 반드시 오류 반환
        return nil, fmt.Errorf("java session deserialize: %w", err)
    }
    if !member.IsAdmin {
        return nil, errors.New("not admin")
    }
    return &member, nil
}
```

> **주의사항:**
> - `sessionAttr:loginMember`의 키 이름은 lifelog의 `session.setAttribute(키, member)` 호출명과 일치해야 합니다.
> - Java 측 직렬화 설정: `GenericJackson2JsonRedisSerializer` **필수**. Java 기본 직렬화 사용 불가.
> - **lifelog 측 요구사항:** `LIFELOG_SESSION` 쿠키는 반드시 `HttpOnly`, `Secure`, `SameSite=Lax` 속성으로 발급해야 합니다.

#### Go 세션 구조 (comment:session:{sessionId})

```go
type CommentSession struct {
    UserID    string `json:"userId"`    // GitHub 사용자 ID
    Email     string `json:"email"`
    Username  string `json:"username"`
    AvatarURL string `json:"avatarUrl"`
    CreatedAt string `json:"createdAt"` // RFC3339
}
```

```
// Redis String (JSON)
GET comment:session:{sessionId}
→ {"userId":"12345678","email":"user@example.com","username":"octocat",...}
```

세션 ID는 `github.com/google/uuid`로 생성한 UUID v4를 사용합니다.

#### Heartbeat — TTL 10분 연장

```
EXPIRE comment:session:{sessionId} 600
```

`POST /api/comments/session/heartbeat` 호출 시 위 명령으로 TTL을 현재 시각 기준 10분으로 재설정합니다.


---

## 5. 인증 설계 (세션 기반)

모든 인증은 **Cookie + Redis 세션**으로 처리합니다.  
세션은 두 종류이며 쿠키 이름으로 구분합니다.

| 쿠키 이름 | 세션 출처              | 용도 |
|-----------|--------------------|------|
| `LIFELOG_SESSION` | [lifelog](https://github.com/lyvius2/lifelog.git) (Java) | 관리자 인증 |
| `COMMENT_SESSION` | comment-api (Go)   | 일반 사용자 인증 |

---

### 5-1. GitHub OAuth2 플로우 (Go 세션 생성)

```
클라이언트                      서버                        GitHub
   │                             │                             │
   │ GET /auth/github             │                             │
   │────────────────────────────>│                             │
   │                             │ state = UUID v4             │
   │                             │ Redis SET oauth:state:{state} EX 300 (5분)
   │ Set-Cookie: OAUTH_STATE={state}; HttpOnly; SameSite=Lax; Max-Age=300
   │<────────────────────────────│                             │
   │                             │ 302 Redirect (state 포함)   │
   │<────────────────────────────│────────────────────────────>│
   │                             │                             │ 사용자 승인
   │                             │<────────────────────────────│
   │                             │ code=xxx, state=xxx         │
   │                             │ ① 쿠키 OAUTH_STATE == state 검증 (클라이언트 바인딩)
   │                             │ ② Redis DEL oauth:state:{state} (재사용 방지)
   │                             │ GitHub AccessToken 교환     │
   │                             │ GitHub 사용자 정보 조회      │
   │                             │ sessionId = UUID v4         │
   │                             │ Redis SET comment:session:{sessionId} EX 600
   │ Set-Cookie: COMMENT_SESSION={sessionId}; HttpOnly; Secure; SameSite=Lax; Path=/
   │ Set-Cookie: OAUTH_STATE=; Max-Age=0  (임시 쿠키 삭제)
   │<────────────────────────────│                             │
```

> **state 검증 2단계:** Redis 존재 여부(①)와 클라이언트 쿠키 일치(②) 모두 통과해야 합니다.  
> 하나라도 실패하면 400 Bad Request를 반환하고 처리를 중단합니다.

### 5-2. 세션 구조

**Go 세션 (comment:session:{sessionId})**
```go
type CommentSession struct {
    UserID    string `json:"userId"`    // GitHub 사용자 ID (불변값, 권한 검증 기준)
    Email     string `json:"email"`
    Username  string `json:"username"`
    AvatarURL string `json:"avatarUrl"`
    CreatedAt string `json:"createdAt"` // RFC3339
}
```

**Java 세션 (spring:session:sessions:{sessionId})**  
Go는 이 세션을 **읽기 전용**으로만 사용합니다. 관리자 여부 확인 목적.

```go
type JavaSessionMember struct {
    UserID   string `json:"userId"`
    Email    string `json:"email"`
    Username string `json:"username"`
    IsAdmin  bool   `json:"isAdmin"`
}
// Redis: HGET spring:session:sessions:{id} sessionAttr:loginMember
```

### 5-3. 미들웨어 동작

```
요청 (Cookie 포함)
  │
  ├─ COMMENT_SESSION 쿠키 존재?
  │       ├─ YES: Redis GET comment:session:{sessionId}
  │       │         ├─ HIT  → CommentSession 역직렬화 → Context 저장
  │       │         └─ MISS → 401 Unauthorized
  │       └─ NO
  │             ├─ LIFELOG_SESSION 쿠키 존재?
  │             │       ├─ YES: Redis HGET spring:session:sessions:{sessionId} sessionAttr:loginMember
  │             │       │         ├─ HIT  → JavaSessionMember 역직렬화 → Context 저장
  │             │       │         └─ MISS → 401 Unauthorized
  │             │       └─ NO → 401 Unauthorized
  │
  └─ 통과 → 다음 핸들러 실행

// 미들웨어 구현 패턴
func SessionMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 쿠키 → Redis 조회 → Context 저장
        ctx := context.WithValue(r.Context(), sessionKey, sessionInfo)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

### 5-4. 세션 유효 기간 및 Heartbeat

- 세션 TTL: **10분** (Redis `EX 600`)
- `POST /api/comments/session/heartbeat` 호출 시 TTL을 10분으로 재설정
- Heartbeat는 **Go 세션(`COMMENT_SESSION`)만** 연장합니다. Java 세션은 lifelog가 관리합니다.

---

## 6. API 명세

> 인증은 모두 **Cookie** 기반입니다. HTTP Header에 토큰을 넣지 않습니다.  
> `COMMENT_SESSION` 또는 `LIFELOG_SESSION` 쿠키 중 하나가 있어야 인증됩니다.

### 인증

| Method | Path | 설명 | 인증 필요 |
|--------|------|------|-----------|
| GET | `/auth/github` | GitHub OAuth2 인증 시작 (Redirect) | ✗ |
| GET | `/auth/github/callback` | OAuth2 콜백, `COMMENT_SESSION` 쿠키 발급 | ✗ |
| POST | `/auth/logout` | Redis에서 Go 세션 삭제, 쿠키 만료 | ✓ |

### 세션

| Method | Path                                   | 설명 | 인증 필요 |
|--------|----------------------------------------|------|-----------|
| POST | `/api/comments/user/session/heartbeat` | Go 세션 TTL 10분 재연장 | ✓ (`COMMENT_SESSION` 전용) |

### 댓글

| Method | Path | 설명 | 인증 필요 |
|--------|------|------|-----------|
| GET | `/api/comments?postSeq={postSeq}` | 게시물 댓글 트리 조회 | ✗ |
| POST | `/api/comments` | 댓글 생성 | ✓ |
| POST | `/api/comments/{commentId}/replies` | 답글 생성 | ✓ |
| PUT | `/api/comments/{commentId}` | 댓글/답글 수정 | ✓ (본인 or 관리자) |
| DELETE | `/api/comments/{commentId}` | 댓글/답글 소프트 삭제 | ✓ (본인 or 관리자) |

### 사진 댓글

답글 기능 없음. 단순 평면(flat) 목록 구조.

| Method | Path | 설명 | 인증 필요 |
|--------|------|------|-----------|
| GET | `/api/photo-comments?photoSeq={photoSeq}` | 사진 댓글 목록 조회 | ✗ |
| POST | `/api/photo-comments` | 사진 댓글 생성 | ✓ |
| PUT | `/api/photo-comments/{commentId}` | 사진 댓글 수정 | ✓ (본인 or 관리자) |
| DELETE | `/api/photo-comments/{commentId}` | 사진 댓글 소프트 삭제 | ✓ (본인 or 관리자) |

### 요청/응답 예시

**댓글 생성 요청** (쿠키 자동 전송)
```
POST /api/comments
Cookie: COMMENT_SESSION=550e8400-e29b-41d4-a716-446655440000
Content-Type: application/json

{
  "postSeq": 1001,
  "content": "좋은 글이네요!"
}
```

**Heartbeat 요청**
```
POST /api/comments/session/heartbeat
Cookie: COMMENT_SESSION=550e8400-e29b-41d4-a716-446655440000

응답: 204 No Content  (TTL 10분 연장)
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
          "replies": []
        }
      ]
    }
  ]
}
```

**사진 댓글 생성 요청**
```
POST /api/photo-comments
Cookie: COMMENT_SESSION=550e8400-e29b-41d4-a716-446655440000
Content-Type: application/json

{
  "photoSeq": 2001,
  "content": "멋진 사진이네요!"
}
```

**사진 댓글 조회 응답** — 평면 배열, `replies` 필드 없음
```json
{
  "success": true,
  "data": [
    {
      "id": "664a1b2c3d4e5f6a7b8c9d10",
      "photoSeq": 2001,
      "content": "멋진 사진이네요!",
      "authorName": "octocat",
      "authorEmail": "octocat@github.com",
      "authorAvatarUrl": "https://avatars.githubusercontent.com/u/583231",
      "createdAt": "2026-06-04T12:00:00Z"
    }
  ]
}
```

---

## 7. 권한 검증 로직

### 7-1. 세션에서 사용자 식별

```
요청 진입 (SessionMiddleware 통과 후)
  │
  ├─ COMMENT_SESSION 경로: CommentSession.UserID 사용
  └─ LIFELOG_SESSION 경로: JavaSessionMember.IsAdmin 직접 사용
```

### 7-2. 댓글 수정/삭제 권한 판단

```
댓글 수정/삭제 요청
  │
  ├─ LIFELOG_SESSION (관리자 세션)?
  │       └─ JavaSessionMember.IsAdmin = true → 즉시 허용
  │
  └─ COMMENT_SESSION (일반 사용자)?
        ├─ MongoDB: 댓글 authorId == CommentSession.UserID → 허용
        └─ 불일치 → 403 Forbidden
```

> **authorId 기준 검증:** 이메일은 변경 가능하므로 GitHub 사용자 ID(불변)로 소유자를 확인합니다.

---

## 8. 환경변수 목록 (.env)

```dotenv
# Server
APP_PORT=8081
APP_ENV=development                         # production 으로 변경 시 쿠키 Secure=true 적용

# GitHub OAuth
GITHUB_CLIENT_ID=your-github-client-id
GITHUB_CLIENT_SECRET=your-github-client-secret
GITHUB_CALLBACK_URL=https://yourdomain.com/auth/github/callback  # 프로덕션: 반드시 HTTPS

# Session
COMMENT_SESSION_COOKIE=COMMENT_SESSION      # Go 세션 쿠키 이름
LIFELOG_SESSION_COOKIE=LIFELOG_SESSION      # Java 세션 쿠키 이름
LIFELOG_SESSION_ATTR=loginMember            # spring:session:sessions:{id} 의 sessionAttr 키 이름
SESSION_TTL_SECONDS=600                     # 세션 유효 기간 (10분)
SESSION_COOKIE_DOMAIN=yourdomain.com        # 쿠키 Domain 속성 (서브도메인 공유 방지용, 좁게 설정)

# MongoDB (프로덕션: URI에 인증 정보 포함 필수)
MONGO_URI=mongodb://user:password@host:27017  # 예: 인증 없는 URI 사용 금지
MONGO_DB_NAME=comment_db

# Redis (lifelog와 동일한 인스턴스 공유, 프로덕션: PASSWORD 필수)
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=                             # 프로덕션: 반드시 강력한 패스워드 설정
REDIS_DB=0
```

> - `APP_PORT`를 8081로 설정해 lifelog(8080)와 포트 충돌을 방지합니다.
> - `APP_ENV=production` 이면 쿠키 `Secure` 플래그를 활성화하도록 코드에서 분기합니다.
> - 프로덕션 `GITHUB_CALLBACK_URL`은 반드시 `https://`로 시작해야 합니다. HTTP 콜백은 code 탈취 위험이 있습니다.

---

## 9. AI 에이전트 작업 지침

### 코딩 컨벤션
- 패키지명: 소문자 단수 (`comment`, `auth`, `session`)
- 에러 처리: `fmt.Errorf("context: %w", err)` wrapping 사용
- 로깅: `log/slog` (구조화 로그) — **세션 ID·쿠키 값을 로그에 기록 금지**
- 컨텍스트: 모든 DB·Redis 호출에 `context.Context` 전달
- JSON 응답: `encoding/json` 표준 라이브러리 사용 (`json.NewEncoder(w).Encode(v)`)
- HTTP 상태 코드: `http.StatusOK`, `http.StatusCreated` 등 상수 사용
- 쿠키: `HttpOnly=true`, `Secure=(APP_ENV==production)`, `SameSite=Lax`, `Path=/` 고정
- 세션 쿠키 이름: 환경변수(`COMMENT_SESSION_COOKIE`, `LIFELOG_SESSION_COOKIE`)로 관리

### 입력값 검증 규칙
- `postSeq`: `int64`로 파싱 후 양수 여부 검증, 실패 시 400 반환 (MongoDB 쿼리 전 필수)
- `photoSeq`: `int64`로 파싱 후 양수 여부 검증, 실패 시 400 반환 (MongoDB 쿼리 전 필수)
- `commentId`: `primitive.ObjectIDFromHex()` 변환 성공 여부 검증, 실패 시 400 반환
- `content`: 최소 1자, 최대 **1,000자** 제한 (초과 시 400 반환)
- `depth`: 클라이언트 입력 무시 — 서버가 부모 댓글 조회 후 직접 계산 (photo 댓글에는 해당 없음)
- 소프트 삭제 댓글 응답: `isDeleted=true`인 댓글은 `content`를 `"삭제된 댓글입니다."`로 대체하여 반환

### CORS 정책
```go
// 허용 Origin을 환경변수로 관리 (CORS_ALLOWED_ORIGINS)
w.Header().Set("Access-Control-Allow-Origin", allowedOrigin) // 와일드카드(*) 절대 사용 금지
w.Header().Set("Access-Control-Allow-Credentials", "true")   // 쿠키 전송을 위해 필수
w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
```
> `Allow-Origin: *`과 `Allow-Credentials: true`를 동시에 사용하면 브라우저가 거부합니다.  
> 반드시 명시적 Origin을 환경변수로 관리하세요.

### 금지 사항
- `panic()` 직접 사용 금지 (main 초기화 제외)
- 전역 변수로 DB·Redis 클라이언트 노출 금지 (의존성 주입 사용)
- 하드코딩된 비밀값 소스 코드 삽입 금지
- Gin, Echo 등 외부 HTTP 프레임워크 추가 금지
- GORM 등 ORM 라이브러리 추가 금지
- HTTP 응답 헤더에 세션 ID 직접 노출 금지 (쿠키만 사용)
- JSON 역직렬화 실패 시 기본값으로 처리 금지 — 오류 반환 필수 (IsAdmin 기본값 false 신뢰 금지)

### 테스트

#### 기본 규칙
- 테스트 파일은 소스와 **별도 경로**에 위치: `test/` 디렉터리 아래 원본 경로를 그대로 미러링
  ```
  internal/auth/github_oauth.go  →  test/internal/auth/github_oauth_test.go
  internal/comment/handler.go    →  test/internal/comment/handler_test.go
  ```
- 패키지 선언: `package auth_test` (외부 테스트 패키지 — exported 심볼만 사용)
- 테스트 함수명: `TestXxx_상황_기대결과` 형식 (예: `TestLogout_NoCommentSession_Returns403`)
- 실행: `go test ./...` (전체) / `go test ./test/internal/auth/...` (패키지 단위)

#### 의존성
| 용도 | 라이브러리 |
|------|-----------|
| Assert / Mock | `github.com/stretchr/testify` |
| 통합 테스트 (실제 DB·Redis) | `github.com/testcontainers/testcontainers-go` |

#### 단위 테스트
- Mock은 `testify/mock` 사용
- Redis Mock: `go-redis/v9` 의 `UniversalClient` 인터페이스 기반 Mock 구현
- HTTP 핸들러 테스트: `net/http/httptest` 의 `httptest.NewRecorder()` + `httptest.NewRequest()` 사용
- 외부 의존성(GitHub API 등)은 인터페이스로 추상화하여 Mock 교체 가능하게 설계

```go
// 핸들러 단위 테스트 패턴
func TestCallback_InvalidState_Returns400(t *testing.T) {
    rr := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state=bad&code=xxx", nil)

    handler.Callback(rr, req)

    assert.Equal(t, http.StatusBadRequest, rr.Code)
}
```

#### 통합 테스트
- `testcontainers-go`로 실제 Redis·MongoDB 컨테이너를 구동하여 테스트
- 파일명: `xxx_integration_test.go`, 빌드 태그 `//go:build integration` 사용
- 실행: `go test -tags integration ./...`

```go
//go:build integration

func TestSaveSession_Integration(t *testing.T) {
    ctx := context.Background()
    redisC, _ := redis.RunContainer(ctx, ...)
    // 실제 Redis에 저장·조회 검증
}
```

#### 커버리지
- 각 패키지 핵심 로직(service, repository, auth) 커버리지 **80% 이상** 목표
- 확인: `go test -cover ./...`

### 추가 구현 시 참고
- Rate Limiting: `rate:limit:{ip}` Redis 키 사용 (분당 60회 제한), IP는 `r.RemoteAddr` 사용 (`X-Forwarded-For` 신뢰 금지)
- 댓글 수 캐시: 조회 시 Redis `comment:count:{postSeq}` 우선 반환, 없으면 MongoDB count 후 10분 캐시
- 사진 댓글 수 캐시: 조회 시 Redis `comment:count:photo:{photoSeq}` 우선 반환, 없으면 MongoDB count 후 10분 캐시
- 로그아웃: Redis `comment:session:{sessionId}` 삭제 + `Set-Cookie: COMMENT_SESSION=; Max-Age=0; Path=/`
- Java 세션 속성 키: `LIFELOG_SESSION_ATTR` 환경변수 값으로 동적 구성 (`sessionAttr:{attr}`)
- Heartbeat: `POST /api/comments/session/heartbeat` → `EXPIRE comment:session:{sessionId} 600` (Go 세션만 해당)
- CORS: `CORS_ALLOWED_ORIGINS` 환경변수에 콤마 구분으로 허용 Origin 목록 관리 (예: `https://furaiki-lifelog.com`)

