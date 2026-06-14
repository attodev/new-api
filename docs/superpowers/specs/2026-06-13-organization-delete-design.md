# Organization Delete 설계

**날짜:** 2026-06-13
**브랜치:** team
**범위:** 관리자 화면에서 기존 조직을 삭제하는 기능 추가

---

## 개요

현재 조직 생성/수정은 가능하지만 삭제 방법이 없다. Root 관리자가 조직을 삭제할 수 있는 기능을 추가한다.

---

## 결정 사항

| 항목 | 결정 | 이유 |
|------|------|------|
| 멤버 처리 | 멤버 있으면 삭제 차단 | 실수로 멤버 데이터 손실 방지 |
| 구독 데이터 | 함께 삭제 | 고아 데이터 방지 |
| 삭제 방식 | 하드 삭제 (트랜잭션) | 소프트 삭제 불필요, 일관성 보장 |
| 인증 | RootAuth | 기존 조직 생성/수정과 동일 |

---

## API

### `DELETE /api/organizations/:id`

- **인증:** RootAuth 미들웨어
- **성공 응답:** `200 { "success": true }`
- **오류 응답:**

| 상태 코드 | 조건 | 메시지 |
|-----------|------|--------|
| 400 | 소속 멤버 존재 | `"organization has N members, remove them first"` |
| 404 | 조직 없음 | 표준 not found |
| 500 | DB 오류 | 표준 서버 오류 |

---

## 백엔드 구현

### 파일

```
model/organization.go      — DeleteOrganization(id int) error 추가
controller/organization.go — DeleteOrganization(c *gin.Context) 핸들러 추가
router/api-router.go       — DELETE /api/organizations/:id 등록
```

### 삭제 트랜잭션 흐름 (`model/organization.go`)

```
tx := DB.Begin()

1. SELECT COUNT(*) FROM users
   WHERE organization_id = id AND deleted_at IS NULL
   → count > 0: tx.Rollback(), return error("organization has N members, remove them first")

2. DELETE FROM organization_user_subscriptions WHERE organization_id = id
3. DELETE FROM organization_subscription_plans WHERE organization_id = id
4. DELETE FROM organizations WHERE id = id
   → rows affected == 0: tx.Rollback(), return ErrRecordNotFound

tx.Commit()
```

---

## 프론트엔드 구현

### 파일

```
web/default/src/features/organizations/api.ts
  — deleteOrganization(id: number) 추가

web/default/src/features/organizations/components/organization-users-table.tsx
  — 조직 목록 행에 삭제 버튼 + 확인 Dialog 추가
```

### 삭제 플로우

1. 삭제 버튼 클릭 → 확인 Dialog 표시
   - `"조직 '[이름]'을 삭제하시겠습니까? 이 작업은 되돌릴 수 없습니다."`
2. 확인 → `DELETE /api/organizations/:id` 호출
3. 성공 → toast + 목록 새로고침
4. 실패(멤버 있음 등) → 오류 메시지 toast 표시

### 표시 조건

- 삭제 버튼은 Root 권한을 가진 사용자에게만 표시 (기존 생성/수정 버튼과 동일 조건)

---

## 범위 외

- 소프트 삭제(비활성화): 제외
- 멤버 자동 해제 후 삭제: 제외 (명시적으로 먼저 제거해야 함)
- 삭제 로그/감사 기록: 제외
