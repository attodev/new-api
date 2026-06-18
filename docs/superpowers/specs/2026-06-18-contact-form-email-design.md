# Contact Form Email — Design Spec

**Date:** 2026-06-18  
**Scope:** Wire the existing landing page contact modal to send an inquiry email via the existing SMTP system.

---

## Background

`index.html` and `index_en.html` already contain a contact modal with fields `org`, `email`, `phone`, `message`. The form already calls `POST /api/contact` but the endpoint does not exist. The project has a working SMTP email system (`common.SendEmail`) and an option-based settings store (`model/option.go`).

---

## What Gets Built

### 1. New setting: `ContactEmail`

The email address that receives submitted inquiries.

- `common/constants.go`: add `var ContactEmail = ""`
- `model/option.go`: add `"ContactEmail": ""` to the default `OptionMap`, and a `case "ContactEmail": common.ContactEmail = value` branch in `UpdateOption()`

### 2. `POST /api/contact` endpoint

**File:** `controller/contact.go` (new)

**Auth:** None — public endpoint, called from the landing page before login.

**Request body (JSON):**
```json
{ "org": "...", "email": "...", "phone": "...", "message": "..." }
```

**Validation:**
- `org`, `email`, `message` are required; return 400 with a message if missing.
- If `ContactEmail` is not configured, return 503 `{"message": "Contact email not configured"}`.

**On success:**
- Build a simple HTML email body containing all four fields.
- Call `common.SendEmail(subject, common.ContactEmail, htmlBody)`.
- Subject: `[AlRouter] 기업 도입 문의 — <org>` (Korean) / adjust subject text via `SystemName` global.
- Return 200 `{"message": "ok"}`.

**On SMTP error:** return 500 with a generic error message; log the underlying error.

### 3. Router registration

**File:** `router/api-router.go`

Add to the existing public (no-auth) route group:
```go
r.POST("/api/contact", controller.Contact)
```

`GlobalWebRateLimit` middleware already applies here, providing baseline spam protection.

### 4. Admin UI — contact email field

**Location:** the existing SMTP settings section in the system settings panel.

**File to modify:** whichever React component renders the SMTP settings fields (to be identified during implementation).

Add a single text input for `ContactEmail` alongside the existing SMTP fields, following the same save pattern as other option fields.

---

## Data Flow

```
Browser (landing page modal)
  → POST /api/contact  { org, email, phone, message }
  → controller.Contact()
      validates fields
      reads common.ContactEmail
      builds HTML body
      calls common.SendEmail(subject, contactEmail, body)
          → SMTP server (existing config)
              → recipient inbox
  ← 200 { message: "ok" }
```

---

## Out of Scope

- CAPTCHA / bot protection (GlobalWebRateLimit covers basic abuse)
- Storing inquiries in the database
- Email templates / i18n of the email body
- index_en.html has no submit button in the modal — this will be added during implementation

---

## Files Changed

| File | Change |
|------|--------|
| `common/constants.go` | Add `var ContactEmail = ""` |
| `model/option.go` | Default + UpdateOption case for `ContactEmail` |
| `controller/contact.go` | New file — Contact handler |
| `router/api-router.go` | Register `POST /api/contact` |
| Frontend settings component | Add ContactEmail input field |
| `web/default/public/index_en.html` | Add missing submit button to contact modal |
