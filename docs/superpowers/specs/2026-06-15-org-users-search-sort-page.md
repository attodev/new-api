# Organization Users — Search, Sort, Pagination 설계

**날짜:** 2026-06-15
**브랜치:** team
**범위:** 조직 사용자 목록에 keyword 검색, 컬럼 정렬, 페이지 네비게이션 추가

---

## 현재 상태

- 백엔드 `ListOrganizationUsers`: pagination 파라미터(`p`, `page_size`) 지원, keyword/sort 미지원
- 프론트엔드: `page: 1, size: 20` 하드코딩, 검색/정렬/페이징 UI 없음

---

## 백엔드 변경

### `controller/organization.go` — `ListOrganizationUsers`

추가 쿼리 파라미터:

| 파라미터 | 타입 | 설명 |
|---------|------|------|
| `keyword` | string | username 또는 display_name LIKE 검색 (빈 문자열이면 무시) |
| `order_by` | string | 정렬 컬럼 (화이트리스트) |
| `order_dir` | string | `asc` 또는 `desc` (기본 `asc`) |

**허용 order_by 값 (화이트리스트):**
`username`, `display_name`, `organization_role`, `quota`, `used_quota`, `group`, `status`

화이트리스트에 없는 값은 무시 (기본 정렬 `id asc` 유지).

**구현 패턴:**
```go
keyword := strings.TrimSpace(c.Query("keyword"))
orderBy := c.Query("order_by")
orderDir := c.Query("order_dir")

allowedOrderBy := map[string]bool{
    "username": true, "display_name": true, "organization_role": true,
    "quota": true, "used_quota": true, "group": true, "status": true,
}
if !allowedOrderBy[orderBy] {
    orderBy = "id"
}
if orderDir != "desc" {
    orderDir = "asc"
}

if keyword != "" {
    query = query.Where("username LIKE ? OR display_name LIKE ?",
        "%"+keyword+"%", "%"+keyword+"%")
}
query = query.Order(orderBy + " " + orderDir)
```

---

## 프론트엔드 변경

### `api.ts` — `getOrganizationUsers`

```typescript
export async function getOrganizationUsers(params: {
  page?: number
  size?: number
  organization_id?: number | null
  keyword?: string
  order_by?: string
  order_dir?: 'asc' | 'desc'
}): Promise<ApiResponse<OrganizationUsersPage>>
```

### `organization-users-table.tsx`

**상태 추가:**
```typescript
const [currentPage, setCurrentPage] = useState(1)
const [keyword, setKeyword] = useState('')
const [orderBy, setOrderBy] = useState('id')
const [orderDir, setOrderDir] = useState<'asc' | 'desc'>('asc')
const [total, setTotal] = useState(0)
```

**검색:**
- 테이블 상단에 검색 input
- 300ms debounce
- keyword 변경 시 currentPage → 1로 리셋

**정렬:**
- 컬럼 헤더 클릭 시 정렬 토글 (같은 컬럼: asc→desc→asc, 다른 컬럼: 해당 컬럼 asc)
- 현재 정렬 컬럼/방향 시각적 표시 (▲▼ 아이콘)

**페이지 네비게이션:**
- 테이블 하단에 이전/다음 버튼 + "N / M 페이지" 표시
- page size: 20 고정
- 현재 페이지가 첫/마지막이면 버튼 비활성

**데이터 로드 트리거:**
`currentPage`, `keyword`, `orderBy`, `orderDir` 변경 시 재조회

---

## 범위 외

- page size 변경 UI: 제외
- 고급 필터 (role, status): 제외
- URL 파라미터 동기화: 제외
