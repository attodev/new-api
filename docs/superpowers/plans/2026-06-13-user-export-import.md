# User Export / Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 관리자가 사용자 목록을 Excel로 export하고, Excel 파일로 사용자를 일괄 import할 수 있게 한다.

**Architecture:** Go 백엔드에서 excelize 라이브러리로 Excel 생성/파싱 처리. `GET /api/user/export`는 파일을 스트리밍 반환하고, `POST /api/user/import`는 multipart 업로드를 받아 결과 JSON을 반환. 프론트엔드는 다운로드 트리거와 Import Dialog만 담당.

**Tech Stack:** Go + excelize/v2, React + TypeScript, Gin, GORM

---

## 파일 구조

| 파일 | 변경 |
|------|------|
| `go.mod` / `go.sum` | excelize 의존성 추가 |
| `service/user_export.go` | 신규 — Export/Import 비즈니스 로직 |
| `model/user.go` | `GetAllUsersForExport()` 함수 추가 |
| `controller/user.go` | `ExportUsers()`, `ImportUsers()` 핸들러 추가 |
| `router/api-router.go` | 두 라우트 등록 |
| `web/default/src/features/users/api.ts` | `exportUsers()`, `importUsers()` 추가 |
| `web/default/src/features/users/types.ts` | `ImportResult` 타입 추가 |
| `web/default/src/features/users/components/users-import-dialog.tsx` | 신규 — Import Dialog |
| `web/default/src/features/users/components/users-primary-buttons.tsx` | Import/Export 버튼 추가 |

---

## Task 1: excelize 의존성 추가

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: excelize 패키지 추가**

```bash
cd /home/molla/new-api
go get github.com/xuri/excelize/v2
```

Expected output: `go: added github.com/xuri/excelize/v2 vX.X.X`

- [ ] **Step 2: 빌드 확인**

```bash
go build ./...
```

Expected: 오류 없음

- [ ] **Step 3: 커밋**

```bash
git add go.mod go.sum
git commit -m "chore: add excelize dependency"
```

---

## Task 2: 모델 — GetAllUsersForExport

**Files:**
- Modify: `model/user.go`

- [ ] **Step 1: 테스트 작성**

`model/user_test.go` 파일이 있는지 확인:
```bash
ls /home/molla/new-api/model/user_test.go 2>/dev/null || echo "no test file"
```

`model/user_export_test.go` 파일 생성:

```go
package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAllUsersForExport(t *testing.T) {
	// This test requires a database connection.
	// Run with: go test ./model/ -run TestGetAllUsersForExport -v
	users, err := GetAllUsersForExport()
	require.NoError(t, err)
	assert.NotNil(t, users)
	// Password field must be empty (omitted)
	for _, u := range users {
		assert.Empty(t, u.Password, "password must not be exported")
		assert.Empty(t, u.AccessToken, "access token must not be exported")
	}
}
```

- [ ] **Step 2: `GetAllUsersForExport` 구현**

`model/user.go` 파일 하단(마지막 함수 다음)에 추가:

```go
// GetAllUsersForExport returns all non-deleted users for Excel export.
// Sensitive fields (Password, AccessToken) are omitted.
func GetAllUsersForExport() ([]*User, error) {
	var users []*User
	err := DB.Omit("password", "access_token").Order("id asc").Find(&users).Error
	return users, err
}
```

- [ ] **Step 3: 커밋**

```bash
git add model/user.go model/user_export_test.go
git commit -m "feat: add GetAllUsersForExport model function"
```

---

## Task 3: 서비스 — user_export.go

**Files:**
- Create: `service/user_export.go`

이 파일이 Export/Import의 핵심 로직을 담당한다.

- [ ] **Step 1: `service/user_export.go` 생성**

