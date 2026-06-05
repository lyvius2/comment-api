# Comment API

Go 기반 댓글 REST API 서버. GitHub SSO + 세션 인증을 사용합니다.  
[Lifelog](https://github.com/lyvius2/lifelog.git) 프로젝트의 댓글 시스템으로 개발 중입니다.

---

## 환경변수 설정

### 1. `.env` 파일 생성

`.env.example`은 필요한 환경변수 목록을 보여주는 **템플릿 파일**입니다.  
이 파일을 복사하여 `.env`를 만들고 실제 값을 채워넣습니다.

```bash
cp .env.example .env
```

### 2. 값 채우기

```dotenv
# GitHub OAuth App 등록 후 발급받은 값
GITHUB_CLIENT_ID=실제_클라이언트_ID
GITHUB_CLIENT_SECRET=실제_시크릿
GITHUB_CALLBACK_URL=http://localhost:8081/auth/github/callback
```

### 3. 서버 기동

```bash
go run main.go
```

서버가 시작되면 `config/config.go`의 `godotenv.Load()`가 `.env` 파일을 자동으로 읽어 환경변수로 등록합니다.

> **프로덕션 환경**에서는 `.env` 파일 없이 시스템 환경변수(Docker, k8s Secret 등)로 직접 주입합니다.  
> `godotenv.Load()` 실패는 무시되므로 서버 기동에 영향을 주지 않습니다.

---

## 파일 구분

| 파일 | 설명 | git 커밋 |
|------|------|----------|
| `.env.example` | 변수 목록 템플릿 (실제 값 없음) | **O** (커밋함) |
| `.env` | 실제 비밀값이 담긴 설정 파일 | **X** (`.gitignore` 제외) |

---

## GitHub OAuth App 등록

1. GitHub → Settings → Developer settings → OAuth Apps → **New OAuth App**
2. 아래 값 입력:

| 항목 | 값 |
|------|----|
| Homepage URL | `http://localhost:8081` (개발) |
| Authorization callback URL | `http://localhost:8081/auth/github/callback` |

3. 발급된 **Client ID** / **Client Secret**을 `.env`에 입력

---

## 인증 엔드포인트

| Method | Path | 설명 |
|--------|------|------|
| GET | `/auth/github` | GitHub 로그인 페이지로 리다이렉트 |
| GET | `/auth/github/callback` | OAuth 콜백 처리, `COMMENT_SESSION` 쿠키 발급 |