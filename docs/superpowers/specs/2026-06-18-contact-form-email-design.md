# 기업 문의 폼 이메일 전송 — 설계 명세

**작성일:** 2026-06-18  
**범위:** 랜딩 페이지의 기업 문의 모달을 기존 SMTP 시스템과 연결해 문의 내용을 이메일로 전송한다.

---

## 배경

`index.html`과 `index_en.html`에는 이미 기업 문의 모달이 구현되어 있으며, `org`, `email`, `phone`, `message` 필드를 포함한다. 폼은 이미 `POST /api/contact`를 호출하고 있지만 해당 엔드포인트가 존재하지 않는다. 프로젝트에는 이미 동작하는 SMTP 이메일 시스템(`common.SendEmail`)과 옵션 기반 설정 저장소(`model/option.go`)가 갖춰져 있다.

---

## 구현 항목

### 1. 새 설정값: `ContactEmail`

문의 메일을 수신할 이메일 주소.

- `common/constants.go`: `var ContactEmail = ""` 추가
- `model/option.go`: 기본값 맵에 `"ContactEmail": ""` 추가, `UpdateOption()` 스위치에 `case "ContactEmail": common.ContactEmail = value` 추가

### 2. `POST /api/contact` 엔드포인트

**파일:** `controller/contact.go` (신규)

**인증:** 없음 — 로그인 전 랜딩 페이지에서 호출되는 공개 엔드포인트.

**요청 바디 (JSON):**
```json
{ "org": "...", "email": "...", "phone": "...", "message": "..." }
```

**유효성 검사:**
- `org`, `email`, `message`는 필수. 누락 시 400 반환.
- `ContactEmail`이 설정되어 있지 않으면 503 `{"message": "문의 수신 이메일이 설정되지 않았습니다"}` 반환.

**성공 시:**
- 네 개 필드를 담은 HTML 이메일 본문 생성.
- `common.SendEmail(제목, common.ContactEmail, htmlBody)` 호출.
- 제목: `[AlRouter] 기업 도입 문의 — <org명>` (`SystemName` 전역 변수 활용).
- 200 `{"message": "ok"}` 반환.

**SMTP 오류 시:** 일반적인 오류 메시지와 함께 500 반환, 실제 오류는 로그에 기록.

### 3. 라우터 등록

**파일:** `router/api-router.go`

기존 공개(인증 불필요) 라우트 그룹에 추가:
```go
r.POST("/api/contact", controller.Contact)
```

`GlobalWebRateLimit` 미들웨어가 이미 적용되어 있어 기본적인 스팸 방지가 된다.

### 4. 어드민 UI — 문의 수신 이메일 입력 필드

**위치:** 시스템 설정 패널의 기존 SMTP 설정 섹션.

**수정 파일:** 구현 단계에서 SMTP 설정을 렌더링하는 React 컴포넌트를 특정한다.

기존 SMTP 필드들과 같은 방식으로 `ContactEmail` 텍스트 입력 필드를 추가한다.

---

## 데이터 흐름

```
브라우저 (랜딩 페이지 모달)
  → POST /api/contact  { org, email, phone, message }
  → controller.Contact()
      필드 유효성 검사
      common.ContactEmail 읽기
      HTML 본문 생성
      common.SendEmail(제목, contactEmail, 본문) 호출
          → SMTP 서버 (기존 설정 사용)
              → 수신자 받은편지함
  ← 200 { message: "ok" }
```

---

## 범위 외

- CAPTCHA / 봇 차단 (GlobalWebRateLimit으로 기본 방어)
- 문의 내용 DB 저장
- 이메일 본문 다국어 처리
- `index_en.html` 문의 모달의 제출 버튼 누락 → 구현 중 추가

---

## 변경 파일 목록

| 파일 | 변경 내용 |
|------|-----------|
| `common/constants.go` | `var ContactEmail = ""` 추가 |
| `model/option.go` | `ContactEmail` 기본값 및 `UpdateOption` 케이스 추가 |
| `controller/contact.go` | 신규 파일 — Contact 핸들러 구현 |
| `router/api-router.go` | `POST /api/contact` 라우트 등록 |
| 프론트엔드 설정 컴포넌트 | ContactEmail 입력 필드 추가 |
| `web/default/public/index_en.html` | 문의 모달 제출 버튼 추가 |
