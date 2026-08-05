# Organization Delete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Root 관리자가 소속 멤버가 없는 조직을 삭제할 수 있게 한다.

**Architecture:** 트랜잭션 안에서 멤버 수 확인 → 구독 데이터 삭제 → 조직 삭제 순서로 처리. 백엔드 model → controller → router 순서로 추가하고, 프론트엔드는 API 함수 + 조직 카드에 삭제 버튼 + 확인 Dialog를 추가한다.

**Tech Stack:** Go, Gin, GORM, React 19, TypeScript, Bun

---

## 파일 구조

| 파일 | 변경 |
|------|------|
| `model/organization.go` | `DeleteOrganization(id int) error` 추가 |
| `controller/organization.go` | `DeleteOrganization(c *gin.Context)` 핸들러 추가 |
| `router/api-router.go` | `DELETE /api/organizations/:id` 등록 |
| `web/default/src/features/organizations/api.ts` | `deleteOrganization(id)` 추가 |
| `web/default/src/features/organizations/components/organization-users-table.tsx` | 삭제 버튼 + 확인 Dialog 추가 |

---

## Task 1: 모델 — DeleteOrganization

**Files:**
- Modify: `model/organization.go`

- [ ] **Step 1: `DeleteOrganization` 함수를 `model/organization.go` 하단에 추가**

```go
// DeleteOrganization removes an organization and all its subscription data atomically.
// Returns an error if the organization still has active members.
func DeleteOrganization(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 1. Check for active members
		var memberCount int64
		if err := tx.Model(&User{}).
			Where("organization_id = ? AND deleted_at IS NULL", id).
			Count(&memberCount).Error; err != nil {
			return err
		}
		if memberCount > 0 {
			return fmt.Errorf("organization has %d members, remove them first", memberCount)
		}

		// 2. Delete user subscriptions
		if err := tx.Where("organization_id = ?", id).
			Delete(&OrganizationUserSubscription{}).Error; err != nil {
			return err
		}

		// 3. Delete subscription plans
		if err := tx.Where("organization_id = ?", id).
			Delete(&OrganizationSubscriptionPlan{}).Error; err != nil {
			return err
		}

		// 4. Delete the organization
		result := tx.Where("id = ?", id).Delete(&Organization{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
	})
}
```

Note: `fmt` is already imported in `model/organization.go` — check first. If not, add it to the import block.

- [ ] **Step 2: 빌드 확인**

```bash
cd /home/molla/new-api && go build ./model/...
```

Expected: 오류 없음

- [ ] **Step 3: 커밋**

```bash
git add model/organization.go
git commit -m "feat: add DeleteOrganization model function"
```

---

## Task 2: 컨트롤러 핸들러 + 라우터 등록

**Files:**
- Modify: `controller/organization.go`
- Modify: `router/api-router.go`

- [ ] **Step 1: `controller/organization.go` 하단에 핸들러 추가**

`strconv`가 이미 import되어 있는지 확인. 없으면 추가.

```go
func DeleteOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiError(c, errors.New("root permission required"))
		return
	}

	organizationId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := model.DeleteOrganization(organizationId); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "organization not found"})
			return
		}
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, nil)
}
```

Note: `gorm.io/gorm` import가 controller/organization.go에 없으면 추가. 확인:
```bash
grep "gorm.io/gorm" /home/molla/new-api/controller/organization.go
```

- [ ] **Step 2: `router/api-router.go`에 라우트 등록**

파일에서 아래 라인을 찾아:
```go
organizationsRoute.PATCH("/:id", controller.UpdateOrganization)
```

바로 다음에 추가:
```go
organizationsRoute.DELETE("/:id", controller.DeleteOrganization)
```

- [ ] **Step 3: 전체 빌드 확인**

```bash
cd /home/molla/new-api && go build ./...
```

Expected: 오류 없음

- [ ] **Step 4: 커밋**

```bash
git add controller/organization.go router/api-router.go
git commit -m "feat: add DeleteOrganization handler and route"
```

---

## Task 3: 프론트엔드 API 함수

**Files:**
- Modify: `web/default/src/features/organizations/api.ts`

- [ ] **Step 1: `api.ts`에 `deleteOrganization` 함수 추가**

`updateOrganization` 함수 바로 다음에 추가:

```typescript
export async function deleteOrganization(organizationId: number): Promise<void> {
  await api.delete(`/api/organizations/${organizationId}`)
}
```

- [ ] **Step 2: 빌드 확인**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | tail -5
```

Expected: 오류 없음

- [ ] **Step 3: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/organizations/api.ts
git commit -m "feat: add deleteOrganization API function"
```

---

