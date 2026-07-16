# 조직 대시보드 차트 구현 계획

> **작업 에이전트용:** 이 계획을 단계별로 실행할 때는 `superpowers:subagent-driven-development` 또는 `superpowers:executing-plans`를 사용한다. 단계 추적은 체크박스(`- [ ]`) 형식을 사용한다.

**Goal:** 조직 대시보드에 관리자 대시보드 수준의 사용량 분포 및 모델 분석 차트를 추가한다.

**Architecture:** 백엔드는 조직 범위 `QuotaData`를 날짜/모델별로 집계해 `model_usage`를 반환한다. 프론트는 이 응답을 관리자 대시보드의 `processChartData` 입력 형태로 변환하고, `VChart` 기반 차트 컴포넌트를 조직 대시보드 전용으로 렌더링한다.

**Tech Stack:** Go, GORM, React 19, TypeScript, Bun, `@visactor/react-vchart`

---

### Task 1: 백엔드 조직 모델별 일자 집계 추가

**Files:**
- Modify: `model/organization_dashboard.go`
- Modify: `model/organization_test.go`

- [ ] `OrganizationDashboardModelDailyUsage` 타입을 추가한다.
- [ ] `OrganizationDashboard`에 `ModelUsage []OrganizationDashboardModelDailyUsage` 필드를 추가한다.
- [ ] `GetOrganizationDashboard` 루프에서 날짜와 모델을 키로 모델별 일자 집계를 누적한다.
- [ ] 기존 테스트에 `dashboard.ModelUsage` 기대값을 추가한다.
- [ ] `go test ./model -run TestGetOrganizationDashboardScopesQuotaDataToOrganization -count=1`을 실행한다.

### Task 2: 프론트 타입과 차트 데이터 변환 추가

**Files:**
- Modify: `web/default/src/features/organizations/types.ts`
- Modify: `web/default/src/features/organizations/components/organization-dashboard.tsx`

- [ ] `OrganizationDashboardModelDailyUsage` 타입을 추가하고 `OrganizationDashboardData.model_usage`에 연결한다.
- [ ] `model_usage`를 `QuotaDataItem[]`으로 변환하는 헬퍼를 만든다.
- [ ] 날짜 문자열은 로컬 자정 기준 초 단위 타임스탬프로 변환한다.
- [ ] 관리자 대시보드의 `processChartData`를 재사용한다.

### Task 3: 조직 대시보드 차트 UI 교체

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-dashboard.tsx`

- [ ] 기존 `recharts` 기반 `UsageTrendChart`를 제거한다.
- [ ] `OrganizationConsumptionChart`를 추가해 `막대 / 영역` 차트를 제공한다.
- [ ] `OrganizationModelAnalyticsChart`를 추가해 `추세 / 비율 / Top` 탭을 제공한다.
- [ ] 빈 데이터 상태와 테마 전환 상태를 처리한다.
- [ ] 기존 Top 사용자/모델 표는 유지한다.

### Task 4: 검증

**Files:**
- Verify only

- [ ] `go test ./model -run TestGetOrganizationDashboardScopesQuotaDataToOrganization -count=1`
- [ ] `cd web/default && bun run build`
- [ ] `git diff --check`
