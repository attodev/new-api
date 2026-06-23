# 조직 사용자 테이블 UX 개선 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 조직 사용자 테이블에 멤버 제거 API, pending state 모델(저장/취소 패턴), 체크박스 다중 선택 + 일괄 작업을 추가한다.

**Architecture:** 백엔드에 `DELETE /api/organization/users/:id/membership` 엔드포인트를 추가하고, 프론트엔드 `organization-users-table.tsx`를 pending state 모델로 전면 리팩토링한다. 모든 변경(quota, status, remove)은 클라이언트 상태에만 쌓이다가 저장 버튼을 누를 때 일괄 API 호출 후 목록 재로드된다.

**Tech Stack:** Go (Gin, GORM), React 19, TypeScript, Tailwind CSS

---

## 변경 파일 목록

| 파일 | 작업 |
|---|---|
| `router/api-router.go` | DELETE `/api/organization/users/:id/membership` 라우트 추가 |
| `controller/organization.go` | `RemoveOrganizationUserMembership` 핸들러 추가 |
| `controller/organization_test.go` | 새 핸들러 테스트 추가 |
| `web/default/src/features/organizations/api.ts` | `removeOrganizationUserMembership` 함수 추가 |
| `web/default/src/features/organizations/components/organization-users-table.tsx` | pending state 모델 + 체크박스 + 툴바 + 배너 리팩토링 |

---

### Task 1: 백엔드 — RemoveOrganizationUserMembership 핸들러

**Files:**
- Modify: `controller/organization.go` (AssignOrganizationUser 함수 끝 이후, 약 729번 줄 이후)
- Modify: `router/api-router.go`

- [ ] **Step 1: `controller/organization.go`에 핸들러 추가**

`AssignOrganizationUser` 함수(729번 줄) 바로 뒤에 추가:

```go
func RemoveOrganizationUserMembership(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationOwnerRole(actor.OrganizationRole) {
		common.ApiError(c, errors.New("organization owner permission required"))
		return
	}

	targetId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	target, err := model.GetUserById(targetId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if target.OrganizationId != actor.OrganizationId {
		common.ApiError(c, errors.New("user does not belong to your organization"))
		return
	}
	if model.HasOrganizationOwnerRole(target.OrganizationRole) {
		common.ApiError(c, errors.New("organization owner cannot be removed"))
		return
	}

	if err := model.DB.Model(&model.User{}).
		Select("organization_id", "organization_role").
		Where("id = ?", target.Id).
		Updates(model.User{OrganizationId: 0, OrganizationRole: ""}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate cache after membership removal: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
```

- [ ] **Step 2: `router/api-router.go`에 라우트 추가**

파일에서 다음 줄을 찾는다:
```go
organizationUserGroup.PUT("/users/:id/membership", controller.AssignOrganizationUser)
```

그 바로 다음 줄에 추가:
```go
organizationUserGroup.DELETE("/users/:id/membership", controller.RemoveOrganizationUserMembership)
```

- [ ] **Step 3: 빌드 확인**

```bash
cd /home/molla/new-api && go build ./...
```

Expected: 에러 없이 완료

- [ ] **Step 4: Commit**

```bash
git add controller/organization.go router/api-router.go
git commit -m "feat: add DELETE /organization/users/:id/membership endpoint"
```

---

### Task 2: 백엔드 — 핸들러 테스트

**Files:**
- Modify: `controller/organization_test.go`

- [ ] **Step 1: 테스트 추가**

`controller/organization_test.go` 파일 끝에 다음 테스트들을 추가한다:

