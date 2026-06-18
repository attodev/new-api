# 기업 문의 폼 이메일 전송 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 랜딩 페이지의 기업 문의 모달 제출 시, 기존 SMTP 시스템을 통해 담당자 이메일로 문의 내용을 전송한다.

**Architecture:** 백엔드에 `ContactEmail` 옵션 추가 → 공개 `POST /api/contact` 엔드포인트 구현 → 프론트엔드 어드민 설정 화면에 입력 필드 추가. 이메일 전송은 기존 `common.SendEmail()`을 그대로 사용한다.

**Tech Stack:** Go (Gin, GORM), `common.SendEmail`, React 19 + react-hook-form + zod

---

## 파일 목록

| 파일 | 변경 |
|------|------|
| `common/constants.go` | `var ContactEmail = ""` 추가 |
| `model/option.go` | 기본값 맵, `UpdateOption` 케이스 추가 |
| `controller/contact.go` | 신규 — Contact 핸들러 |
| `router/api-router.go` | `POST /api/contact` 등록 |
| `web/default/src/features/system-settings/types.ts` | `OperationsSettings`에 `ContactEmail` 필드 추가 |
| `web/default/src/features/system-settings/operations/index.tsx` | 기본값에 `ContactEmail: ''` 추가 |
| `web/default/src/features/system-settings/integrations/email-settings-section.tsx` | `ContactEmail` 입력 필드 추가 |
| `web/default/src/features/system-settings/operations/section-registry.tsx` | `EmailSettingsSection` defaultValues에 `ContactEmail` 전달 |
| `web/default/public/index_en.html` | 문의 모달에 제출 버튼 추가 |

---

## Task 1: 백엔드 — `ContactEmail` 설정 추가

**Files:**
- Modify: `common/constants.go`
- Modify: `model/option.go`

- [ ] **Step 1: `common/constants.go`에 변수 추가**

기존 SMTP 변수들 바로 아래에 추가한다:
```go
var ContactEmail = ""
```

- [ ] **Step 2: `model/option.go` — `InitOptionMap()`에 기본값 추가**

기존 `common.OptionMap["SMTPToken"] = ""` 라인 아래에 추가:
```go
common.OptionMap["ContactEmail"] = ""
```

- [ ] **Step 3: `model/option.go` — `updateOptionMap()`에 케이스 추가**

기존 `case "SMTPToken": common.SMTPToken = value` 블록 아래에 추가:
```go
case "ContactEmail":
    common.ContactEmail = value
```

- [ ] **Step 4: 빌드 확인**
```bash
go build ./...
```
Expected: 오류 없음

- [ ] **Step 5: 커밋**
```bash
git add common/constants.go model/option.go
git commit -m "feat: add ContactEmail option"
```

---

## Task 2: 백엔드 — `POST /api/contact` 엔드포인트

**Files:**
- Create: `controller/contact.go`
- Modify: `router/api-router.go`

- [ ] **Step 1: `controller/contact.go` 생성**

```go
package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type contactRequest struct {
	Org     string `json:"org"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Message string `json:"message"`
}

