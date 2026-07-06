# Org User Export/Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 조직 소유자/관리자가 조직 멤버를 Excel로 export하고, Excel로 import(기존 사용자 배정 + 신규 사용자 생성)할 수 있게 한다.

**Architecture:** 새 `service/org_user_export.go` 파일에 export/import 로직을 분리하고, `controller/organization.go`에 핸들러 2개를 추가한다. 프론트엔드는 `organization-users-table.tsx`에 버튼과 import dialog를 추가한다.

**Tech Stack:** Go, Gin, GORM, excelize/v2, React 19, TypeScript, Bun, i18next

---

## 파일 구조

| 파일 | 변경 |
|------|------|
| `service/org_user_export.go` | 새 파일: `BuildOrgExportFile`, `ImportOrgUsersFromFile`, `OrgImportResult` |
| `controller/organization.go` | `ExportOrganizationUsers`, `ImportOrganizationUsers` 핸들러 추가 |
| `router/api-router.go` | `GET/POST /api/organization/users/export|import` 등록 |
| `web/default/src/features/organizations/types.ts` | `OrgImportResult` 인터페이스 추가 |
| `web/default/src/features/organizations/api.ts` | `exportOrgUsers()`, `importOrgUsers(file)` 추가 |
| `web/default/src/features/organizations/components/org-users-import-dialog.tsx` | 새 파일: import dialog 컴포넌트 |
| `web/default/src/features/organizations/components/organization-users-table.tsx` | Export/Import 버튼 + dialog 연결 |

---

## Task 1: 백엔드 서비스 — `service/org_user_export.go`

**Files:**
- Create: `service/org_user_export.go`

### 배경 지식

- `model.User` 구조체: `Id`, `Username`, `DisplayName`, `Email`, `Role`, `Status`, `Group`, `Quota`, `UsedQuota`, `OrganizationId`, `OrganizationRole`, `Remark`
- `model.CheckUserExistOrDeleted(username, email) (bool, error)` — 이미 존재/삭제된 계정인지 확인
- `model.InvalidateUserCache(userId int) error` — 캐시 무효화
- `model.IncreaseUserQuota(userId, delta int, logRecord bool) error`
- `model.DecreaseUserQuota(userId, delta int, logRecord bool) error`
- `common.QuotaForNewUser` — 신규 유저 기본 쿼터
- `common.RoleAdminUser` — 관리자 role 상수 (10)
- JSON은 반드시 `common.Marshal`/`common.Unmarshal` 사용 (이 파일에서는 JSON 미사용)
- excelize는 `github.com/xuri/excelize/v2` (이미 go.mod에 있음)
- `model.OrganizationRoleMember = "member"`, `model.OrganizationRoleAdmin = "admin"`, `model.OrganizationRoleOwner = "owner"`

- [ ] **Step 1: `service/org_user_export.go` 파일 생성**