```go
func TestRemoveOrganizationUserMembershipSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(member.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(member.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.True(t, payload.Success)

	var updated model.User
	require.NoError(t, model.DB.First(&updated, member.Id).Error)
	require.Equal(t, 0, updated.OrganizationId)
	require.Equal(t, "", updated.OrganizationRole)
}

func TestRemoveOrganizationUserMembershipRejectsOwner(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	owner2 := model.User{Username: "owner2", Password: "x", Role: common.RoleCommonUser, AffCode: "o2", OrganizationId: 1, OrganizationRole: "owner"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&owner2).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(owner2.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(owner2.Id)},
	)
	requireOrganizationApiError(t, res, "organization owner cannot be removed")
}

func TestRemoveOrganizationUserMembershipRejectsNonMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	outsider := model.User{Username: "outsider", Password: "x", Role: common.RoleCommonUser, AffCode: "out", OrganizationId: 2, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&outsider).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(outsider.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(outsider.Id)},
	)
	requireOrganizationApiError(t, res, "user does not belong to your organization")
}

func TestRemoveOrganizationUserMembershipRejectsNonOwner(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "admin"}
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		admin,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(member.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(member.Id)},
	)
	requireOrganizationApiError(t, res, "organization owner permission required")
}

func TestRemoveOrganizationUserMembershipRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/abc/membership",
		"",
		gin.Param{Key: "id", Value: "abc"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct{ Success bool `json:"success"` }
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.False(t, payload.Success)
}
```

- [ ] **Step 2: 테스트 실행**

```bash
cd /home/molla/new-api && go test ./controller/ -run "TestRemoveOrganization" -v
```

Expected: 5개 테스트 모두 PASS

- [ ] **Step 3: Commit**

```bash
git add controller/organization_test.go
git commit -m "test: add tests for RemoveOrganizationUserMembership"
```

---

### Task 3: 프론트엔드 — API 함수 추가

**Files:**
- Modify: `web/default/src/features/organizations/api.ts`

- [ ] **Step 1: `removeOrganizationUserMembership` 함수 추가**

`api.ts`에서 `assignOrganizationUser` 함수(298번 줄) 바로 뒤에 추가:

```typescript
export async function removeOrganizationUserMembership(
  userId: number
): Promise<ApiResponse> {
  const res = await api.delete(
    `/api/organization/users/${userId}/membership`
  )
  return res.data
}
```

- [ ] **Step 2: TypeScript 타입 체크**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | grep -E "error|Error" | head -20
```

Expected: api.ts 관련 에러 없음

- [ ] **Step 3: Commit**

```bash
git add web/default/src/features/organizations/api.ts
git commit -m "feat: add removeOrganizationUserMembership API function"
```

---

### Task 4: 프론트엔드 — organization-users-table.tsx pending state 리팩토링

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-users-table.tsx`

이 태스크는 테이블 컴포넌트에 pending state 모델을 도입한다. 기존 즉시 반영 방식(onBlur, toggleStatus)을 모두 pending 큐 방식으로 교체한다.

#### 변경 범위

1. **새 import 추가**: `removeOrganizationUserMembership`, `Checkbox` (lucide-react의 `CheckSquare` 또는 HTML checkbox)
2. **새 state 추가**: `pendingChanges`, `selectedUserIds`
3. **기존 함수 교체**: `saveQuota`, `saveDisplayQuota`, `toggleStatus` → pending 방식으로
4. **새 함수 추가**: `handleSavePending`, `handleCancelPending`, `handleBulkDisable`, `handleBulkRemove`
5. **JSX 수정**: 배너, 툴바, 헤더 체크박스, 행 체크박스, quota 셀, 상태 셀, Actions 컬럼 제거

- [ ] **Step 1: 파일 상단 imports 수정**

기존 import 블록에서 `removeOrganizationUserMembership`을 추가하고 불필요한 `Power` 아이콘 제거:

```typescript
// 기존 from '../api' import에 추가
import {
  assignOrganizationUser,
  createOrganization,
  deleteOrganization,
  exportOrgUsers,
  getAssignableOrganizationUsers,
  getOrganizationProfile,
  getOrganizationUsers,
  getOrganizationUserSubscriptions,
  getOrganizations,
  removeOrganizationUserMembership,
  updateOrganization,
  updateOrganizationUser,
} from '../api'
```

lucide-react에서 `Power` 제거 (더 이상 행별 토글 버튼 없음):
```typescript
import {
  BarChart3,
  Building2,
  RefreshCw,
  Save,
  UserPlus,
  Wallet,
} from 'lucide-react'
```

- [ ] **Step 2: PendingChange 타입 및 state 추가**

`OrganizationUsersTable` 컴포넌트 함수 상단(기존 state 선언 부분) 직전에 타입 선언 추가:

```typescript
type PendingChange =
  | { type: 'quota'; userId: number; newQuota: number; originalQuota: number }
  | { type: 'status'; userId: number; newStatus: number }
  | { type: 'remove'; userId: number }
```

컴포넌트 내 state 선언부에 추가 (기존 `const [loading, setLoading] = useState(false)` 근처):

```typescript
const [pendingChanges, setPendingChanges] = useState<PendingChange[]>([])
const [selectedUserIds, setSelectedUserIds] = useState<Set<number>>(new Set())
const [saving, setSaving] = useState(false)
```

- [ ] **Step 3: pending 관련 헬퍼 함수 추가**

`loadUsers` 함수 앞에 추가:

```typescript
function getPendingForUser(userId: number): PendingChange | undefined {
  // 제거가 최우선, 그 다음 status, quota 순서로 반환
  return (
    pendingChanges.find(c => c.type === 'remove' && c.userId === userId) ??
    pendingChanges.find(c => c.type === 'status' && c.userId === userId) ??
    pendingChanges.find(c => c.type === 'quota' && c.userId === userId)
  )
}

function setPendingChange(change: PendingChange) {
  setPendingChanges(prev => {
    const filtered = prev.filter(c => {
      if (c.userId !== change.userId) return true
      if (c.type !== change.type) return true
      return false
    })
    return [...filtered, change]
  })
}

function removePendingChange(userId: number, type: PendingChange['type']) {
  setPendingChanges(prev =>
    prev.filter(c => !(c.userId === userId && c.type === type))
  )
}
```

- [ ] **Step 4: 기존 saveUser, saveQuota, saveDisplayQuota, toggleStatus 함수 교체**

기존 `saveUser`, `saveQuota`, `saveDisplayQuota`, `toggleStatus` 함수를 **모두 삭제**하고 다음으로 교체:

```typescript
function handleQuotaChange(user: OrganizationUser, displayValue: string) {
  const value = Number(displayValue)
  if (!Number.isFinite(value)) return
  const newQuota = parseQuotaFromDollars(value)
  if (newQuota === user.quota) {
    removePendingChange(user.id, 'quota')
    return
  }
  setPendingChange({ type: 'quota', userId: user.id, newQuota, originalQuota: user.quota })
}

function handleQuotaAdd(user: OrganizationUser, amount: number) {
  const quotaChange = pendingChanges.find(c => c.type === 'quota' && c.userId === user.id) as
    | { type: 'quota'; userId: number; newQuota: number; originalQuota: number }
    | undefined
  const base = quotaChange ? quotaChange.newQuota : user.quota
  const newQuota = base + parseQuotaFromDollars(amount)
  setPendingChange({ type: 'quota', userId: user.id, newQuota, originalQuota: user.quota })
}

function handleToggleStatus(user: OrganizationUser) {
  const statusChange = pendingChanges.find(c => c.type === 'status' && c.userId === user.id) as
    | { type: 'status'; userId: number; newStatus: number }
    | undefined
  const currentStatus = statusChange ? statusChange.newStatus : user.status
  const newStatus = currentStatus === USER_STATUS_ENABLED ? USER_STATUS_DISABLED : USER_STATUS_ENABLED
  if (newStatus === user.status) {
    removePendingChange(user.id, 'status')
  } else {
    setPendingChange({ type: 'status', userId: user.id, newStatus })
  }
}

function handleMarkRemove(userId: number) {
  // 제거 예정으로 표시 — 다른 pending도 제거
  setPendingChanges(prev => [
    ...prev.filter(c => c.userId !== userId),
    { type: 'remove', userId },
  ])
  setSelectedUserIds(prev => {
    const next = new Set(prev)
    next.delete(userId)
    return next
  })
}

function handleCancelPending() {
  setPendingChanges([])
  setSelectedUserIds(new Set())
}

async function handleSavePending() {
  if (pendingChanges.length === 0) return
  setSaving(true)
  const errors: string[] = []
  await Promise.all(
    pendingChanges.map(async change => {
      try {
        if (change.type === 'quota') {
          const res = await updateOrganizationUser(change.userId, { quota: change.newQuota })
          if (!res.success) errors.push(res.message ?? t('Failed to update quota'))
        } else if (change.type === 'status') {
          const res = await updateOrganizationUser(change.userId, { status: change.newStatus })
          if (!res.success) errors.push(res.message ?? t('Failed to update status'))
        } else if (change.type === 'remove') {
          const res = await removeOrganizationUserMembership(change.userId)
          if (!res.success) errors.push(res.message ?? t('Failed to remove member'))
        }
      } catch (e: unknown) {
        errors.push(e instanceof Error ? e.message : t('Unknown error'))
      }
    })
  )
  setSaving(false)
  if (errors.length > 0) {
    errors.forEach(msg => toast.error(msg))
  } else {
    toast.success(t('Changes saved'))
  }
  setPendingChanges([])
  setSelectedUserIds(new Set())
  await loadUsers()
}
```

