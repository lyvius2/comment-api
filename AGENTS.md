# comment-api — AGENTS.md

> 이 문서는 Claude Code, OpenAI Codex 등 AI 에이전트가 프로젝트를 이해하고 작업할 수 있도록 설계 명세를 기술합니다.

---

## 1. 프로젝트 개요

| 항목 | 내용 |
|------|------|
| 언어 | Go (Golang) 1.26+ |
| 아키텍처 | REST API (Layered: Handler → Service → Repository) |
| 주요 기능 | 댓글/답글 CRUD, GitHub SSO 기반 JWT 인증 |
| 프레임워크 | Gin (HTTP), GORM (MySQL), mongo-driver (MongoDB), go-redis (Redis) |

---

## 2. 디렉터리 구조

```
comment-api/
├── AGENTS.md
├── main.go                          # 진입점: DB 연결 초기화 → 라우터 등록 → 서버 기동
├── go.mod / go.sum
├── config/
│   └── config.go                    # 환경변수 및 설정 로드 (godotenv)
├── router/
│   └── router.go                    # Gin 라우터 및 미들웨어 등록
├── pkg/
│   ├── database/
│   │   ├── mongo.go                 # MongoDB 클라이언트 싱글톤
│   │   ├── redis.go                 # Redis 클라이언트 싱글톤
│   │   └── mysql.go                 # GORM MySQL 클라이언트 싱글톤
│   └── response/
│       └── response.go              # 공통 API 응답 구조체
├── internal/
│   ├── model/
│   │   └── comment.go               # MongoDB BSON 스키마 (Comment)
│   ├── user/
│   │   ├── model.go                 # MySQL User 모델 (GORM)
│   │   └── repository.go            # User DB 조회 로직
│   ├── auth/
│   │   ├── jwt.go                   # JWT 생성 / 복호화
│   │   ├── github_oauth.go          # GitHub OAuth2 SSO 처리
│   │   └── middleware.go            # JWT 인증 미들웨어
│   └── comment/
│       ├── handler.go               # HTTP 핸들러 (요청 파싱 → 서비스 호출)
│       ├── service.go               # 비즈니스 로직 (depth 제한, 권한 검증)
│       └── repository.go            # MongoDB CRUD
```

---

## 3. 데이터베이스 설계

### 3-1. 공통 접속 정보 (localhost 기본값)

| DB | 기본 주소 | 환경변수 |
|----|-----------|----------|
| MongoDB | `mongodb://localhost:27017` | `MONGO_URI` |
| Redis | `localhost:6379` | `REDIS_ADDR` |
| MySQL | `root:password@tcp(localhost:3306)/comment_db` | `MYSQL_DSN` |

### 3-2. MongoDB — Comment 컬렉션 BSON 스키마

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

### 3-3. MySQL — User 테이블

```sql
CREATE TABLE `User` (
    `id`         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    `github_id`  VARCHAR(64)  NOT NULL UNIQUE,
    `email`      VARCHAR(255) NOT NULL UNIQUE,
    `username`   VARCHAR(100) NOT NULL,
    `avatar_url` VARCHAR(512),
    `is_admin`   TINYINT(1)   NOT NULL DEFAULT 0,
    `created_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
```

- `is_admin = 1` 인 사용자만 타인 댓글 수정/삭제 가능
- 이메일은 JWT 복호화 결과로 조회 키로 사용

### 3-4. Redis — 키 설계

| 키 패턴 | 타입 | TTL | 설명 |
|---------|------|-----|------|
| `auth:token:{jti}` | String | 24h | JWT ID → userId 매핑 (유효 토큰 목록) |
| `auth:session:{userId}` | Hash | 24h | userId → {email, username, isAdmin} |
| `rate:limit:{ip}` | String (incr) | 60s | IP별 분당 API 요청 수 (Rate Limiting) |
| `comment:count:{postSeq}` | String | 10m | 게시물별 댓글 수 캐시 |

---

## 4. 인증 설계 (GitHub SSO + JWT)

> Golang에서 JWT는 `github.com/golang-jwt/jwt/v5` 패키지로 완전히 구현 가능합니다.  
> Java의 `io.jsonwebtoken:jjwt`와 동일한 개념(HS256 서명, Claims 구조)을 지원합니다.

### 4-1. GitHub OAuth2 플로우

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
   │                         │ MySQL User upsert           │
   │                         │ JWT 발급 (HS256)             │
   │                         │ Redis에 token TTL 저장       │
   │ {token: "eyJ..."}       │                             │
   │<────────────────────────│                             │
```

### 4-2. JWT Claims 구조

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

### 4-3. 미들웨어 동작

```
요청 Authorization: Bearer <token>
  │
  ├─ 1. JWT 서명 검증 (Secret Key)
  ├─ 2. exp 만료 검증
  ├─ 3. Redis auth:token:{jti} 존재 여부 확인 (강제 로그아웃 지원)
  ├─ 4. gin.Context에 Claims 저장
  └─ 5. 통과 → 핸들러 실행
```

---

## 5. API 명세

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

## 6. 권한 검증 로직

```
댓글 수정/삭제 요청
  │
  ├─ JWT 복호화 → email 추출
  ├─ MySQL: SELECT is_admin FROM User WHERE email = ?
  │     ├─ is_admin = 1 → 허용
  │     └─ is_admin = 0
  │           ├─ MongoDB: 댓글 authorEmail == 요청자 email → 허용
  │           └─ 불일치 → 403 Forbidden
```

---

## 7. 환경변수 목록 (.env)

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

# MySQL
MYSQL_DSN=root:password@tcp(localhost:3306)/comment_db?charset=utf8mb4&parseTime=True&loc=Local
```

---

## 8. AI 에이전트 작업 지침

### 코딩 컨벤션
- 패키지명: 소문자 단수 (`comment`, `auth`, `user`)
- 에러 처리: `fmt.Errorf("context: %w", err)` wrapping 사용
- 로깅: `log/slog` (구조화 로그)
- 컨텍스트: 모든 DB 호출에 `context.Context` 전달

### 금지 사항
- `panic()` 직접 사용 금지 (main 초기화 제외)
- 전역 변수로 DB 클라이언트 노출 금지 (의존성 주입 사용)
- 하드코딩된 비밀값 소스 코드 삽입 금지

### 테스트
- `_test.go` 파일은 각 패키지 디렉터리에 위치
- Mock은 `testify/mock` 사용
- 통합 테스트는 `testcontainers-go`로 실제 DB 구동 권장

### 추가 구현 시 참고
- Rate Limiting: `rate:limit:{ip}` Redis 키 사용 (분당 60회 제한)
- 댓글 수 캐시: 조회 시 Redis `comment:count:{postSeq}` 우선 반환, 없으면 MongoDB count 후 10분 캐시
- 강제 로그아웃: Redis에서 `auth:token:{jti}` 키 삭제로 구현