```go
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

// OrgExcelHeaders defines ordered columns for org user export/import.
var OrgExcelHeaders = []string{
	"username", "display_name", "organization_role",
	"quota", "used_quota", "group", "status", "remark", "initial_password",
}

// BuildOrgExportFile creates an Excel file of all members in an organization.
func BuildOrgExportFile(organizationId int) (*excelize.File, error) {
	var users []model.User
	err := model.DB.
		Where("organization_id = ? AND role < ?", organizationId, common.RoleAdminUser).
		Order("id asc").
		Find(&users).Error
	if err != nil {
		return nil, err
	}

	const maxExportRows = 10000
	if len(users) > maxExportRows {
		return nil, fmt.Errorf("too many members to export: %d (maximum %d)", len(users), maxExportRows)
	}

	f := excelize.NewFile()
	sheet := "OrgUsers"
	f.SetSheetName("Sheet1", sheet)

	for col, header := range OrgExcelHeaders {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	for rowIdx, u := range users {
		row := rowIdx + 2
		values := []interface{}{
			u.Username,
			u.DisplayName,
			u.OrganizationRole,
			u.Quota,
			u.UsedQuota,
			u.Group,
			statusToString(u.Status), // reuse from user_export.go in same package
			u.Remark,
			"", // initial_password: always empty on export
		}
		for col, val := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			f.SetCellValue(sheet, cell, val)
		}
	}

	return f, nil
}

// OrgImportResult holds the outcome of an org user import operation.
type OrgImportResult struct {
	Assigned         int      `json:"assigned"`
	Created          int      `json:"created"`
	Skipped          int      `json:"skipped"`
	SkippedUsernames []string `json:"skipped_usernames"`
	Errors           []string `json:"errors"`
}

// ImportOrgUsersFromFile parses an Excel file and assigns/creates users for an org.
func ImportOrgUsersFromFile(f *excelize.File, organizationId int) (*OrgImportResult, error) {
	result := &OrgImportResult{
		SkippedUsernames: []string{},
		Errors:           []string{},
	}

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read sheet: %w", err)
	}
	if len(rows) < 2 {
		return result, nil
	}

	const maxImportRows = 1000
	if len(rows)-1 > maxImportRows {
		return nil, fmt.Errorf("too many rows: maximum %d, got %d", maxImportRows, len(rows)-1)
	}

	headerIdx := map[string]int{}
	for i, h := range rows[0] {
		headerIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	getCell := func(row []string, key string) string {
		idx, ok := headerIdx[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	for rowNum, row := range rows[1:] {
		lineNum := rowNum + 2

		username := getCell(row, "username")
		if username == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: username is empty", lineNum))
			continue
		}

		orgRole := getCell(row, "organization_role")
		if orgRole == "" {
			orgRole = model.OrganizationRoleMember
		}
		if !model.IsValidOrganizationRole(orgRole) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid organization_role %q", lineNum, username, orgRole))
			continue
		}

		// Check if user exists
		var existingUser model.User
		err := model.DB.Where("username = ?", username).First(&existingUser).Error
		if err != nil && err.Error() != "record not found" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): db error: %v", lineNum, username, err))
			continue
		}

		userExists := existingUser.Id > 0

		if userExists {
			// Already in this org → skip
			if existingUser.OrganizationId == organizationId {
				result.Skipped++
				result.SkippedUsernames = append(result.SkippedUsernames, username)
				continue
			}
			// Belongs to another org → error
			if existingUser.OrganizationId > 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): belongs to another organization", lineNum, username))
				continue
			}
			// Not in any org → assign
			if err := model.DB.Model(&model.User{}).
				Select("organization_id", "organization_role").
				Where("id = ?", existingUser.Id).
				Updates(model.User{
					OrganizationId:   organizationId,
					OrganizationRole: orgRole,
				}).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to assign: %v", lineNum, username, err))
				continue
			}
			if err := model.InvalidateUserCache(existingUser.Id); err != nil {
				common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + username)
			}
			result.Assigned++
			continue
		}

		// User does not exist → create + assign
		password := getCell(row, "initial_password")
		if password == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): initial_password is required for new users", lineNum, username))
			continue
		}

		// Parse quota
		quota := 0
		if qs := getCell(row, "quota"); qs != "" {
			if q, err := strconv.Atoi(qs); err == nil {
				quota = q
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota %q", lineNum, username, qs))
				continue
			}
		}

		group := getCell(row, "group")
		if group == "" {
			group = "default"
		}
		displayName := getCell(row, "display_name")
		if displayName == "" {
			displayName = username
		}

		newUser := &model.User{
			Username:         username,
			Password:         password,
			DisplayName:      displayName,
			Role:             common.RoleCommonUser,
			Status:           stringToStatus(getCell(row, "status")), // reuse from user_export.go
			Group:            group,
			Remark:           getCell(row, "remark"),
			OrganizationId:   organizationId,
			OrganizationRole: orgRole,
		}

		// Insert with inviterId=0 to avoid referral bonus logic
		if err := newUser.Insert(0); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
			continue
		}

		// Assign org membership (Insert doesn't set org fields)
		if err := model.DB.Model(&model.User{}).
			Select("organization_id", "organization_role").
			Where("id = ?", newUser.Id).
			Updates(model.User{
				OrganizationId:   organizationId,
				OrganizationRole: orgRole,
			}).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set org membership: %v", lineNum, username, err))
			continue
		}

		// Adjust quota if differs from default
		if quota != 0 {
			delta := quota - int(common.QuotaForNewUser)
			if delta > 0 {
				if err := model.IncreaseUserQuota(newUser.Id, delta, true); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set quota: %v", lineNum, username, err))
				}
			} else if delta < 0 {
				if err := model.DecreaseUserQuota(newUser.Id, -delta, true); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set quota: %v", lineNum, username, err))
				}
			}
		}

		result.Created++
	}

	return result, nil
}
```