```go
package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

// ExcelHeaders defines the ordered column headers for user export/import.
var ExcelHeaders = []string{
	"id", "username", "display_name", "email",
	"role", "status", "group", "quota",
	"used_quota", "request_count", "remark",
	"aff_quota", "inviter_id", "initial_password", "created_at",
}

// roleToString converts integer role to human-readable string.
func roleToString(role int) string {
	switch role {
	case 100:
		return "root"
	case 10:
		return "admin"
	default:
		return "user"
	}
}

// stringToRole converts role string to integer. Defaults to 1 (user).
func stringToRole(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "root":
		return 100
	case "admin":
		return 10
	default:
		return 1
	}
}

// statusToString converts integer status to human-readable string.
func statusToString(status int) string {
	if status == 2 {
		return "disabled"
	}
	return "enabled"
}

// stringToStatus converts status string to integer. Defaults to 1 (enabled).
func stringToStatus(s string) int {
	if strings.ToLower(strings.TrimSpace(s)) == "disabled" {
		return 2
	}
	return 1
}

// BuildExportFile creates an Excel file from all users and returns it.
func BuildExportFile() (*excelize.File, error) {
	users, err := model.GetAllUsersForExport()
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	sheet := "Users"
	f.SetSheetName("Sheet1", sheet)

	// Write header row
	for col, header := range ExcelHeaders {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	// Write data rows
	for rowIdx, u := range users {
		row := rowIdx + 2 // 1-indexed, row 1 is header
		values := []interface{}{
			u.Id,
			u.Username,
			u.DisplayName,
			u.Email,
			roleToString(u.Role),
			statusToString(u.Status),
			u.Group,
			u.Quota,
			u.UsedQuota,
			u.RequestCount,
			u.Remark,
			u.AffQuota,
			u.InviterId,
			"", // initial_password: always empty on export
			u.CreatedAt,
		}
		for col, val := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			f.SetCellValue(sheet, cell, val)
		}
	}

	return f, nil
}

// ImportResult holds the outcome of a user import operation.
type ImportResult struct {
	Created          int      `json:"created"`
	Skipped          int      `json:"skipped"`
	SkippedUsernames []string `json:"skipped_usernames"`
	Errors           []string `json:"errors"`
}

// ImportUsersFromFile parses an Excel file and creates new users.
// Existing usernames are skipped. Returns a result summary.
func ImportUsersFromFile(f *excelize.File) (*ImportResult, error) {
	result := &ImportResult{
		SkippedUsernames: []string{},
		Errors:           []string{},
	}

	// Find the sheet — use first sheet regardless of name
	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read sheet: %w", err)
	}

	if len(rows) < 2 {
		// No data rows (only header or empty)
		return result, nil
	}

	// Build header index map from first row
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

	for rowNum, row := range rows[1:] { // skip header
		lineNum := rowNum + 2 // human-readable line number

		username := getCell(row, "username")
		if username == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: username is empty", lineNum))
			continue
		}

		password := getCell(row, "initial_password")
		if password == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): initial_password is empty", lineNum, username))
			continue
		}

		// Check for duplicate username
		exists, err := model.CheckUserExistOrDeleted(username, "")
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): db error: %v", lineNum, username, err))
			continue
		}
		if exists {
			result.Skipped++
			result.SkippedUsernames = append(result.SkippedUsernames, username)
			continue
		}

		// Parse quota
		quota := 0
		if qs := getCell(row, "quota"); qs != "" {
			if q, err := strconv.Atoi(qs); err == nil {
				quota = q
			}
		}

		// Parse aff_quota
		affQuota := 0
		if qs := getCell(row, "aff_quota"); qs != "" {
			if q, err := strconv.Atoi(qs); err == nil {
				affQuota = q
			}
		}

		// Parse inviter_id
		inviterId := 0
		if is := getCell(row, "inviter_id"); is != "" {
			if id, err := strconv.Atoi(is); err == nil {
				inviterId = id
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

		user := &model.User{
			Username:    username,
			Password:    password,
			DisplayName: displayName,
			Email:       getCell(row, "email"),
			Role:        stringToRole(getCell(row, "role")),
			Status:      stringToStatus(getCell(row, "status")),
			Group:       group,
			Quota:       quota,
			AffQuota:    affQuota,
			InviterId:   inviterId,
			Remark:      getCell(row, "remark"),
		}

		if err := user.Insert(inviterId); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
			continue
		}

		result.Created++
	}

	return result, nil
}
```

- [ ] **Step 2: 빌드 확인**

```bash
go build ./service/...
```

Expected: 오류 없음

- [ ] **Step 3: 커밋**

```bash
git add service/user_export.go
git commit -m "feat: add user export/import service"
```

---

## Task 4: 컨트롤러 핸들러 추가

**Files:**
- Modify: `controller/user.go`

- [ ] **Step 1: import 확인**

`controller/user.go` 상단 import 블록에 아래가 없으면 추가:

```go
import (
    // 기존 imports ...
    "fmt"
    "time"
    "github.com/QuantumNous/new-api/service"
    "github.com/xuri/excelize/v2"
)
```

`controller/user.go` 파일 하단에 아래 두 함수를 추가:

- [ ] **Step 2: `ExportUsers` 핸들러 추가**

```go
// ExportUsers streams an Excel file of all users to the client.
func ExportUsers(c *gin.Context) {
	f, err := service.BuildExportFile()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer f.Close()

	filename := fmt.Sprintf("users-%d.xlsx", time.Now().Unix())
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	if err := f.Write(c.Writer); err != nil {
		common.ApiError(c, err)
	}
}
```

- [ ] **Step 3: `ImportUsers` 핸들러 추가**

```go
// ImportUsers handles Excel file upload and bulk user creation.
func ImportUsers(c *gin.Context) {
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

	result, err := service.ImportUsersFromFile(f)
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

- [ ] **Step 4: 빌드 확인**

```bash
go build ./controller/...
```

Expected: 오류 없음

- [ ] **Step 5: 커밋**

```bash
git add controller/user.go
git commit -m "feat: add ExportUsers and ImportUsers handlers"
```

---

## Task 5: 라우터 등록

**Files:**
- Modify: `router/api-router.go`

- [ ] **Step 1: adminRoute 블록에 두 라우트 추가**

`router/api-router.go`에서 아래 라인을 찾아:

```go
adminRoute.DELETE("/:id/2fa", controller.AdminDisable2FA)
```

바로 다음에 추가:

```go
adminRoute.GET("/export", controller.ExportUsers)
adminRoute.POST("/import", controller.ImportUsers)
```

- [ ] **Step 2: 빌드 확인**

```bash
go build ./...
```

Expected: 오류 없음

- [ ] **Step 3: 수동 테스트 — Export**

서버를 실행하고 admin 토큰으로 테스트:

```bash
# 서버 실행 (별도 터미널)
go run main.go

# export 테스트 (admin 토큰으로)
curl -H "Authorization: Bearer <admin-token>" \
     http://localhost:3000/api/user/export \
     -o users.xlsx

file users.xlsx
```

Expected: `users.xlsx: Microsoft Excel 2007+`

- [ ] **Step 4: 커밋**

```bash
git add router/api-router.go
git commit -m "feat: register user export/import routes"
```

---

## Task 6: 프론트엔드 타입 및 API 함수

**Files:**
- Modify: `web/default/src/features/users/types.ts`
- Modify: `web/default/src/features/users/api.ts`

- [ ] **Step 1: `ImportResult` 타입을 `types.ts`에 추가**

`types.ts` 파일 하단에 추가:

```typescript
// ============================================================================
// Import / Export Types
// ============================================================================

export interface ImportResult {
  created: number
  skipped: number
  skipped_usernames: string[]
  errors: string[]
}
```

- [ ] **Step 2: `api.ts`에 `exportUsers`, `importUsers` 추가**

`api.ts` 파일 하단에 추가:

```typescript
/**
 * Export all users as an Excel file.
 * Triggers a file download in the browser.
 */
export async function exportUsers(): Promise<void> {
  const res = await api.get('/api/user/export', { responseType: 'blob' })
  const url = window.URL.createObjectURL(new Blob([res.data]))
  const link = document.createElement('a')
  link.href = url
  const timestamp = Math.floor(Date.now() / 1000)
  link.setAttribute('download', `users-${timestamp}.xlsx`)
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.URL.revokeObjectURL(url)
}

/**
 * Import users from an Excel file.
 */
