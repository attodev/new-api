# Organization Usage Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 조직 소유자와 조직 관리자가 조직 전체 사용량, 기간별 추이, 상위 사용자, 상위 모델을 확인할 수 있는 조직 사용량 대시보드를 만든다.

**Architecture:** 백엔드는 `/api/organization/dashboard` 조직 전용 API를 추가하고, 현재 로그인 사용자의 조직과 역할을 기준으로 권한을 제한한다. 집계 데이터는 `quota_data`와 `users`, `organizations`를 사용하며, 프론트엔드는 `/organization/dashboard` 화면에서 기간 프리셋과 차트/표를 렌더링한다.

**Tech Stack:** Go 1.22+, Gin, GORM, SQLite/MySQL/PostgreSQL 호환 쿼리, React 19, TypeScript, TanStack Router, TanStack Query, Recharts, Bun.

---

## File Structure

- Create: `model/organization_dashboard.go`
  - 조직 대시보드 응답 DTO와 `GetOrganizationDashboard` 집계 함수를 담당한다.
- Modify: `model/organization_test.go`
  - 조직 대시보드 집계 모델 테스트를 추가한다.
- Modify: `controller/organization.go`
  - `GetOrganizationDashboard` 컨트롤러를 추가한다.
- Modify: `controller/organization_test.go`
  - owner/admin/member/no-org 권한 테스트와 응답 집계 테스트를 추가한다.
- Modify: `router/api-router.go`
  - `GET /api/organization/dashboard` 라우트를 추가한다.
- Modify: `web/default/src/features/organizations/types.ts`
  - 조직 대시보드 API 응답 타입을 추가한다.
- Modify: `web/default/src/features/organizations/api.ts`
  - `getOrganizationDashboard` API 클라이언트를 추가한다.
- Create: `web/default/src/features/organizations/components/organization-dashboard.tsx`
  - 조직 사용량 대시보드 화면을 담당한다.
- Create: `web/default/src/routes/_authenticated/organization/dashboard.tsx`
  - `/organization/dashboard` 라우트를 추가한다.
- Modify: `web/default/src/features/organizations/components/organization-users-table.tsx`
  - 조직 관리자 화면에서 대시보드 진입 버튼을 추가한다.
- Modify: `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi,kr}.json`
  - 새 UI 문구 번역을 추가한다.
- Generated/Modify: `web/default/src/routeTree.gen.ts`
  - 프론트 빌드 또는 라우트 생성 과정에서 갱신된다.

---

### Task 1: Add Backend Model Aggregation

**Files:**
- Create: `model/organization_dashboard.go`
- Modify: `model/organization_test.go`

- [ ] **Step 1: Write the failing model test**

Add this test to `model/organization_test.go`:

