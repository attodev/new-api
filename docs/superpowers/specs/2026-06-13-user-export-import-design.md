# User Export / Import 설계

**날짜:** 2026-06-13  
**브랜치:** team  
**범위:** 관리자 사용자 관리 화면에 Excel export/import 기능 추가

---

## 개요

관리자가 사용자 목록을 Excel 파일로 내보내고, Excel로 정리된 사용자를 일괄 등록할 수 있는 기능을 추가한다.

---

## 결정 사항

| 항목 | 결정 | 이유 |
|------|------|------|
| 처리 위치 | 백엔드 (Go) | 보안, 성능, 유지보수 우위 |
| Excel 라이브러리 | `github.com/xuri/excelize/v2` | Go 생태계 표준 |
| Export 범위 | 전체 관리 정보 (민감 필드 제외) | 백업/마이그레이션 목적 |
| 조직 필드 | 제외 | 사용자 관리 화면에 조직 정보 미표시 |
| 중복 처리 | Skip (건너뛰기) | import 후 결과 화면에 목록 표시 |
| 신규 비밀번호 | `initial_password` 컬럼 | export 시 빈 값, import 시 필수 입력 |
| UI 배치 | 페이지 헤더 우측 | Export / Import 버튼 |

---

## API

### `GET /api/user/export`

- **인증:** AdminAuth 미들웨어
- **응답:** `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`
- **헤더:** `Content-Disposition: attachment; filename="users-{timestamp}.xlsx"`
- **동작:** 전체 사용자(soft-deleted 제외)를 Excel 파일로 스트리밍 반환

### `POST /api/user/import`

- **인증:** AdminAuth 미들웨어
- **요청:** `multipart/form-data`, 파일 키 `file`, 최대 10MB, `.xlsx`만 허용
- **응답:**
  ```json
  {
    "created": 42,
    "skipped": 3,
    "skipped_usernames": ["alice", "bob", "charlie"],
    "errors": []
  }
  ```

---

## Excel 컬럼 명세

| 컬럼명 | 모델 필드 | import 처리 |
|--------|-----------|-------------|
| `id` | Id | 무시 (읽기 전용) |
| `username` | Username | 중복 체크 키, 필수 |
| `display_name` | DisplayName | |
| `email` | Email | |
| `role` | Role | `user`/`admin`/`root` 문자열 → 1/10/100 변환 |
| `status` | Status | `enabled`/`disabled` 문자열 → 1/2 변환 |
| `group` | Group | 없으면 `default` 사용 |
| `quota` | Quota | |
| `used_quota` | UsedQuota | 무시 (읽기 전용) |
| `request_count` | RequestCount | 무시 (읽기 전용) |
| `remark` | Remark | |
| `aff_quota` | AffQuota | |
| `inviter_id` | InviterId | |
| `initial_password` | — | export 시 빈 값; import 시 신규 계정 비밀번호, 필수 |
| `created_at` | CreatedAt | 무시 (읽기 전용) |

---

## 백엔드 구현

### 파일 구조

```
controller/user.go          — ExportUsers(), ImportUsers() 핸들러 추가
service/user_export.go      — Export/Import 비즈니스 로직 (신규 파일)
model/user.go               — GetAllUsersForExport() 쿼리 추가
router/api-router.go        — 두 라우트 등록
```

### Export 흐름

1. `GetAllUsersForExport()` — DB에서 전체 사용자 조회 (soft-deleted 제외)
2. excelize로 시트 생성, 헤더 행 → 데이터 행 순서로 작성
3. `initial_password` 컬럼은 빈 값으로 포함 (template 역할)
4. `Content-Disposition` 헤더와 함께 스트리밍 반환

### Import 흐름

1. multipart form에서 `.xlsx` 파일 수신, 10MB 초과 시 400 오류
2. excelize로 파싱, 1행(헤더) 건너뜀
3. 각 행 처리:
   - `username` 빈 값이면 해당 행 오류로 기록 후 건너뜀
   - `GetUserByUsername()`으로 중복 확인 → 중복이면 `skipped_usernames`에 추가
   - `initial_password` 없으면 해당 행 오류로 기록 후 건너뜀
   - role/status 문자열 → 숫자 변환 (알 수 없는 값은 기본값: role=1, status=1)
   - `CreateUser()` 호출
4. 결과 반환

---

## 프론트엔드 구현

### UI 배치

`web/default/src/features/users/index.tsx` 페이지 헤더 우측에 버튼 추가:

```
[사용자 목록]                              [Import] [Export]
────────────────────────────────────────────────────────────
| 검색 | 필터 |                                          ... |
```

### Export

- `Export` 버튼 클릭 → `GET /api/user/export` 호출
- 응답 Blob을 `<a>` 태그로 브라우저 다운로드 트리거
- 요청 중 버튼 비활성화, 완료 후 재활성화

### Import Dialog

- `Import` 버튼 클릭 → Dialog 오픈
- Dialog 내용:
  1. `.xlsx` 파일 선택 input
  2. `업로드` 버튼 → `POST /api/user/import` 전송
  3. 완료 후 결과 표시:
     - `✓ N명 생성됨`
     - `⚠ N명 건너뜀 (이미 존재): username1, username2, ...`
     - 오류가 있으면 오류 행 목록 표시
  4. `닫기` 클릭 시 사용자 목록 자동 새로고침

### 신규 파일

```
web/default/src/features/users/components/users-import-dialog.tsx
web/default/src/features/users/api.ts  — exportUsers(), importUsers() 추가
```

---

## 범위 외

- 조직(Organization) 필드 export/import: 제외 (별도 기능으로 처리)
- Export 필터링 (특정 그룹/역할만): 제외 (전체 export만 지원)
- Import 시 기존 사용자 업데이트(upsert): 제외 (skip만 지원)