export async function importUsers(file: File): Promise<ImportResult> {
  const formData = new FormData()
  formData.append('file', file)
  const res = await api.post('/api/user/import', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return res.data.data as ImportResult
}
```

- [ ] **Step 3: 커밋**

```bash
git add web/default/src/features/users/types.ts \
        web/default/src/features/users/api.ts
git commit -m "feat: add exportUsers and importUsers API functions"
```

---

## Task 7: Import Dialog 컴포넌트

**Files:**
- Create: `web/default/src/features/users/components/users-import-dialog.tsx`

- [ ] **Step 1: `users-import-dialog.tsx` 생성**

```typescript
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
import { importUsers } from '../api'
import type { ImportResult } from '../types'

interface UsersImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function UsersImportDialog({
  open,
  onOpenChange,
  onSuccess,
}: UsersImportDialogProps) {
  const { t } = useTranslation()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<ImportResult | null>(null)
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
      const res = await importUsers(file)
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
          <DialogTitle>{t('Import Users')}</DialogTitle>
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
              <p className='text-green-600'>
                ✓ {result.created}{t('users created')}
              </p>
              {result.skipped > 0 && (
                <p className='text-amber-600'>
                  ⚠ {result.skipped}{t('users skipped (already exist)')}: {result.skipped_usernames.join(', ')}
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

- [ ] **Step 2: 커밋**

```bash
git add web/default/src/features/users/components/users-import-dialog.tsx
git commit -m "feat: add UsersImportDialog component"
```

---

## Task 8: 버튼 및 페이지 연결

**Files:**
- Modify: `web/default/src/features/users/components/users-primary-buttons.tsx`

- [ ] **Step 1: `users-primary-buttons.tsx` 수정**

파일 전체를 아래로 교체:

```typescript
import { useState } from 'react'
import { Download, Plus, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { exportUsers } from '../api'
import { useUsers } from './users-provider'
import { UsersImportDialog } from './users-import-dialog'

export function UsersPrimaryButtons() {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow, refresh } = useUsers()
  const [exportLoading, setExportLoading] = useState(false)
  const [importOpen, setImportOpen] = useState(false)

  const handleCreate = () => {
    setCurrentRow(null)
    setOpen('create')
  }

  const handleExport = async () => {
    setExportLoading(true)
    try {
      await exportUsers()
    } finally {
      setExportLoading(false)
    }
  }

  return (
    <>
      <div className='flex gap-2'>
        <Button variant='outline' size='sm' onClick={() => setImportOpen(true)}>
          <Upload className='h-4 w-4' />
          {t('Import')}
        </Button>
        <Button
          variant='outline'
          size='sm'
          onClick={handleExport}
          disabled={exportLoading}
        >
          <Download className='h-4 w-4' />
          {exportLoading ? t('Exporting...') : t('Export')}
        </Button>
        <Button size='sm' onClick={handleCreate}>
          <Plus className='h-4 w-4' />
          {t('Add User')}
        </Button>
      </div>

      <UsersImportDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        onSuccess={refresh}
      />
    </>
  )
}
```

- [ ] **Step 2: `useUsers`에 `refresh` 함수 있는지 확인**

```bash
grep -n "refresh\|refetch" /home/molla/new-api/web/default/src/features/users/components/users-provider.tsx
```

`refresh`가 없으면 `users-provider.tsx`에서 내보내는 함수명을 확인하고 맞게 수정. 보통 `refetch` 또는 리스트 쿼리 무효화 함수를 사용.

- [ ] **Step 3: 빌드 확인**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | tail -20
```

Expected: 오류 없음

- [ ] **Step 4: 커밋**

```bash
git add web/default/src/features/users/components/users-primary-buttons.tsx
git commit -m "feat: add import/export buttons to users page"
```

---

## Task 9: 수동 E2E 검증

- [ ] **Step 1: 백엔드 서버 실행**

```bash
go run main.go
```

- [ ] **Step 2: 프론트엔드 개발 서버 실행**

```bash
cd web/default && bun run dev
```

- [ ] **Step 3: Export 테스트**

1. 관리자로 로그인
2. `/admin/users` 페이지 접속
3. `Export` 버튼 클릭
4. `users-{timestamp}.xlsx` 파일 다운로드 확인
5. Excel에서 파일 열어 헤더와 데이터 확인
6. `initial_password` 컬럼이 빈 값인지 확인

- [ ] **Step 4: Import 테스트**

1. 다운로드한 Excel 파일에서 새 사용자 행 추가:
   - username: `testimport1`
   - initial_password: `password123`
   - role: `user`
   - status: `enabled`
2. `Import` 버튼 클릭 → 파일 선택 → 업로드
3. 결과 확인: `✓ 1명 생성됨`
4. 같은 파일로 다시 Import → `⚠ 1명 건너뜀 (이미 존재): testimport1` 확인
5. `닫기` 클릭 후 목록 새로고침 확인

- [ ] **Step 5: 최종 커밋**

```bash
git add -A
git commit -m "feat: complete user export/import feature"
```