- [ ] **Step 2: 빌드 확인**

```bash
cd /home/molla/new-api && go build ./service/...
```

Expected: 오류 없음

- [ ] **Step 3: 커밋**

```bash
git add service/org_user_export.go
git commit -m "feat: add org user export/import service"
```

---

## Task 2: 백엔드 컨트롤러 + 라우터

**Files:**
- Modify: `controller/organization.go`
- Modify: `router/api-router.go`

### 배경 지식

- `resolveOrganizationAdminTarget(c)` — org admin/owner 또는 root(쿼리 파라미터 `organization_id` 필요)가 대상 org ID를 반환
- `c.Request.FormFile("file")` — multipart 파일 읽기
- `excelize.OpenReader(reader)` — Excel 파일 파싱
- `controller/user.go`의 `ExportUsers`/`ImportUsers` 패턴을 그대로 따름
- `fmt`와 `time`이 `controller/organization.go` imports에 있는지 확인; 없으면 추가

- [ ] **Step 1: `controller/organization.go` 하단에 핸들러 2개 추가**

먼저 imports를 확인:
```bash
grep -n '"fmt"\|"time"\|"net/http"\|excelize' /home/molla/new-api/controller/organization.go | head -10
```

`"fmt"`, `"time"`, `"net/http"` 없으면 import 블록에 추가. `excelize` import 추가:
```go
"github.com/xuri/excelize/v2"
```

그 다음 파일 하단에 추가:

```go
// ExportOrganizationUsers streams an Excel file of the org's members.
func ExportOrganizationUsers(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}

	f, err := service.BuildOrgExportFile(organizationId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer f.Close()

	filename := fmt.Sprintf("org-users-%d-%d.xlsx", organizationId, time.Now().Unix())
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	if err := f.Write(c.Writer); err != nil {
		common.ApiError(c, err)
	}
}

// ImportOrganizationUsers handles Excel upload and bulk org user assignment/creation.
func ImportOrganizationUsers(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}

	const maxSize = 10 << 20 // 10 MB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to read file: %w", err))
		return
	}
	defer file.Close()

	f, err := excelize.OpenReader(file)
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid xlsx file: %w", err))
		return
	}
	defer f.Close()

	result, err := service.ImportOrgUsersFromFile(f, organizationId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
```

- [ ] **Step 2: `router/api-router.go`에 라우트 등록**

파일에서 `/organization` 라우트 블록 내에서 아래를 찾아:
```go
organizationRoute.GET("/users", controller.ListOrganizationUsers)
```

그 바로 다음 줄에 추가:
```go
organizationRoute.GET("/users/export", controller.ExportOrganizationUsers)
organizationRoute.POST("/users/import", controller.ImportOrganizationUsers)
```

- [ ] **Step 3: 전체 빌드 확인**

```bash
cd /home/molla/new-api && go build ./...
```

Expected: 오류 없음

- [ ] **Step 4: 커밋**

```bash
git add controller/organization.go router/api-router.go
git commit -m "feat: add ExportOrganizationUsers and ImportOrganizationUsers handlers"
```

---

## Task 3: 프론트엔드 타입 + API 함수

**Files:**
- Modify: `web/default/src/features/organizations/types.ts`
- Modify: `web/default/src/features/organizations/api.ts`

- [ ] **Step 1: `types.ts`에 `OrgImportResult` 추가**

파일 하단(또는 `OrganizationUpdatePayload` 다음)에 추가:

```typescript
export interface OrgImportResult {
  assigned: number
  created: number
  skipped: number
  skipped_usernames: string[]
  errors: string[]
}
```

- [ ] **Step 2: `api.ts`에 `exportOrgUsers`, `importOrgUsers` 추가**

`deleteOrganization` 함수 다음에 추가:

```typescript
export async function exportOrgUsers(organizationId?: number): Promise<void> {
  const params = organizationId ? `?organization_id=${organizationId}` : ''
  const res = await api.get(`/api/organization/users/export${params}`, {
    responseType: 'blob',
  })
  const url = URL.createObjectURL(new Blob([res.data as BlobPart]))
  const a = document.createElement('a')
  a.href = url
  a.download = `org-users-${Date.now()}.xlsx`
  a.click()
  URL.revokeObjectURL(url)
}

export async function importOrgUsers(
  file: File,
  organizationId?: number
): Promise<OrgImportResult> {
  const params = organizationId ? `?organization_id=${organizationId}` : ''
  const form = new FormData()
  form.append('file', file)
  const res = await api.post(`/api/organization/users/import${params}`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return res.data.data as OrgImportResult
}
```