```go
func TestGetOrganizationDashboardScopesQuotaDataToOrganization(t *testing.T) {
	setupOrganizationModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&QuotaData{}))

	org := Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 500000, UsedQuota: 1200, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(&org).Error)

	owner := User{Id: 1, Username: "owner", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleOwner, Quota: 1000, UsedQuota: 100, RequestCount: 2, AffCode: "owner"}
	member := User{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember, Quota: 2000, UsedQuota: 700, RequestCount: 4, AffCode: "member"}
	outside := User{Id: 3, Username: "outside", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: OrganizationRoleMember, Quota: 3000, UsedQuota: 900, RequestCount: 8, AffCode: "outside"}
	require.NoError(t, DB.Create(&owner).Error)
	require.NoError(t, DB.Create(&member).Error)
	require.NoError(t, DB.Create(&outside).Error)

	start := int64(1717200000)
	require.NoError(t, DB.Create(&QuotaData{UserID: 1, Username: "owner", ModelName: "gpt-a", CreatedAt: start, Count: 1, Quota: 100, TokenUsed: 10}).Error)
	require.NoError(t, DB.Create(&QuotaData{UserID: 2, Username: "member", ModelName: "gpt-b", CreatedAt: start + 3600, Count: 3, Quota: 300, TokenUsed: 30}).Error)
	require.NoError(t, DB.Create(&QuotaData{UserID: 3, Username: "outside", ModelName: "gpt-c", CreatedAt: start + 7200, Count: 9, Quota: 900, TokenUsed: 90}).Error)

	dashboard, err := GetOrganizationDashboard(1, start, start+86400)

	require.NoError(t, err)
	require.Equal(t, 1, dashboard.Organization.Id)
	require.Equal(t, 500000, dashboard.Organization.Quota)
	require.Equal(t, 1200, dashboard.Organization.UsedQuota)
	require.Equal(t, 400, dashboard.Summary.PeriodQuota)
	require.Equal(t, 4, dashboard.Summary.PeriodRequests)
	require.Equal(t, 2, dashboard.Summary.MemberCount)
	require.Equal(t, 2, dashboard.Summary.ActiveMemberCount)
	require.Len(t, dashboard.TopUsers, 2)
	require.Equal(t, "member", dashboard.TopUsers[0].Username)
	require.Equal(t, 300, dashboard.TopUsers[0].PeriodQuota)
	require.Len(t, dashboard.TopModels, 2)
	require.Equal(t, "gpt-b", dashboard.TopModels[0].Model)
	require.Equal(t, 300, dashboard.TopModels[0].Quota)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./model -run TestGetOrganizationDashboardScopesQuotaDataToOrganization -count=1
```

Expected: FAIL because `GetOrganizationDashboard` and related DTOs are not defined.

- [ ] **Step 3: Add minimal aggregation implementation**

Create `model/organization_dashboard.go`:

```go
package model

import (
	"sort"
	"time"
)

type OrganizationDashboardOrganization struct {
	Id        int    `json:"id"`
	Name      string `json:"name"`
	Quota     int    `json:"quota"`
	UsedQuota int    `json:"used_quota"`
}

type OrganizationDashboardSummary struct {
	PeriodQuota       int `json:"period_quota"`
	PeriodRequests    int `json:"period_requests"`
	MemberCount       int `json:"member_count"`
	ActiveMemberCount int `json:"active_member_count"`
}

type OrganizationDashboardDailyUsage struct {
	Date     string `json:"date"`
	Quota    int    `json:"quota"`
	Requests int    `json:"requests"`
}

type OrganizationDashboardTopUser struct {
	UserId         int    `json:"user_id"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	Quota          int    `json:"quota"`
	UsedQuota      int    `json:"used_quota"`
	PeriodQuota    int    `json:"period_quota"`
	PeriodRequests int    `json:"period_requests"`
}

type OrganizationDashboardTopModel struct {
	Model    string `json:"model"`
	Quota    int    `json:"quota"`
	Requests int    `json:"requests"`
}

type OrganizationDashboard struct {
	Organization OrganizationDashboardOrganization `json:"organization"`
	Summary      OrganizationDashboardSummary      `json:"summary"`
	DailyUsage   []OrganizationDashboardDailyUsage `json:"daily_usage"`
	TopUsers     []OrganizationDashboardTopUser    `json:"top_users"`
	TopModels    []OrganizationDashboardTopModel   `json:"top_models"`
}

func normalizeDashboardRange(startTime int64, endTime int64) (int64, int64) {
	now := time.Now().Unix()
	if endTime <= 0 {
		endTime = now
	}
	if startTime <= 0 || startTime > endTime {
		startTime = endTime - 7*24*3600
	}
	return startTime, endTime
}

