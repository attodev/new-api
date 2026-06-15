# Organization Import — Update Existing + Remove Absent 설계

**날짜:** 2026-06-15
**브랜치:** team
**범위:** 조직 사용자 import 시 기존 멤버 필드 업데이트 + 파일에 없는 멤버 제거 옵션 추가

---

## 개요

기존 import는 이미 조직에 속한 사용자를 skipped 처리했다. 이번 변경으로:
1. 기존 조직 멤버도 파일 값으로 필드 업데이트 (항상 적용)
2. 파일에 없는 기존 멤버를 조직에서 제거하는 sync 옵션 추가 (체크박스, 기본 비활성)

사용자 계정 자체는 삭제하지 않는다. 조직 관련 필드(및 기타 필드)만 갱신/초기화한다.

---

## 결정 사항

| 항목 | 결정 |
|------|------|
| 기존 멤버 처리 | skipped → 필드 업데이트 (항상) |
| 업데이트 대상 필드 | display_name, organization_role, quota, group, status, remark |
| 제거 모드 | 다이얼로그 체크박스, 기본값 비활성 |
| owner 보호 | 파일에 없어도 제거 안 함 |
| 부분 롤백 | 없음 — 오류 행은 건너뛰고 나머지 계속 처리 |

---

## 업데이트 필드 상세

기존 조직 멤버를 import할 때 다음 필드를 덮어씀:

| 필드 | 비고 |
|------|------|
| display_name | 빈칸이면 기존 값 유지 |
| organization_role | 빈칸이면 기존 값 유지 |
| quota | 빈칸이면 기존 값 유지; 변경 시 IncreaseUserQuota/DecreaseUserQuota 사용 |
| group | 빈칸이면 기존 값 유지 |
| status | 빈칸이면 기존 값 유지 |
| remark | 빈칸이어도 덮어씀 (명시적 삭제 허용) |

`used_quota`는 읽기 전용이므로 무시.

---

## 결과 구조 변경

`skipped`/`skipped_usernames`는 제거하고 `updated`로 교체.

```json
{
  "assigned": 2,
  "created": 1,
  "updated": 3,
  "updated_usernames": ["alice", "bob", "carol"],
  "removed": 1,
  "removed_usernames": ["dave"],
  "errors": []
}
```

---

## API 변경

### `POST /api/organization/users/import`

multipart form에 `remove_absent` 필드 추가 (값: `"true"` / 생략 또는 다른 값 = false)

---

## 백엔드 변경

### `service/org_user_export.go`

**`OrgImportResult` 변경:**
```go
type OrgImportResult struct {
    Assigned         int      `json:"assigned"`
    Created          int      `json:"created"`
    Updated          int      `json:"updated"`
    UpdatedUsernames []string `json:"updated_usernames"`
    Removed          int      `json:"removed"`
    RemovedUsernames []string `json:"removed_usernames"`
    Errors           []string `json:"errors"`
}
```

**`ImportOrgUsersFromFile` 시그니처 변경:**
```go
func ImportOrgUsersFromFile(f *excelize.File, organizationId int, removeAbsent bool) (*OrgImportResult, error)
```

**기존 멤버 처리 (organizationId 일치):**
1. display_name, organization_role, group, status, remark, quota를 파일 값으로 업데이트
2. 빈 문자열 필드는 해당 필드 업데이트 건너뜀 (remark 제외)
3. quota 변경 시 현재 quota와 비교해 delta만큼 Increase/Decrease
4. `presentUsernames` set에 username 추가
5. result.Updated++ / result.UpdatedUsernames append

**removeAbsent 처리 (기존과 동일):**
1. import 완료 후 현재 조직 멤버 전체 조회
2. owner 제외
3. `presentUsernames`에 없는 멤버 조직 탈퇴
4. result.Removed, result.RemovedUsernames 기록
5. 캐시 무효화

### `controller/organization.go`

```go
removeAbsent := c.PostForm("remove_absent") == "true"
result, err := service.ImportOrgUsersFromFile(f, orgId, removeAbsent)
```

---

## 프론트엔드 변경

### `web/default/src/features/organizations/types.ts`

```typescript
export interface OrgImportResult {
  assigned: number
  created: number
  updated: number
  updated_usernames: string[]
  removed: number
  removed_usernames: string[]
  errors: string[]
}
```

### `web/default/src/features/organizations/api.ts`

`importOrgUsers(file, organizationId?, removeAbsent?)` 파라미터 추가:
```typescript
if (removeAbsent) formData.append('remove_absent', 'true')
```

### `web/default/src/features/organizations/components/org-users-import-dialog.tsx`

- 체크박스: "파일에 없는 멤버 조직에서 제거" (기본값: false)
- 결과 표시:
  - assigned N명 배정
  - created N명 생성
  - updated N명 업데이트
  - removed N명 제거 (removed > 0일 때만)
  - errors가 있으면 목록 표시

---

## 범위 외

- 계정 삭제: 제외
- owner 강제 제거: 제외
- used_quota 변경: 제외