`OrgImportResult`를 import 블록에 추가:
```typescript
import type {
  ...
  OrgImportResult,
  ...
} from './types'
```

- [ ] **Step 3: 빌드 확인**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | tail -5
```

Expected: 오류 없음

- [ ] **Step 4: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/organizations/types.ts web/default/src/features/organizations/api.ts
git commit -m "feat: add OrgImportResult type and exportOrgUsers/importOrgUsers API functions"
```

---

## Task 4: Import Dialog 컴포넌트

**Files:**
- Create: `web/default/src/features/organizations/components/org-users-import-dialog.tsx`

이 컴포넌트는 `web/default/src/features/users/components/users-import-dialog.tsx`와 동일한 UX 패턴이지만 `OrgImportResult` 타입을 사용하고 결과 표시가 다르다.

- [ ] **Step 1: `org-users-import-dialog.tsx` 생성**

```tsx
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { importOrgUsers } from '../api'
import type { OrgImportResult } from '../types'

interface OrgUsersImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
  organizationId?: number
}

export function OrgUsersImportDialog({
  open,
  onOpenChange,
  onSuccess,
  organizationId,
}: OrgUsersImportDialogProps) {
  const { t } = useTranslation()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<OrgImportResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (f) {
      setFile(f)
      setResult(null)
      setError(null)
    }
  }

  const handleUpload = async () => {
    if (!file) return
    setLoading(true)
    setError(null)
    try {
      const res = await importOrgUsers(file, organizationId)
      setResult(res)
    } catch (e: any) {
      setError(e?.response?.data?.message ?? t('Import failed'))
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => {
    if (result) onSuccess()
    setFile(null)
    setResult(null)
    setError(null)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Import Organization Members')}</DialogTitle>
        </DialogHeader>

        <div className='flex flex-col gap-4 py-2'>
          <input
            ref={fileInputRef}
            type='file'
            accept='.xlsx'
            className='hidden'
            onChange={handleFileChange}
          />

          <Button
            variant='outline'
            onClick={() => fileInputRef.current?.click()}
            disabled={loading}
          >
            {file ? file.name : t('Select .xlsx file')}
          </Button>

          {result && (
            <div className='rounded-md border p-3 text-sm space-y-1'>
              {result.assigned > 0 && (
                <p className='text-green-600'>
                  ✓ {t('{{count}} members assigned', { count: result.assigned })}
                </p>
              )}
              {result.created > 0 && (
                <p className='text-green-600'>
                  ✓ {t('{{count}} members created', { count: result.created })}
                </p>
              )}
              {result.skipped > 0 && (
                <p className='text-amber-600'>
                  ⚠ {t('{{count}} members skipped (already in org)', { count: result.skipped })}: {result.skipped_usernames.join(', ')}
                </p>
              )}
              {result.errors.length > 0 && (
                <div className='text-red-600'>
                  <p>{t('Errors')}:</p>
                  <ul className='list-disc list-inside'>
                    {result.errors.map((e, i) => <li key={i}>{e}</li>)}
                  </ul>
                </div>
              )}
            </div>
          )}

          {error && (
            <p className='text-sm text-red-600'>{error}</p>
          )}
        </div>

        <DialogFooter className='gap-2'>
          {!result && (
            <Button
              onClick={handleUpload}
              disabled={!file || loading}
            >
              {loading ? t('Uploading...') : t('Upload')}
            </Button>
          )}
          <Button variant='outline' onClick={handleClose}>
            {result ? t('Close') : t('Cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
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
git add web/default/src/features/organizations/components/org-users-import-dialog.tsx
git commit -m "feat: add OrgUsersImportDialog component"
```

---

## Task 5: Export/Import 버튼 연결 + 배포

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-users-table.tsx`

현재 `organization-users-table.tsx`의 헤더 영역:
```tsx
<div className='flex items-center justify-between gap-3'>
  <h1 className='text-xl font-semibold'>{t('Organization Users')}</h1>
  <Button variant='outline' onClick={() => void loadUsers()} disabled={loading}>
    <RefreshCw />
    {t('Refresh')}
  </Button>
