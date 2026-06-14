# Organization User Export/Import 설계

**날짜:** 2026-06-14
**브랜치:** team
**범위:** 조직 소유자/관리자가 조직 멤버를 Excel로 export하고, Excel로 import(기존 사용자 배정 + 신규 사용자 생성)하는 기능

---

## 개요

관리자 화면의 사용자 export/import와 별도로, 조직 소유자가 자신의 조직 멤버를 Excel로 관리할 수 있는 기능을 추가한다. 조직 컨텍스트에 맞는 컬럼 구성을 사용하며, import 시 기존 사용자는 조직에 배정하고 신규 사용자는 생성 후 배정한다.

---

## 결정 사항

| 항목 | 결정 | 이유 |
|------|------|------|
| 서비스 파일 | 새 `service/org_user_export.go` | admin export와 컬럼/로직이 달라 별도 분리 |
| 권한 | org admin/owner | 기존 `resolveOrganizationAdminTarget` 패턴 |
| import 충돌 | 다른 조직 소속이면 error | 다른 조직 멤버를 무단 이동 방지 |
| email 컬럼 | 제외 | 조직 화면 범위 밖, 개인정보 |
| global role 컬럼 | 제외 | 조직 소유자가 변경 불가한 필드 |

---

## Export 컬럼

순서대로:

| # | 컬럼 | 설명 |
|---|------|------|
| 1 | username | 사용자명 |
| 2 | display_name | 표시 이름 |
| 3 | organization_role | 조직 내 역할 (member/admin/owner) |
| 4 | quota | 할당 쿼터 |
| 5 | used_quota | 사용된 쿼터 (읽기 전용) |
| 6 | group | 그룹 |
| 7 | status | enabled / disabled |
| 8 | remark | 비고 |
| 9 | initial_password | 항상 빈칸 (export 시) |

---

## Import 처리 로직

각 행에 대해 순서대로 처리:

| 상황 | 처리 |
|------|------|
| username 빈칸 | error, skip |
| 이미 이 조직 멤버 | skipped 카운트 + username 기록 |
| 다른 조직 소속 | error (다른 조직 멤버는 배정 불가) |
| 존재하고 조직 없음 | 이 조직에 배정 (organization_id, organization_role 업데이트) |
| 존재하지 않음 | initial_password 없으면 error; 신규 유저 생성 후 이 조직에 배정 |

- `organization_role` 기본값: `member`
- `group` 기본값: `default`
- `display_name` 기본값: username
- 최대 1,000행

**결과 구조:**
```json
{
  "assigned": 3,
  "created": 2,
  "skipped": 1,
  "skipped_usernames": ["alice"],
  "errors": ["line 5 (bob): belongs to another organization"]
}
```

---

## API

### `GET /api/organization/users/export`
- **권한:** org admin/owner (UserAuth 미들웨어 + resolveOrganizationAdminTarget)
- **응답:** `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`
- **파일명:** `org-users-{orgId}-{timestamp}.xlsx`

### `POST /api/organization/users/import`
- **권한:** org admin/owner
- **요청:** multipart/form-data, 파일 필드명 `file`
- **제한:** 10MB
- **응답:** `{ "success": true, "data": { assigned, created, skipped, skipped_usernames, errors } }`

---

## 백엔드 구현

### 파일

```
service/org_user_export.go     — 새 파일: OrgExcelHeaders, BuildOrgExportFile(), ImportOrgUsersFromFile()
controller/organization.go     — ExportOrganizationUsers(), ImportOrganizationUsers() 핸들러 추가
router/api-router.go           — GET/POST /api/organization/users/export|import 등록
```

### `service/org_user_export.go`

```go
var OrgExcelHeaders = []string{
    "username", "display_name", "organization_role",
    "quota", "used_quota", "group", "status", "remark", "initial_password",
}

func BuildOrgExportFile(organizationId int) (*excelize.File, error)
// Queries users WHERE organization_id = organizationId AND role < RoleAdminUser

type OrgImportResult struct {
    Assigned         int      `json:"assigned"`
    Created          int      `json:"created"`
    Skipped          int      `json:"skipped"`
    SkippedUsernames []string `json:"skipped_usernames"`
    Errors           []string `json:"errors"`
}

func ImportOrgUsersFromFile(f *excelize.File, organizationId int) (*OrgImportResult, error)
```

### `controller/organization.go` 핸들러

```go
func ExportOrganizationUsers(c *gin.Context)
// resolveOrganizationAdminTarget → BuildOrgExportFile → stream response

func ImportOrganizationUsers(c *gin.Context)
// resolveOrganizationAdminTarget → parse multipart → ImportOrgUsersFromFile → ApiSuccess
```

### `router/api-router.go`

`organizationRoute` 블록에 추가:
```go
organizationRoute.GET("/users/export", controller.ExportOrganizationUsers)
organizationRoute.POST("/users/import", controller.ImportOrganizationUsers)
```

---

## 프론트엔드 구현

### 파일

```
web/default/src/features/organizations/api.ts
  — exportOrgUsers(), importOrgUsers(file) 추가

web/default/src/features/organizations/types.ts
  — OrgImportResult 인터페이스 추가

web/default/src/features/organizations/components/organization-users-table.tsx
  — Export 버튼, Import 버튼 + OrgUsersImportDialog 추가 (isOrganizationOwner 조건)
```

### `OrgImportResult` 타입

```typescript
export interface OrgImportResult {
  assigned: number
  created: number
  skipped: number
  skipped_usernames: string[]
  errors: string[]
}
```

### UI

- Export/Import 버튼은 `isOrganizationOwner` 일 때만 표시
- Import 결과: assigned N명 배정, created N명 생성, skipped N명 건너뜀 형태로 표시
- 기존 `UsersImportDialog`와 동일한 UX 패턴 (파일 선택 → 업로드 → 결과 표시)

---

## 범위 외

- 조직 멤버의 비밀번호 변경: 제외
- email 컬럼: 제외
- global role(root/admin) 변경: 제외
- 다른 조직의 멤버를 강제 이동: 제외
