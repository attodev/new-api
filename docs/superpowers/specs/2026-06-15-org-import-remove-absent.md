# Organization Import — Remove Absent Members 설계

**날짜:** 2026-06-15
**브랜치:** team
**범위:** 조직 사용자 import 시 파일에 없는 기존 멤버를 조직에서 제거하는 옵션 추가

---

## 개요

기존 import는 추가/생성만 지원한다. 이번 기능은 "파일에 없는 기존 멤버를 조직에서 제거"하는 sync 옵션을 추가한다. 사용자 계정 자체는 유지되고 `organization_id`, `organization_role` 필드만 초기화된다.

---

## 결정 사항

| 항목 | 결정 |
|------|------|
| 삭제 범위 | 조직 탈퇴만 (계정 유지) |
| 모드 선택 | 다이얼로그 체크박스, 기본값 비활성 |
| owner 보호 | 파일에 없어도 제거 안 함 |

---

## API 변경

### `POST /api/organization/users/import`

multipart form에 `remove_absent` 필드 추가 (값: `"true"` / 생략 또는 다른 값 = false)

**응답 `data` 필드 변경:**
```json
{
  "assigned": 3,
  "created": 2,
  "skipped": 1,
  "skipped_usernames": ["alice"],
  "removed": 2,
  "removed_usernames": ["bob", "carol"],
  "errors": []
}
```

---

## 백엔드 변경

### `service/org_user_export.go`

**`OrgImportResult` 필드 추가:**
```go
Removed          int      `json:"removed"`
RemovedUsernames []string `json:"removed_usernames"`
```

**`ImportOrgUsersFromFile` 시그니처 변경:**
```go
func ImportOrgUsersFromFile(f *excelize.File, organizationId int, removeAbsent bool) (*OrgImportResult, error)
```

`removeAbsent == true`일 때 처리 순서:
1. import 처리 중 성공적으로 처리된 username을 `presentUsernames` set에 수집 (assigned, created, skipped 모두 포함)
2. import 처리 완료 후, 현재 조직 멤버 전체 조회
3. organization owner (`organization_role = "owner"`)는 제외
4. `presentUsernames`에 없는 멤버의 `organization_id`, `organization_role`을 `.Select(...).Updates(...)` 로 초기화
5. 제거된 사용자들을 `result.Removed`, `result.RemovedUsernames`에 기록
6. 캐시 무효화

### `controller/organization.go`

`ImportOrganizationUsers` 핸들러에서 form 필드 파싱:
```go
removeAbsent := c.PostForm("remove_absent") == "true"
result, err := service.ImportOrgUsersFromFile(f, orgId, removeAbsent)
```

---

## 프론트엔드 변경

### `web/default/src/features/organizations/types.ts`

`OrgImportResult`에 필드 추가:
```typescript
removed: number
removed_usernames: string[]
```

### `web/default/src/features/organizations/api.ts`

`importOrgUsers` 함수에 `removeAbsent?: boolean` 파라미터 추가:
```typescript
if (removeAbsent) formData.append('remove_absent', 'true')
```

### `web/default/src/features/organizations/components/org-users-import-dialog.tsx`

- 파일 선택 아래 체크박스 추가
- 레이블: "파일에 없는 멤버 조직에서 제거" (i18n 키 동일)
- 기본값: `false`
- 결과 표시: removed > 0이면 "N명 제거됨" 항목 표시 (removed_usernames 펼쳐보기)

---

## 범위 외

- 계정 삭제: 제외
- owner 강제 제거: 제외
- 부분 롤백: 오류 발생 시 이미 처리된 항목은 유지