## Task 4: 프론트엔드 UI — 삭제 버튼 + Dialog

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-users-table.tsx`

이 파일의 `isRoot && organizations.length > 0` 블록 안에 각 조직 카드에 삭제 버튼을 추가하고, 확인 Dialog를 연결한다.

- [ ] **Step 1: import에 `deleteOrganization` 추가**

파일 상단의 organizations api import 블록을 찾아 (`../api`에서 import하는 부분):

```typescript
import {
  assignOrganizationUser,
  createOrganization,
  deleteOrganization,         // ← 추가
  getAssignableOrganizationUsers,
  getOrganizationProfile,
  getOrganizationUsers,
  getOrganizationUserSubscriptions,
  getOrganizations,
  updateOrganization,
  updateOrganizationUser,
} from '../api'
```

- [ ] **Step 2: AlertDialog import 추가**

파일 상단 UI imports에 추가 (아직 없다면):

```typescript
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
```

확인:
```bash
grep "AlertDialog" /home/molla/new-api/web/default/src/features/organizations/components/organization-users-table.tsx
```
이미 있으면 추가 불필요.

확인:
```bash
find /home/molla/new-api/web/default/src/components/ui -name "alert-dialog*"
```

- [ ] **Step 3: 삭제 핸들러 함수 추가**

컴포넌트 함수 내부, 기존 `saveOrganizationQuota` 함수 근처에 추가:

```typescript
const handleDeleteOrganization = async (organization: Organization) => {
  try {
    await deleteOrganization(organization.id)
    toast.success(t('Organization deleted'))
    void refreshOrganizations()
  } catch (e: any) {
    toast.error(e?.response?.data?.message ?? t('Failed to delete organization'))
  }
}
```

`refreshOrganizations`가 컴포넌트에 있는지 확인:
```bash
grep -n "refreshOrganization\|loadOrganization\|fetchOrganization\|getOrganizations" /home/molla/new-api/web/default/src/features/organizations/components/organization-users-table.tsx | head -10
```

데이터를 다시 불러오는 함수 이름을 확인해서 맞게 사용.

- [ ] **Step 4: 조직 카드에 삭제 버튼 추가**

각 조직 카드 (`organizations.map((organization) => (...)`) 안의 버튼 영역에 삭제 버튼을 추가한다.

현재 카드 구조 (line ~576~628):
```tsx
<div key={organization.id} className='rounded-md border p-3'>
  <div className='flex items-start justify-between gap-2'>
    <div>
      <div className='font-medium'>{organization.name}</div>
      ...
    </div>
    <span className='text-muted-foreground text-xs'>#{organization.id}</span>
  </div>
  <div className='mt-3 flex items-center gap-2'>
    <Input ... />
    <span>...</span>
    <Button ...>{t('Save')}</Button>
  </div>
</div>
```

Save 버튼 다음에 AlertDialog로 감싼 삭제 버튼 추가:

```tsx
<AlertDialog>
  <AlertDialogTrigger asChild>
    <Button
      type='button'
      variant='destructive'
      size='sm'
    >
      {t('Delete')}
    </Button>
  </AlertDialogTrigger>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>{t('Delete Organization')}</AlertDialogTitle>
      <AlertDialogDescription>
        {t('Are you sure you want to delete organization "{{name}}"? This action cannot be undone.', { name: organization.name })}
      </AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
      <AlertDialogAction
        onClick={() => void handleDeleteOrganization(organization)}
      >
        {t('Delete')}
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

- [ ] **Step 5: 빌드 확인**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | tail -10
```

Expected: 오류 없음. 빌드 에러가 있으면 수정.

- [ ] **Step 6: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/organizations/components/organization-users-table.tsx
git commit -m "feat: add organization delete button and confirmation dialog"
```

---

## Task 5: i18n 동기화 및 서버 반영

**Files:**
- Modify: `web/default/src/i18n/locales/*.json`
- Binary: `new-api`

- [ ] **Step 1: i18n 동기화**

```bash
cd /home/molla/new-api/web/default && bun run i18n:sync 2>&1 | tail -10
```

새 번역 키(`Delete Organization`, `Are you sure you want to delete organization...`, `Organization deleted`, `Failed to delete organization`) 추가 확인.

- [ ] **Step 2: 프론트엔드 빌드**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | tail -5
```

- [ ] **Step 3: 백엔드 바이너리 빌드 및 배포**

```bash
cd /home/molla/new-api
go build -o ./tmp/new-api .
cp ./tmp/new-api ./new-api
sudo systemctl restart new-api
sleep 3
sudo systemctl status new-api --no-pager | tail -5
```

- [ ] **Step 4: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/i18n/
git commit -m "chore: sync i18n for organization delete feature"
```
