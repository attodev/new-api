# Quota 금액 입력 개선 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 관리자와 조직 관리자가 quota를 내부 단위 대신 금액 기준으로 보고 입력할 수 있게 한다.

**Architecture:** 백엔드 API와 저장 구조는 유지한다. 프론트엔드에서 기존 `formatQuota`, `parseQuotaFromDollars`, `quotaUnitsToDollars`, currency display 유틸리티를 사용해 표시와 입력을 변환한다.

**Tech Stack:** React 19, TypeScript, Base UI, Tailwind CSS, i18next, Bun.

---

### Task 1: 조직 사용자 quota 입력 개선

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-users-table.tsx`

- [ ] **Step 1: quota 포맷 유틸리티 import**

`formatQuota`, `parseQuotaFromDollars`, `quotaUnitsToDollars`를 import한다. currency 표시가 tokens 전용인지 판단하려면 `getCurrencyDisplay`, `getCurrencyLabel`도 import한다.

- [ ] **Step 2: 조직 quota 저장 헬퍼 추가**

금액 입력 문자열을 숫자로 파싱하고 `parseQuotaFromDollars`로 내부 quota 단위로 변환한 뒤 기존 `saveQuota`를 호출한다.

- [ ] **Step 3: quota 셀 UI 교체**

기존 raw quota `Input`을 현재 잔액 표시, 금액 입력, 빠른 버튼 `$1`, `$5`, `$10`, `$100` 조합으로 바꾼다. tokens-only 모드에서는 빠른 버튼을 숨긴다.

- [ ] **Step 4: 저장 동작 확인**

`onBlur`와 Enter 입력 시 목표 잔액이 저장되어야 한다. 빠른 버튼은 해당 금액을 즉시 absolute quota로 저장해야 한다.

### Task 2: 관리자 quota 다이얼로그 빠른 버튼 추가

**Files:**
- Modify: `web/default/src/features/users/components/user-quota-dialog.tsx`

- [ ] **Step 1: 빠른 금액 목록 추가**

tokens-only 모드가 아닐 때 `$1`, `$5`, `$10`, `$100` 버튼을 렌더링한다.

- [ ] **Step 2: 버튼 클릭 시 입력값 갱신**

버튼 클릭은 현재 모드를 바꾸지 않고 `amount`만 선택 금액으로 설정한다. 기존 미리보기와 확인 로직은 그대로 사용한다.

### Task 3: i18n 및 검증

**Files:**
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/vi.json`

- [ ] **Step 1: 새 UI 문구 번역 추가**

`Quick amount`, `Set quota to`, `Quota amount`, `Current quota balance` 같은 키를 각 locale 파일에 추가한다. 한국어 문서는 별도 파일에 있고, 앱 locale은 기존 지원 언어만 유지한다.

- [ ] **Step 2: 포맷 실행**

Run: `bunx prettier --write web/default/src/features/organizations/components/organization-users-table.tsx web/default/src/features/users/components/user-quota-dialog.tsx web/default/src/i18n/locales/*.json`

- [ ] **Step 3: 빌드 검증**

Run: `bun run build` from `web/default/`
Expected: build succeeds.