func GetOrganizationDashboard(organizationId int, startTime int64, endTime int64) (*OrganizationDashboard, error) {
	startTime, endTime = normalizeDashboardRange(startTime, endTime)

	var org Organization
	if err := DB.First(&org, organizationId).Error; err != nil {
		return nil, err
	}

	var users []User
	if err := DB.Where("organization_id = ?", organizationId).Find(&users).Error; err != nil {
		return nil, err
	}

	dashboard := &OrganizationDashboard{
		Organization: OrganizationDashboardOrganization{
			Id:        org.Id,
			Name:      org.Name,
			Quota:     org.Quota,
			UsedQuota: org.UsedQuota,
		},
		Summary: OrganizationDashboardSummary{MemberCount: len(users)},
		DailyUsage: []OrganizationDashboardDailyUsage{},
		TopUsers:   []OrganizationDashboardTopUser{},
		TopModels:  []OrganizationDashboardTopModel{},
	}
	if len(users) == 0 {
		return dashboard, nil
	}

	userById := make(map[int]User, len(users))
	userIds := make([]int, 0, len(users))
	for _, user := range users {
		userById[user.Id] = user
		userIds = append(userIds, user.Id)
	}

	var rows []QuotaData
	if err := DB.Where("user_id IN ? AND created_at >= ? AND created_at <= ?", userIds, startTime, endTime).Find(&rows).Error; err != nil {
		return nil, err
	}

	daily := map[string]*OrganizationDashboardDailyUsage{}
	topUsers := map[int]*OrganizationDashboardTopUser{}
	topModels := map[string]*OrganizationDashboardTopModel{}
	activeUsers := map[int]bool{}

	for _, row := range rows {
		dashboard.Summary.PeriodQuota += row.Quota
		dashboard.Summary.PeriodRequests += row.Count
		if row.Count > 0 || row.Quota > 0 {
			activeUsers[row.UserID] = true
		}

		date := time.Unix(row.CreatedAt, 0).Format("2006-01-02")
		if daily[date] == nil {
			daily[date] = &OrganizationDashboardDailyUsage{Date: date}
		}
		daily[date].Quota += row.Quota
		daily[date].Requests += row.Count

		user := userById[row.UserID]
		if topUsers[row.UserID] == nil {
			topUsers[row.UserID] = &OrganizationDashboardTopUser{
				UserId:      user.Id,
				Username:    user.Username,
				DisplayName: user.Username,
				Quota:       user.Quota,
				UsedQuota:   user.UsedQuota,
			}
		}
		topUsers[row.UserID].PeriodQuota += row.Quota
		topUsers[row.UserID].PeriodRequests += row.Count

		modelName := row.ModelName
		if modelName == "" {
			modelName = "(unknown)"
		}
		if topModels[modelName] == nil {
			topModels[modelName] = &OrganizationDashboardTopModel{Model: modelName}
		}
		topModels[modelName].Quota += row.Quota
		topModels[modelName].Requests += row.Count
	}
	dashboard.Summary.ActiveMemberCount = len(activeUsers)

	for _, item := range daily {
		dashboard.DailyUsage = append(dashboard.DailyUsage, *item)
	}
	sort.Slice(dashboard.DailyUsage, func(i, j int) bool {
		return dashboard.DailyUsage[i].Date < dashboard.DailyUsage[j].Date
	})

	for _, item := range topUsers {
		dashboard.TopUsers = append(dashboard.TopUsers, *item)
	}
	sort.Slice(dashboard.TopUsers, func(i, j int) bool {
		if dashboard.TopUsers[i].PeriodQuota == dashboard.TopUsers[j].PeriodQuota {
			return dashboard.TopUsers[i].Username < dashboard.TopUsers[j].Username
		}
		return dashboard.TopUsers[i].PeriodQuota > dashboard.TopUsers[j].PeriodQuota
	})

	for _, item := range topModels {
		dashboard.TopModels = append(dashboard.TopModels, *item)
	}
	sort.Slice(dashboard.TopModels, func(i, j int) bool {
		if dashboard.TopModels[i].Quota == dashboard.TopModels[j].Quota {
			return dashboard.TopModels[i].Model < dashboard.TopModels[j].Model
		}
		return dashboard.TopModels[i].Quota > dashboard.TopModels[j].Quota
	})

	return dashboard, nil
}
```

- [ ] **Step 4: Run the model test to verify it passes**

Run:

```bash
go test ./model -run TestGetOrganizationDashboardScopesQuotaDataToOrganization -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add model/organization_dashboard.go model/organization_test.go
git commit -m "feat: add organization dashboard aggregation"
```

---

### Task 2: Add Backend Controller and Route

**Files:**
- Modify: `controller/organization.go`
- Modify: `controller/organization_test.go`
- Modify: `router/api-router.go`

- [ ] **Step 1: Write failing controller tests**

Add these tests to `controller/organization_test.go`:

```go
func TestOrganizationOwnerCanReadDashboard(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.QuotaData{}))

	owner := model.User{Id: 1, Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 500000, UsedQuota: 100, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{UserID: 1, Username: "owner", ModelName: "gpt-a", CreatedAt: 1717200000, Count: 2, Quota: 250}).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		owner,
		http.MethodGet,
		"/api/organization/dashboard?start_timestamp=1717200000&end_timestamp=1717286400",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"period_quota":250`)
	require.Contains(t, res.Body.String(), `"period_requests":2`)
}

func TestOrganizationMemberCannotReadDashboard(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	member := model.User{Id: 1, Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		member,
		http.MethodGet,
		"/api/organization/dashboard",
		"",
	)

	require.NotEqual(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), "organization admin permission required")
}
```

