# 조직 과금 책임자 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 조직 구성원의 API 사용 비용을 구성원 한도와 조직 소유자 지갑에 함께 반영한다.

**Architecture:** model 계층은 조직 owner 조회만 담당한다. service 계층은 기존 BillingSession 생명주기를 유지하면서 조직 구성원용 WalletFunding을 추가한다.

**Tech Stack:** Go 1.22, Gin, GORM, SQLite/MySQL/PostgreSQL 호환 쿼리.

---

### Task 1: RED 테스트 작성

**Files:**
- Create: `service/organization_billing_test.go`

- [ ] **Step 1: 조직 구성원 pre-consume 테스트 작성**

SQLite in-memory DB에 owner, member, organization을 만들고 `NewBillingSession`을 호출한다. 기대값은 member quota와 owner quota가 모두 선차감되는 것이다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./service -run TestOrganizationWalletBilling -count=1`
Expected: FAIL. 아직 조직 owner 지갑 과금이 구현되지 않았으므로 owner quota가 줄지 않는다.

### Task 2: model owner 조회 헬퍼

**Files:**
- Modify: `model/organization.go`

- [ ] **Step 1: owner 조회 함수 추가**

`GetOrganizationOwnerUserId(organizationId int) (int, error)`를 추가한다. organization_id가 0이면 0을 반환하고, DB 조회 실패는 그대로 반환한다.

### Task 3: 조직 wallet funding 추가

**Files:**
- Modify: `service/funding_source.go`
- Modify: `service/billing_session.go`

- [ ] **Step 1: OrganizationWalletFunding 추가**

구성원 quota와 owner quota를 함께 차감/환불하는 funding source를 추가한다. owner와 member가 같으면 기존 WalletFunding을 사용한다.

- [ ] **Step 2: NewBillingSession wallet 경로 확장**

조직 구성원이면 member quota와 owner quota를 모두 검사한다. quota가 부족하면 기존 insufficient quota 계열 오류를 반환한다.

### Task 4: GREEN 검증

**Files:**
- Test: `service/organization_billing_test.go`

- [ ] **Step 1: 서비스 테스트 통과 확인**

Run: `go test ./service -run TestOrganizationWalletBilling -count=1`
Expected: PASS.

- [ ] **Step 2: 관련 조직 테스트 확인**

Run: `go test ./model ./controller ./service -run 'TestOrganization|TestOrganizationWalletBilling' -count=1`
Expected: PASS.