- [ ] **Step 5: 일괄 작업 함수 추가**

`handleSavePending` 다음에 추가:

```typescript
function handleBulkDisable() {
  selectedUserIds.forEach(userId => {
    const user = users.find(u => u.id === userId)
    if (!user) return
    if (user.status === USER_STATUS_ENABLED) {
      setPendingChange({ type: 'status', userId, newStatus: USER_STATUS_DISABLED })
    }
  })
}

function handleBulkRemove() {
  selectedUserIds.forEach(userId => {
    handleMarkRemove(userId)
  })
}

function handleSelectAll(checked: boolean) {
  if (checked) {
    const selectableIds = users
      .filter(u => u.organization_role !== ORGANIZATION_ROLE.OWNER)
      .map(u => u.id)
    setSelectedUserIds(new Set(selectableIds))
  } else {
    setSelectedUserIds(new Set())
  }
}

function handleSelectUser(userId: number, checked: boolean) {
  setSelectedUserIds(prev => {
    const next = new Set(prev)
    if (checked) next.add(userId)
    else next.delete(userId)
    return next
  })
}
```

- [ ] **Step 6: JSX — 저장/취소 배너 추가**

`return (` 직후 `<div className='space-y-4'>` 바로 다음(OrgUsersImportDialog 바로 뒤)에 배너 추가:

```tsx
{pendingChanges.length > 0 && (
  <div className='flex items-center justify-between rounded-md border border-blue-200 bg-blue-50 px-4 py-2 text-sm'>
    <span className='text-blue-700'>
      {[
        pendingChanges.filter(c => c.type === 'quota').length > 0 &&
          t('{{count}} quota changes', { count: pendingChanges.filter(c => c.type === 'quota').length }),
        pendingChanges.filter(c => c.type === 'status').length > 0 &&
          t('{{count}} status changes', { count: pendingChanges.filter(c => c.type === 'status').length }),
        pendingChanges.filter(c => c.type === 'remove').length > 0 &&
          t('{{count}} pending removal', { count: pendingChanges.filter(c => c.type === 'remove').length }),
      ]
        .filter(Boolean)
        .join(' · ')}
    </span>
    <div className='flex gap-2'>
      <Button size='sm' onClick={() => void handleSavePending()} disabled={saving}>
        {t('Save')}
      </Button>
      <Button size='sm' variant='outline' onClick={handleCancelPending} disabled={saving}>
        {t('Cancel')}
      </Button>
    </div>
  </div>
)}
```

- [ ] **Step 7: JSX — 검색 행에 선택 툴바 추가**

기존 검색 Input이 있는 `<div className='flex items-center gap-2'>` 블록을 다음으로 교체:

```tsx
<div className='flex flex-wrap items-center justify-between gap-2'>
  <div className='flex items-center gap-2'>
    <Input
      value={keywordInput}
      onChange={(e) => setKeywordInput(e.target.value)}
      placeholder={t('Search by username or display name')}
      className='max-w-xs'
    />
    {keywordInput && (
      <Button
        variant='ghost'
        size='sm'
        onClick={() => { setKeywordInput(''); setKeyword(''); setCurrentPage(1) }}
      >
        ✕
      </Button>
    )}
  </div>
  {selectedUserIds.size > 0 && (
    <div className='flex items-center gap-2 text-sm'>
      <span className='text-muted-foreground'>
        {t('{{count}} selected', { count: selectedUserIds.size })}
      </span>
      <div className='h-4 w-px bg-border' />
      <Button size='sm' variant='outline' onClick={handleBulkDisable}>
        {t('Disable')}
      </Button>
      <Button size='sm' variant='outline' onClick={handleBulkRemove} className='border-destructive text-destructive'>
        {t('Remove from organization')}
      </Button>
    </div>
  )}
</div>
```