- [ ] **Step 2: Run controller tests to verify failure**

Run:

```bash
go test ./controller -run 'TestOrganizationOwnerCanReadDashboard|TestOrganizationMemberCannotReadDashboard' -count=1
```

Expected: FAIL because `GetOrganizationDashboard` controller is not defined.

- [ ] **Step 3: Add controller implementation**

In `controller/organization.go`, add:

```go
func GetOrganizationDashboard(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiError(c, errors.New("organization admin permission required"))
		return
	}

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if startTimestamp > 0 && endTimestamp > 0 && startTimestamp > endTimestamp {
		common.ApiError(c, errors.New("start_timestamp must be before end_timestamp"))
		return
	}

	dashboard, err := model.GetOrganizationDashboard(actor.OrganizationId, startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, dashboard)
}
```

- [ ] **Step 4: Add route**

In `router/api-router.go`, inside `organizationRoute := apiRouter.Group("/organization")`, add:

```go
organizationRoute.GET("/dashboard", controller.GetOrganizationDashboard)
```

Place it near `organizationRoute.GET("/", controller.GetOrganizationProfile)`.

- [ ] **Step 5: Run controller tests**

Run:

```bash
go test ./controller -run 'TestOrganizationOwnerCanReadDashboard|TestOrganizationMemberCannotReadDashboard' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run scoped backend regression tests**

Run:

```bash
go test ./model ./controller -run 'TestGetOrganizationDashboardScopesQuotaDataToOrganization|TestOrganization' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add controller/organization.go controller/organization_test.go router/api-router.go
git commit -m "feat: expose organization usage dashboard api"
```

---

### Task 3: Add Frontend API Types and Client

**Files:**
- Modify: `web/default/src/features/organizations/types.ts`
- Modify: `web/default/src/features/organizations/api.ts`

- [ ] **Step 1: Add dashboard types**

In `web/default/src/features/organizations/types.ts`, add:

```ts
export type OrganizationDashboardRangePreset = 'today' | '7d' | '30d' | 'custom'

export interface OrganizationDashboardOrganization {
  id: number
  name: string
  quota: number
  used_quota: number
}

export interface OrganizationDashboardSummary {
  period_quota: number
  period_requests: number
  member_count: number
  active_member_count: number
}

export interface OrganizationDashboardDailyUsage {
  date: string
  quota: number
  requests: number
}

export interface OrganizationDashboardTopUser {
  user_id: number
  username: string
  display_name: string
  quota: number
  used_quota: number
  period_quota: number
  period_requests: number
}

export interface OrganizationDashboardTopModel {
  model: string
  quota: number
  requests: number
}

export interface OrganizationDashboardData {
  organization: OrganizationDashboardOrganization
  summary: OrganizationDashboardSummary
  daily_usage: OrganizationDashboardDailyUsage[]
  top_users: OrganizationDashboardTopUser[]
  top_models: OrganizationDashboardTopModel[]
}