</div>
```

Export/Import 버튼은 `isOrganizationOwner` 조건으로 보여준다. root가 `organization_id` 쿼리 파라미터로 접근할 때는 조직 ID를 찾아 전달해야 한다. `organization-users-table.tsx` 컴포넌트가 `organizationId` prop을 받는지 먼저 확인:

```bash
grep -n "organizationId\|props\|interface.*Props" /home/molla/new-api/web/default/src/features/organizations/components/organization-users-table.tsx | head -10
```

컴포넌트가 `organizationId`를 prop으로 받지 않으면, `currentUser?.organization_id`를 사용한다. root 사용자가 이 화면을 볼 때는 URL 쿼리 파라미터나 선택된 조직 ID를 전달해야 하는데, 현재 구조에서 `currentUser?.organization_id`가 0이면 export 시 백엔드에서 organization_id가 필요하다는 에러가 난다. 따라서 **org admin/owner** 케이스 (isOrganizationOwner)만 버튼을 표시하면 충분하다.

- [ ] **Step 1: `OrgUsersImportDialog` import 및 상태 추가**

파일 상단 import에 추가:
```typescript
import { OrgUsersImportDialog } from './org-users-import-dialog'
```

`exportOrgUsers`를 `../api` import 블록에 추가:
```typescript
import {
  ...
  exportOrgUsers,
  ...
} from '../api'
```

컴포넌트 함수 내 상태 추가 (기존 `const [loading, ...` 근처):
```typescript
const [importDialogOpen, setImportDialogOpen] = useState(false)
const [exporting, setExporting] = useState(false)
```

- [ ] **Step 2: Export 핸들러 추가**

컴포넌트 내 함수들 중 (handleDeleteOrganization 근처) 추가:
```typescript
async function handleExportOrgUsers() {
  setExporting(true)
  try {
    await exportOrgUsers()
  } catch (e: any) {
    toast.error(e?.response?.data?.message ?? t('Export failed'))
  } finally {
    setExporting(false)
  }
}
```

- [ ] **Step 3: 헤더 영역에 버튼 추가**

현재:
```tsx
<div className='flex items-center justify-between gap-3'>
  <h1 className='text-xl font-semibold'>{t('Organization Users')}</h1>
  <Button variant='outline' onClick={() => void loadUsers()} disabled={loading}>
    <RefreshCw />
    {t('Refresh')}
  </Button>
</div>
```

다음으로 교체:
```tsx
<div className='flex items-center justify-between gap-3'>
  <h1 className='text-xl font-semibold'>{t('Organization Users')}</h1>
  <div className='flex items-center gap-2'>
    {isOrganizationOwner && (
      <>
        <Button
          variant='outline'
          onClick={() => void handleExportOrgUsers()}
          disabled={exporting}
        >
          {t('Export')}
        </Button>
        <Button
          variant='outline'
          onClick={() => setImportDialogOpen(true)}
        >
          {t('Import')}
        </Button>
      </>
    )}
    <Button variant='outline' onClick={() => void loadUsers()} disabled={loading}>
      <RefreshCw />
      {t('Refresh')}
    </Button>
  </div>
</div>
```

- [ ] **Step 4: Dialog 컴포넌트 렌더링 추가**

return 문 최상단 `<div className='space-y-4'>` 바로 다음에 추가:
```tsx
<OrgUsersImportDialog
  open={importDialogOpen}
  onOpenChange={setImportDialogOpen}
  onSuccess={() => void loadUsers()}
/>
```

- [ ] **Step 5: 빌드 확인**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | tail -5
```

Expected: 오류 없음. 에러 있으면 수정.

- [ ] **Step 6: i18n 동기화**

```bash
cd /home/molla/new-api/web/default && bun run i18n:sync 2>&1 | tail -10
```

- [ ] **Step 7: 백엔드/프론트엔드 빌드 및 배포**

```bash
cd /home/molla/new-api
go build -o ./tmp/new-api .
sudo systemctl stop new-api
cp ./tmp/new-api ./new-api
sudo systemctl start new-api
sleep 3
sudo systemctl status new-api --no-pager | tail -5
```

- [ ] **Step 8: 커밋**

```bash
cd /home/molla/new-api
git add web/default/src/features/organizations/components/organization-users-table.tsx web/default/src/i18n/
git commit -m "feat: add export/import buttons to organization users table"
```