- [ ] **Step 8: JSX — 테이블 헤더에 체크박스 컬럼 추가**

기존 `<thead>` 내 `<tr>` 첫 번째 `<th>` 앞에 체크박스 열 추가:

```tsx
<th className='w-10 px-3 py-2'>
  <input
    type='checkbox'
    checked={
      users.filter(u => u.organization_role !== ORGANIZATION_ROLE.OWNER).length > 0 &&
      users
        .filter(u => u.organization_role !== ORGANIZATION_ROLE.OWNER)
        .every(u => selectedUserIds.has(u.id))
    }
    onChange={e => handleSelectAll(e.target.checked)}
    className='cursor-pointer'
  />
</th>
```

기존 `<th className='px-3 py-2 text-right font-medium'>{t('Actions')}</th>` 를 **제거**.

- [ ] **Step 9: JSX — 각 행에 체크박스 추가, 상태 변경, quota 변경, Actions 열 제거**

기존 `users.map((user) => { ... })` 블록을 교체. 핵심 변경사항:

```tsx
{users.map((user) => {
  const isOwner = user.organization_role === ORGANIZATION_ROLE.OWNER
  const pendingRemove = pendingChanges.find(c => c.type === 'remove' && c.userId === user.id)
  const pendingStatus = pendingChanges.find(c => c.type === 'status' && c.userId === user.id) as
    | { type: 'status'; userId: number; newStatus: number } | undefined
  const pendingQuota = pendingChanges.find(c => c.type === 'quota' && c.userId === user.id) as
    | { type: 'quota'; userId: number; newQuota: number; originalQuota: number } | undefined
  const quotaControlState = getOrganizationQuotaControlState(
    user.id,
    user.quota,
    subscriptionRecords
  )
  const hasActivePlan = quotaControlState.hasActivePlan
  const effectiveStatus = pendingStatus ? pendingStatus.newStatus : user.status

  return (
    <tr key={user.id} className='border-t'>
      <td className='px-3 py-2'>
        <input
          type='checkbox'
          disabled={isOwner}
          checked={!isOwner && selectedUserIds.has(user.id)}
          onChange={e => !isOwner && handleSelectUser(user.id, e.target.checked)}
          className={isOwner ? 'cursor-not-allowed opacity-30' : 'cursor-pointer'}
          title={isOwner ? t('Owner cannot be modified') : undefined}
        />
      </td>
      <td className='px-3 py-2'>
        <div className={`font-medium${pendingRemove ? ' line-through text-muted-foreground' : ''}`}>
          {user.username}
        </div>
        {user.display_name && (
          <div className='text-muted-foreground text-xs'>{user.display_name}</div>
        )}
        {pendingRemove && (
          <div className='text-xs text-destructive'>{t('Pending removal')}</div>
        )}
        {!pendingRemove && pendingStatus && (
          <div className='text-xs text-orange-600'>
            {pendingStatus.newStatus === USER_STATUS_DISABLED
              ? t('Pending disable')
              : t('Pending enable')}
          </div>
        )}
        {!pendingRemove && !pendingStatus && pendingQuota && (
          <div className='text-xs text-amber-700'>{t('Quota modified')}</div>
        )}
      </td>
      <td className={`px-3 py-2${pendingRemove ? ' text-muted-foreground line-through' : ''}`}>
        {user.organization_role}
      </td>
      <td className={`px-3 py-2${pendingRemove ? ' text-muted-foreground line-through' : ''}`}>
        {effectiveStatus === USER_STATUS_ENABLED ? t('Enabled') : t('Disabled')}
      </td>
      <td className='px-3 py-2'>
        <div className='min-w-64 space-y-2'>
          <div className='text-muted-foreground text-xs'>
            {t('Current quota balance')}:{' '}
            {hasActivePlan
              ? `${formatQuota(0)} (${t('Organization plan in use')})`
              : formatQuota(quotaControlState.displayQuota)}
          </div>
          <div className='flex items-center gap-2'>
            <Input
              key={`${user.id}-${user.quota}-${hasActivePlan}`}
              className={`w-32${pendingQuota ? ' border-amber-500' : ''}`}
              type='number'
              step={tokensOnly ? 1 : 0.01}
              min={0}
              defaultValue={quotaUnitsToDollars(
                pendingQuota ? pendingQuota.newQuota : quotaControlState.displayQuota
              )}
              placeholder={t('Quota amount')}
              disabled={!!pendingRemove || quotaControlState.disabled}
              onBlur={(event) => {
                if (!shouldDisableQuotaForOrganizationSubscription(user.id, subscriptionRecords)) {
                  handleQuotaChange(user, event.currentTarget.value)
                }
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter') event.currentTarget.blur()
              }}
            />
            <span className='text-muted-foreground text-xs'>{currencyLabel}</span>
            {pendingQuota && (
              <span className='text-muted-foreground text-xs'>
                ({t('was')} {quotaUnitsToDollars(pendingQuota.originalQuota).toFixed(2)})
              </span>
            )}
          </div>
          {!tokensOnly && (
            <div className='flex flex-wrap gap-1'>
              {QUICK_QUOTA_AMOUNTS.map((amount) => (
                <Button
                  key={amount}
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={!!pendingRemove || quotaControlState.disabled}
                  onClick={() => handleQuotaAdd(user, amount)}
                >
                  +{formatQuota(parseQuotaFromDollars(amount))}
                </Button>
              ))}
            </div>
          )}
        </div>
      </td>
    </tr>
  )
})}
```