func Contact(c *gin.Context) {
	var req contactRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "잘못된 요청입니다."})
		return
	}

	if req.Org == "" || req.Email == "" || req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "회사명, 이메일, 문의내용은 필수입니다."})
		return
	}

	if common.ContactEmail == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "문의 수신 이메일이 설정되지 않았습니다."})
		return
	}

	subject := fmt.Sprintf("[%s] 기업 도입 문의 — %s", common.SystemName, req.Org)
	body := fmt.Sprintf(`<html><body style="font-family:sans-serif;color:#222;">
<h2 style="color:#0EA5E9;">기업 도입 문의</h2>
<table style="border-collapse:collapse;width:100%%;max-width:560px;">
<tr><td style="padding:8px 12px;font-weight:700;width:120px;background:#f3f4f6;">회사/기관</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">이메일</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">전화번호</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">문의내용</td><td style="padding:8px 12px;white-space:pre-wrap;">%s</td></tr>
</table>
</body></html>`,
		req.Org, req.Email, req.Phone, req.Message,
	)

	if err := common.SendEmail(subject, common.ContactEmail, body); err != nil {
		common.SysError(fmt.Sprintf("contact email send failed: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"message": "이메일 전송에 실패했습니다. 잠시 후 다시 시도해 주세요."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
```

- [ ] **Step 2: `router/api-router.go` — 공개 라우트에 등록**

`apiRouter.GET("/about", controller.GetAbout)` 라인 아래에 추가:
```go
apiRouter.POST("/contact", middleware.CriticalRateLimit(), controller.Contact)
```

> `CriticalRateLimit`은 IP당 분당 3회로 제한되어 스팸 방지에 적합하다.

- [ ] **Step 3: 빌드 확인**
```bash
go build ./...
```
Expected: 오류 없음

- [ ] **Step 4: 동작 확인 — ContactEmail 미설정 시**
```bash
curl -s -X POST http://localhost:3000/api/contact \
  -H "Content-Type: application/json" \
  -d '{"org":"테스트","email":"a@b.com","message":"문의"}'
```
Expected: `{"message":"문의 수신 이메일이 설정되지 않았습니다."}` (503)

- [ ] **Step 5: 동작 확인 — 필드 누락 시**
```bash
curl -s -X POST http://localhost:3000/api/contact \
  -H "Content-Type: application/json" \
  -d '{"org":"테스트"}'
```
Expected: `{"message":"회사명, 이메일, 문의내용은 필수입니다."}` (400)

- [ ] **Step 6: 커밋**
```bash
git add controller/contact.go router/api-router.go
git commit -m "feat: add POST /api/contact endpoint"
```

---

## Task 3: 프론트엔드 — 어드민 설정에 ContactEmail 필드 추가

**Files:**
- Modify: `web/default/src/features/system-settings/types.ts`
- Modify: `web/default/src/features/system-settings/operations/index.tsx`
- Modify: `web/default/src/features/system-settings/integrations/email-settings-section.tsx`
- Modify: `web/default/src/features/system-settings/operations/section-registry.tsx`

- [ ] **Step 1: `types.ts` — `OperationsSettings` 타입에 필드 추가**

`SMTPForceAuthLogin: boolean` 아래에 추가:
```ts
ContactEmail: string
```

- [ ] **Step 2: `operations/index.tsx` — 기본값에 추가**

`SMTPForceAuthLogin: false,` 아래에 추가:
```ts
ContactEmail: '',
```

- [ ] **Step 3: `email-settings-section.tsx` — 스키마, 타입, onSubmit, 필드 UI 추가**

스키마 `z.object({...})` 안 `SMTPForceAuthLogin: z.boolean()` 아래에 추가:
```ts
ContactEmail: z.string().refine((value) => {
  const trimmed = value.trim()
  if (!trimmed) return true
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)
}, t('Enter a valid email or leave blank')),
```

`EmailSettingsSectionProps`의 `defaultValues: EmailFormValues`는 타입 추론으로 자동 반영된다.

`onSubmit`의 `sanitized` 객체에 추가:
```ts
ContactEmail: values.ContactEmail.trim(),
```

`onSubmit`의 `initial` 객체에 추가:
```ts
ContactEmail: defaultValues.ContactEmail.trim(),
```

`onSubmit`의 diff 비교 블록에 추가 (기존 `SMTPForceAuthLogin` 블록 패턴 참고):
```ts
if (sanitized.ContactEmail !== initial.ContactEmail) {
  updates.push({ key: 'ContactEmail', value: sanitized.ContactEmail })
}
```

마지막 `</SettingsForm>` 바로 위, `SMTPToken` `FormField` 아래에 UI 추가:
```tsx
<FormField
  control={form.control}
  name='ContactEmail'
  render={({ field }) => (
    <FormItem>
      <FormLabel>{t('Contact Recipient Email')}</FormLabel>
      <FormControl>
        <Input
          autoComplete='off'
          type='email'
          placeholder='contact@yourcompany.com'
          {...field}
          onChange={(event) => field.onChange(event.target.value)}
        />
      </FormControl>
      <FormDescription>
        {t('Inquiry emails from the landing page will be sent to this address')}
      </FormDescription>
      <FormMessage />
    </FormItem>
  )}
/>
```

- [ ] **Step 4: `section-registry.tsx` — `EmailSettingsSection` defaultValues에 추가**

```tsx
<EmailSettingsSection
  defaultValues={{
    SMTPServer: settings.SMTPServer,
    SMTPPort: settings.SMTPPort,
    SMTPAccount: settings.SMTPAccount,
    SMTPFrom: settings.SMTPFrom,
    SMTPToken: settings.SMTPToken,
    SMTPSSLEnabled: settings.SMTPSSLEnabled,
    SMTPForceAuthLogin: settings.SMTPForceAuthLogin,
    ContactEmail: settings.ContactEmail,   // 추가
  }}
/>
```

- [ ] **Step 5: 프론트엔드 빌드 확인**
```bash
cd web/default && bun run build 2>&1 | tail -5
```
Expected: 오류 없이 빌드 완료

- [ ] **Step 6: 커밋**
```bash
git add web/default/src/features/system-settings/types.ts \
  web/default/src/features/system-settings/operations/index.tsx \
  web/default/src/features/system-settings/integrations/email-settings-section.tsx \
  web/default/src/features/system-settings/operations/section-registry.tsx
git commit -m "feat: add ContactEmail field to admin email settings"
```

---

## Task 4: `index_en.html` 제출 버튼 추가 및 전체 배포

**Files:**
- Modify: `web/default/public/index_en.html`

- [ ] **Step 1: `index_en.html` 문의 모달에 제출 버튼 추가**

`index_en.html`의 `<div id="contactResult"...>` 아래 `</form>` 바로 위에 추가:

```html
<button type="submit" class="cf-submit">Submit →</button>
```

한국어 `index.html`의 `.cf-submit` CSS가 이미 두 파일에 모두 있으므로 별도 스타일 추가 불필요.

- [ ] **Step 2: Go 바이너리 재빌드 및 서비스 재시작**
```bash
go build -o new-api . && sudo systemctl restart new-api && sleep 3
systemctl status new-api --no-pager | grep Active
```
Expected: `Active: active (running)`

- [ ] **Step 3: 최종 동작 확인**

어드민 → 시스템 설정 → Operations → SMTP Email 섹션에서 `ContactEmail` 필드 확인 및 저장 후:
```bash
curl -s -X POST http://localhost:3000/api/contact \
  -H "Content-Type: application/json" \
  -d '{"org":"테스트 주식회사","email":"test@example.com","phone":"010-1234-5678","message":"도입 문의드립니다."}'
```
Expected: `{"message":"ok"}` (200) + 수신 이메일 확인

- [ ] **Step 4: 커밋**
```bash
git add web/default/public/index_en.html new-api
git commit -m "feat: wire contact form to email — add submit button, rebuild binary"
```