export interface OrganizationDashboardParams {
  start_timestamp?: number
  end_timestamp?: number
  preset?: OrganizationDashboardRangePreset
}
```

- [ ] **Step 2: Add API client**

In `web/default/src/features/organizations/api.ts`, import the new types and add:

```ts
export async function getOrganizationDashboard(
  params: OrganizationDashboardParams
): Promise<ApiResponse<OrganizationDashboardData>> {
  const search = new URLSearchParams()
  if (params.start_timestamp) {
    search.set('start_timestamp', String(params.start_timestamp))
  }
  if (params.end_timestamp) {
    search.set('end_timestamp', String(params.end_timestamp))
  }
  if (params.preset) {
    search.set('preset', params.preset)
  }
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/dashboard${suffix}`)
  return res.data
}
```

Update the type import block so it includes:

```ts
OrganizationDashboardData,
OrganizationDashboardParams,
```

- [ ] **Step 3: Run frontend type/build check**

Run:

```bash
cd web/default
bun run build
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/organizations/types.ts web/default/src/features/organizations/api.ts
git commit -m "feat: add organization dashboard frontend api"
```

---

### Task 4: Build Organization Dashboard UI

**Files:**
- Create: `web/default/src/features/organizations/components/organization-dashboard.tsx`
- Create: `web/default/src/routes/_authenticated/organization/dashboard.tsx`

- [ ] **Step 1: Create route shell**

Create `web/default/src/routes/_authenticated/organization/dashboard.tsx`:

```tsx
import { createFileRoute } from '@tanstack/react-router'
import { hasOrganizationAdminRole } from '@/lib/organization-roles'
import { useAuthStore } from '@/stores/auth'
import { OrganizationDashboard } from '@/features/organizations/components/organization-dashboard'

export const Route = createFileRoute('/_authenticated/organization/dashboard')({
  beforeLoad: () => {
    const auth = useAuthStore.getState()
    if (!auth.user || !hasOrganizationAdminRole(auth.user.organization_role)) {
      throw new Error('organization admin permission required')
    }
  },
  component: OrganizationDashboard,
})
```

- [ ] **Step 2: Create dashboard component**

Create `web/default/src/features/organizations/components/organization-dashboard.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Activity, BarChart3, CalendarDays, Users, WalletCards } from 'lucide-react'
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { getOrganizationDashboard } from '@/features/organizations/api'
import type { OrganizationDashboardRangePreset } from '@/features/organizations/types'
import { formatQuota } from '@/lib/format'

function getRange(preset: OrganizationDashboardRangePreset) {
  const now = Math.floor(Date.now() / 1000)
  const day = 24 * 60 * 60
  if (preset === 'today') {
    const start = new Date()
    start.setHours(0, 0, 0, 0)
    return { start_timestamp: Math.floor(start.getTime() / 1000), end_timestamp: now, preset }
  }
  if (preset === '30d') {
    return { start_timestamp: now - 30 * day, end_timestamp: now, preset }
  }
  return { start_timestamp: now - 7 * day, end_timestamp: now, preset: '7d' as const }
}

export function OrganizationDashboard() {
  const { t } = useTranslation()
  const [preset, setPreset] = useState<OrganizationDashboardRangePreset>('7d')
  const range = useMemo(() => getRange(preset), [preset])

  const query = useQuery({
    queryKey: ['organization-dashboard', range],
    queryFn: async () => {
      const response = await getOrganizationDashboard(range)
      return response.data
    },
  })

  const data = query.data
  const summaryCards = [
    {
      label: t('Organization balance'),
      value: formatQuota(data?.organization.quota ?? 0),
      icon: WalletCards,
    },
    {
      label: t('Organization used quota'),
      value: formatQuota(data?.organization.used_quota ?? 0),
      icon: BarChart3,
    },
    {
      label: t('Period usage'),
      value: formatQuota(data?.summary.period_quota ?? 0),
      icon: CalendarDays,
    },
    {
      label: t('Period requests'),
      value: (data?.summary.period_requests ?? 0).toLocaleString(),
      icon: Activity,
    },
  ]

  return (
    <div className='flex flex-col gap-4 p-4 md:p-6'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div>
          <h1 className='text-2xl font-semibold tracking-normal'>{t('Organization dashboard')}</h1>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Monitor organization usage by period, user, and model.')}
          </p>
        </div>
        <div className='flex items-center gap-2'>
          <Button variant='outline' render={<Link to='/organization' />}>
            <Users className='size-4' />
            {t('Users')}
          </Button>
          <Select value={preset} onValueChange={(value) => setPreset(value as OrganizationDashboardRangePreset)}>
            <SelectTrigger className='w-36'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='today'>{t('Today')}</SelectItem>
              <SelectItem value='7d'>{t('Last 7 days')}</SelectItem>
              <SelectItem value='30d'>{t('Last 30 days')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className='grid gap-3 md:grid-cols-4'>
        {summaryCards.map((card) => (
          <Card key={card.label}>
            <CardContent className='p-4'>
              <div className='flex items-center gap-2 text-sm text-muted-foreground'>
                <card.icon className='size-4' />
                {card.label}
              </div>
              <div className='mt-2 font-mono text-xl font-semibold'>
                {query.isLoading ? <Skeleton className='h-7 w-24' /> : card.value}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('Usage trend')}</CardTitle>
        </CardHeader>
        <CardContent className='h-72'>
          {query.isLoading ? (
            <Skeleton className='h-full w-full' />
          ) : (
            <ResponsiveContainer width='100%' height='100%'>
              <AreaChart data={data?.daily_usage ?? []}>
                <CartesianGrid strokeDasharray='3 3' />
                <XAxis dataKey='date' />
                <YAxis />
                <Tooltip formatter={(value) => formatQuota(Number(value) || 0)} />
                <Area type='monotone' dataKey='quota' stroke='hsl(var(--primary))' fill='hsl(var(--primary))' fillOpacity={0.18} />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>

      <div className='grid gap-4 lg:grid-cols-2'>
        <Card>
          <CardHeader>
            <CardTitle>{t('Top users')}</CardTitle>
          </CardHeader>
          <CardContent className='space-y-3'>
            {(data?.top_users ?? []).map((user) => (
              <div key={user.user_id} className='flex items-center justify-between gap-3 border-b pb-3 last:border-0 last:pb-0'>
                <div className='min-w-0'>
                  <div className='truncate text-sm font-medium'>{user.display_name || user.username}</div>
                  <div className='text-muted-foreground text-xs'>{user.period_requests.toLocaleString()} {t('requests')}</div>
                </div>
                <div className='font-mono text-sm font-semibold'>{formatQuota(user.period_quota)}</div>
              </div>
            ))}
            {!query.isLoading && (data?.top_users ?? []).length === 0 && (
              <div className='text-muted-foreground text-sm'>{t('No usage data')}</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t('Top models')}</CardTitle>
          </CardHeader>
          <CardContent className='space-y-3'>
            {(data?.top_models ?? []).map((model) => (
              <div key={model.model} className='flex items-center justify-between gap-3 border-b pb-3 last:border-0 last:pb-0'>
                <div className='min-w-0'>
                  <div className='truncate text-sm font-medium'>{model.model}</div>
                  <div className='text-muted-foreground text-xs'>{model.requests.toLocaleString()} {t('requests')}</div>
                </div>
                <div className='font-mono text-sm font-semibold'>{formatQuota(model.quota)}</div>
              </div>
            ))}
            {!query.isLoading && (data?.top_models ?? []).length === 0 && (
              <div className='text-muted-foreground text-sm'>{t('No usage data')}</div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Run frontend build**

Run:

```bash
cd web/default
bun run build
```

Expected: PASS and `routeTree.gen.ts` is updated.

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/organizations/components/organization-dashboard.tsx web/default/src/routes/_authenticated/organization/dashboard.tsx web/default/src/routeTree.gen.ts
git commit -m "feat: add organization usage dashboard page"
```

---

### Task 5: Add Navigation Entry and Translations

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-users-table.tsx`
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/vi.json`
- Modify: `web/default/src/i18n/locales/kr.json`

- [ ] **Step 1: Add dashboard entry button**

In `organization-users-table.tsx`, near the existing organization wallet or quota management actions, add a button:

```tsx
<Button variant='outline' render={<Link to='/organization/dashboard' />}>
  <BarChart3 className='size-4' />
  {t('Usage dashboard')}
</Button>
```

Update lucide imports to include `BarChart3` if it is not already imported.

- [ ] **Step 2: Add English and Korean translations**

Add these keys to `web/default/src/i18n/locales/en.json`:

```json
"Organization dashboard": "Organization dashboard",
"Monitor organization usage by period, user, and model.": "Monitor organization usage by period, user, and model.",
"Organization balance": "Organization balance",
"Period usage": "Period usage",
"Period requests": "Period requests",
"Usage trend": "Usage trend",
"Top users": "Top users",
"Top models": "Top models",
"No usage data": "No usage data",
"Usage dashboard": "Usage dashboard",
"Last 7 days": "Last 7 days",
"Last 30 days": "Last 30 days",
"requests": "requests"
```

Add Korean equivalents to `web/default/src/i18n/locales/kr.json`:

```json
"Organization dashboard": "조직 대시보드",
"Monitor organization usage by period, user, and model.": "기간, 사용자, 모델별 조직 사용량을 모니터링합니다.",
"Organization balance": "조직 잔여 쿼터",
"Period usage": "기간 내 사용량",
"Period requests": "기간 내 요청 수",
"Usage trend": "사용량 추이",
"Top users": "상위 사용자",
"Top models": "상위 모델",
"No usage data": "사용량 데이터가 없습니다",
"Usage dashboard": "사용량 대시보드",
"Last 7 days": "최근 7일",
"Last 30 days": "최근 30일",
"requests": "요청"
```

For `zh`, `fr`, `ja`, `ru`, `vi`, add the same English text if a precise translation is not already available. Then run the i18n sync command to normalize keys.

- [ ] **Step 3: Run i18n sync**

Run:

```bash
cd web/default
bun run i18n:sync
```

Expected: command completes without removing the added keys.

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd web/default
bun run build
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/default/src/features/organizations/components/organization-users-table.tsx web/default/src/i18n/locales/en.json web/default/src/i18n/locales/zh.json web/default/src/i18n/locales/fr.json web/default/src/i18n/locales/ja.json web/default/src/i18n/locales/ru.json web/default/src/i18n/locales/vi.json web/default/src/i18n/locales/kr.json
git commit -m "feat: add organization dashboard navigation"
```

---

### Task 6: Final Verification

**Files:**
- Verify all files changed in Tasks 1-5.

- [ ] **Step 1: Run backend focused tests**

Run:

```bash
go test ./model ./controller -run 'TestGetOrganizationDashboardScopesQuotaDataToOrganization|TestOrganization' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader backend smoke tests**

Run:

```bash
go test ./model ./controller -count=1
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run:

```bash
cd web/default
bun run build
```

Expected: PASS.

- [ ] **Step 4: Check patch hygiene**

Run:

```bash
git diff --check
git status --short
```

Expected: `git diff --check` has no output. `git status --short` only shows intentional uncommitted work if the user chose not to commit intermediate tasks.

- [ ] **Step 5: Manual browser verification**

Start or reuse the dev servers:

```bash
go run .
```

```bash
cd web/default
PORT=3001 bun run dev
```

Open:

```text
http://localhost:3001/organization/dashboard
```

Verify:

- 조직 owner 계정은 대시보드를 볼 수 있다.
- 조직 admin 계정은 대시보드를 볼 수 있다.
- 조직 member 계정은 접근할 수 없다.
- 기간 선택을 오늘, 최근 7일, 최근 30일로 바꾸면 카드와 차트가 다시 조회된다.
- 상위 사용자와 상위 모델에 조직 밖 사용자의 데이터가 나오지 않는다.

- [ ] **Step 6: Commit final cleanup if needed**

If final verification required small fixes:

```bash
git add <changed-files>
git commit -m "fix: polish organization usage dashboard"
```

