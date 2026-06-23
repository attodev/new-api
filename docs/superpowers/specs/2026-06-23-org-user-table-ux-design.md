# 조직 사용자 테이블 UX 개선 설계

## 목표

조직 관리 화면의 사용자 테이블에 다음 기능을 추가한다:

1. 조직에서 사용자 제거 기능 (백엔드 API + 프론트엔드)
2. 즉시 반영 방식 → 저장/취소 패턴으로 전환
3. 체크박스 다중 선택 + 일괄 작업 (비활성화, 조직 제거)

---

## 백엔드

### 새 API: `DELETE /organization/users/:id/membership`

- **인증**: 조직 admin 이상 (기존 membership PUT과 동일)
- **동작**: 해당 user의 `organization_id = 0`, `organization_role = ""` 업데이트
- **제약**: owner는 제거 불가 (403 반환)
- **응답**: `{"message": "ok"}`

### 라우팅

[`router/organization.go`] — 기존 `PUT /organization/users/:id/membership` 옆에 추가:

```
organizationUserGroup.DELETE("/users/:id/membership", controller.RemoveOrganizationUserMembership)
```

### 컨트롤러

[`controller/organization.go`] — 새 핸들러 `RemoveOrganizationUserMembership`:

- URL param `id` 파싱 (invalid → 400)
- 대상 유저 조회 (not found → 404)
- 대상 유저가 같은 조직인지 확인 (다른 조직 → 403)
- 대상 유저가 owner인지 확인 (owner → 403)
- `service.RemoveOrganizationUserMembership(userId)` 호출

### 서비스

[`service/organization.go`] — 새 함수 `RemoveOrganizationUserMembership(userId int) error`:

- `model.UpdateUser(userId, map[string]any{"organization_id": 0, "organization_role": ""})`

---

## 프론트엔드

### Pending State 모델

모든 변경은 클라이언트 상태(`pendingChanges`)에만 저장되다가 "저장" 클릭 시 일괄 API 호출.

```typescript
type PendingChange =
  | { type: 'quota'; userId: number; newQuota: number; originalQuota: number }
  | { type: 'status'; userId: number; newStatus: number }  // 0=비활성, 1=활성
  | { type: 'remove'; userId: number }
```

### 저장/취소 배너

변경이 1건 이상일 때만 테이블 상단에 표시:

```
📝 quota 1건 수정 · 1명 비활성화 예정 · 1명 제거 예정   [저장]  [모두 취소]
```

저장 시: 각 pending change 타입별 API 병렬 호출 → 완료 후 목록 재로드.  
취소 시: `pendingChanges` 초기화.

### 체크박스 + 일괄 작업 툴바

- 각 행 맨 앞에 체크박스 — owner 행은 `disabled`
- 헤더 체크박스: 전체 선택/해제 (owner 제외)
- 1명 이상 선택 시 검색 우측에 툴바 표시:
  ```
  2명 선택됨  |  [🚫 비활성화]  [✕ 조직에서 제거]
  ```

일괄 작업은 pending 상태로만 추가 (즉시 API 호출 안 함).

### Pending 표시 (라벨만, 배경색 없음)

각 행의 이름 아래 소형 컬러 라벨:

| 상태 | 라벨 | 스타일 |
|---|---|---|
| quota 수정됨 | `● quota 수정됨` | `text-amber-700` |
| 비활성화 예정 | `● 비활성화 예정` | `text-orange-600` |
| 제거 예정 | `● 제거 예정` | `text-red-600` + 이름에 취소선 |

배경색 변경 없음.

### Quota 셀 (인라인 편집 유지)

기존과 동일한 Input 필드 유지. 변경 감지 방식만 onBlur 즉시저장 → pending 상태로 변경:

```
[  8.00  ] USD  (기존: $3.00)   ← 변경 시 주황 테두리 + 기존값 표시
[$1] [$5] [$10]
```

제거 예정인 사용자의 Quota 인풋은 `disabled`.

### 행별 버튼 없음

기존의 행별 "비활성화", "제거" 버튼 추가 안 함. 모든 상태 변경은 체크박스 선택 후 상단 툴바로만 수행.

---

## 변경 파일 목록

| 파일 | 작업 |
|---|---|
| `router/organization.go` | DELETE membership 라우트 추가 |
| `controller/organization.go` | `RemoveOrganizationUserMembership` 핸들러 추가 |
| `service/organization.go` | `RemoveOrganizationUserMembership` 서비스 추가 (또는 기존 파일) |
| `controller/organization_test.go` | 새 핸들러 테스트 추가 |
| `web/default/src/features/organizations/components/organization-users-table.tsx` | 전면 리팩토링 (pending state, 체크박스, 툴바, 배너) |
| `web/default/src/features/organizations/api.ts` | `removeOrganizationUserMembership` API 함수 추가 |

---

## 제약 사항

- Owner는 비활성화/제거 불가 (프론트: disabled, 백: 403)
- 저장 도중 일부 실패 시 성공한 건은 반영, 실패한 건은 에러 토스트 표시
- 기존 페이지네이션/검색/정렬은 그대로 유지