주의: Actions 열(`<td className='px-3 py-2 text-right'>`) 전체 삭제. 테이블 헤더에서도 Actions `<th>` 삭제.

- [ ] **Step 10: `shouldDisableQuotaForOrganizationSubscription` import 확인**

파일 상단 import에 이미 있는지 확인:
```typescript
import {
  getActiveOrganizationSubscriptionUserIds,
  getOrganizationQuotaControlState,
  shouldDisableQuotaForOrganizationSubscription,
} from '../lib/organization-subscription-utils'
```

없으면 추가.

- [ ] **Step 11: TypeScript 빌드 확인**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | grep -E "error TS|Error" | head -30
```

Expected: 에러 없음

- [ ] **Step 12: Commit**

```bash
git add web/default/src/features/organizations/components/organization-users-table.tsx
git commit -m "feat: refactor org user table to pending state model with bulk actions"
```

---

### Task 5: i18n 동기화 및 빌드 배포

**Files:**
- Modify: `web/default/src/i18n/locales/*.json` (자동 생성)

- [ ] **Step 1: i18n 동기화**

```bash
cd /home/molla/new-api/web/default && bun run i18n:sync
```

Expected: 새 키들(`Pending removal`, `Pending disable`, `Pending enable`, `Quota modified`, `Remove from organization`, `Changes saved`, `{{count}} quota changes`, `{{count}} status changes`, `{{count}} pending removal`, `Owner cannot be modified`, `was`, `{{count}} selected`) 이 en.json에 추가됨

- [ ] **Step 2: 프로덕션 빌드**

```bash
cd /home/molla/new-api/web/default && bun run build
```

Expected: 에러 없이 빌드 완료

- [ ] **Step 3: 서버 재시작 및 수동 검증**

```bash
cd /home/molla/new-api && pkill new-api-bin 2>/dev/null; go build -o new-api-bin . && ./new-api-bin &
```

브라우저에서 조직 사용자 화면 접속 후 다음 확인:
1. 체크박스가 각 행에 표시되고 owner 행은 비활성화됨
2. 체크박스 선택 시 상단 툴바에 "비활성화", "조직에서 제거" 버튼 표시
3. quota 변경 시 즉시 저장 안 되고 배너에 "1 quota changes" 표시
4. 저장 버튼 클릭 시 일괄 처리 후 목록 재로드
5. 취소 버튼 클릭 시 모든 pending 상태 초기화
6. "조직에서 제거" 후 저장 시 해당 사용자가 목록에서 사라짐

- [ ] **Step 4: Commit**

```bash
git add web/default/src/i18n/locales/
git commit -m "feat: sync i18n for org user table UX improvements"
```
