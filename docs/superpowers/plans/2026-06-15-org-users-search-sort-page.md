# Org Users Search/Sort/Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add keyword search, column sorting, and page navigation to the organization users table.

**Architecture:** Backend `ListOrganizationUsers` gains `keyword`, `order_by`, `order_dir` query params; frontend adds search input, sortable column headers, pagination controls, and reactive state to drive the query.

**Tech Stack:** Go + GORM, Gin; React 19 + TypeScript, i18next

---

## File Map

| File | Change |
|------|--------|
| `controller/organization.go` | Add keyword/sort params to `ListOrganizationUsers` |
| `web/default/src/features/organizations/api.ts` | Extend `getOrganizationUsers` params |
| `web/default/src/features/organizations/components/organization-users-table.tsx` | Search input, sortable headers, pagination, reactive load |
| `web/default/src/i18n/locales/en.json` | New i18n keys |
| `web/default/src/i18n/locales/kr.json` | Korean translations |

---

### Task 1: Backend — add keyword search and sort to ListOrganizationUsers

**Files:**
- Modify: `controller/organization.go` (lines 510–535)

- [ ] **Step 1: Replace `ListOrganizationUsers` body**

Find the function starting at line 510. Replace the body with:

```go
func ListOrganizationUsers(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}

	pageInfo := common.GetPageQuery(c)
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

	query := model.DB.
		Where("organization_id = ?", organizationId).
		Where("role < ?", common.RoleAdminUser)

	if keyword != "" {
		query = query.Where("username LIKE ? OR display_name LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	if err := query.Model(&model.User{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var users []model.User
	if err := query.Order(orderBy + " " + orderDir).
		Offset(pageInfo.GetStartIdx()).
		Limit(pageInfo.GetPageSize()).
		Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}
```

Note: `strings` is already imported in `controller/organization.go`. Verify with `grep '"strings"' controller/organization.go` — if missing, add it to the import block.

- [ ] **Step 2: Build**

```bash
cd /home/molla/new-api && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add controller/organization.go
git commit -m "feat: org users list — add keyword search and column sort"
```

---

### Task 2: Frontend API — extend getOrganizationUsers params

**Files:**
- Modify: `web/default/src/features/organizations/api.ts` (lines 258–270)

- [ ] **Step 1: Replace `getOrganizationUsers`**

Replace the existing function:

```typescript
export async function getOrganizationUsers(params: {
  page?: number
  size?: number
  organization_id?: number | null
  keyword?: string
  order_by?: string
  order_dir?: 'asc' | 'desc'
}): Promise<ApiResponse<OrganizationUsersPage>> {
  const search = new URLSearchParams()
  if (params.page) search.set('p', String(params.page))
  if (params.size) search.set('page_size', String(params.size))
  appendOrganizationId(search, params.organization_id)
  if (params.keyword) search.set('keyword', params.keyword)
  if (params.order_by) search.set('order_by', params.order_by)
  if (params.order_dir) search.set('order_dir', params.order_dir)
  const suffix = search.toString() ? `?${search.toString()}` : ''
  const res = await api.get(`/api/organization/users${suffix}`)
  return res.data
}
```

- [ ] **Step 2: Commit**

```bash
git add web/default/src/features/organizations/api.ts
git commit -m "feat: getOrganizationUsers — add keyword, order_by, order_dir params"
```

---

### Task 3: Frontend table — search input, sortable headers, pagination

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-users-table.tsx`

This is the largest task. Make all changes to this one file.

- [ ] **Step 1: Add new state variables**

Find the existing `useState` declarations block (around line 60–140). Add these new states after the existing ones:

```typescript
const [currentPage, setCurrentPage] = useState(1)
const [totalUsers, setTotalUsers] = useState(0)
const [keyword, setKeyword] = useState('')
const [orderBy, setOrderBy] = useState('id')
const [orderDir, setOrderDir] = useState<'asc' | 'desc'>('asc')
const [keywordInput, setKeywordInput] = useState('')
```

- [ ] **Step 2: Add debounce effect for keyword**

After the existing `useEffect` blocks (around line 232–238), add a debounce effect:

```typescript
useEffect(() => {
  const timer = setTimeout(() => {
    setKeyword(keywordInput)
    setCurrentPage(1)
  }, 300)
  return () => clearTimeout(timer)
}, [keywordInput])
```

- [ ] **Step 3: Update `loadUsers` to use new state and save total**

Replace the existing `loadUsers` function:

```typescript
async function loadUsers() {
  if (!canManageOrganizationUsers) {
    setUsers([])
    setSubscriptionRecords([])
    return
  }
  setLoading(true)
  try {
    const [userRes, subscriptionRes] = await Promise.all([
      getOrganizationUsers({
        page: currentPage,
        size: 20,
        keyword: keyword || undefined,
        order_by: orderBy,
        order_dir: orderDir,
      }),
      getOrganizationUserSubscriptions(),
    ])
    if (userRes.success && userRes.data?.items) {
      setUsers(userRes.data.items)
      setTotalUsers(userRes.data.total)
    } else {
      toast.error(userRes.message || t('Failed to load organization users'))
    }
    if (subscriptionRes.success) {
      setSubscriptionRecords(subscriptionRes.data || [])
    } else {
      setSubscriptionRecords([])
    }
  } finally {
    setLoading(false)
  }
}
```

- [ ] **Step 4: Update useEffect to re-trigger on search/sort/page changes**

Replace the existing:
```typescript
useEffect(() => {
  void loadUsers()
}, [canManageOrganizationUsers])
```

With:
```typescript
useEffect(() => {
  void loadUsers()
}, [canManageOrganizationUsers, currentPage, keyword, orderBy, orderDir])
```

- [ ] **Step 5: Add sort handler helper**

Add a helper function before the `return` statement:

```typescript
function handleSort(col: string) {
  if (orderBy === col) {
    setOrderDir(d => d === 'asc' ? 'desc' : 'asc')
  } else {
    setOrderBy(col)
    setOrderDir('asc')
  }
  setCurrentPage(1)
}

function SortIcon({ col }: { col: string }) {
  if (orderBy !== col) return <span className='ml-1 text-muted-foreground opacity-40'>↕</span>
  return <span className='ml-1'>{orderDir === 'asc' ? '↑' : '↓'}</span>
}
```

- [ ] **Step 6: Add search input above the table**

Find the `<div className='overflow-x-auto rounded-md border'>` line (around line 737). Add a search input just before it:

```tsx
<div className='flex items-center gap-2'>
  <Input
    value={keywordInput}
    onChange={(e) => setKeywordInput(e.target.value)}
    placeholder={t('Search by username or display name')}
    className='max-w-xs'
  />
  {keywordInput && (
    <Button
      variant='ghost'
      size='sm'
      onClick={() => { setKeywordInput(''); setKeyword(''); setCurrentPage(1) }}
    >
      ✕
    </Button>
  )}
</div>
```

- [ ] **Step 7: Replace table header `<tr>` with sortable columns**

Replace the existing `<thead>` content:

```tsx
<thead className='bg-muted/50'>
  <tr>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('username')}
    >
      {t('Username')}<SortIcon col='username' />
    </th>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('display_name')}
    >
      {t('Display Name')}<SortIcon col='display_name' />
    </th>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('organization_role')}
    >
      {t('Role')}<SortIcon col='organization_role' />
    </th>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('status')}
    >
      {t('Status')}<SortIcon col='status' />
    </th>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('quota')}
    >
      {t('Quota')}<SortIcon col='quota' />
    </th>
    <th className='px-3 py-2 text-right font-medium'>
      {t('Actions')}
    </th>
  </tr>
</thead>
```

Note: the original header had 5 columns (Username, Role, Status, Quota, Actions). The new header adds Display Name as a separate sortable column. Check the tbody to see if display_name is currently inline under username — if so, keep it there in the cell but add the header column for sorting.

Actually: keep the original cell layout (display_name shown as subtitle under username) but add the sortable header for display_name as a separate column header mapped to the same column. Since username and display_name are in the same `<td>`, use 5 columns in the header matching the existing tbody structure: Username (sorts username), Role, Status, Quota, Actions. Add display_name sort under the Username header label.

Use this simpler approach — keep 5 columns, merge Username+DisplayName sort into the first header:

```tsx
<thead className='bg-muted/50'>
  <tr>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('username')}
    >
      {t('Username')}<SortIcon col='username' />
    </th>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('organization_role')}
    >
      {t('Role')}<SortIcon col='organization_role' />
    </th>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('status')}
    >
      {t('Status')}<SortIcon col='status' />
    </th>
    <th
      className='px-3 py-2 text-left font-medium cursor-pointer select-none'
      onClick={() => handleSort('quota')}
    >
      {t('Quota')}<SortIcon col='quota' />
    </th>
    <th className='px-3 py-2 text-right font-medium'>
      {t('Actions')}
    </th>
  </tr>
</thead>
```

- [ ] **Step 8: Add pagination controls below the table**

Find the closing `</div>` after `</table>` (the one that closes `overflow-x-auto rounded-md border`). After that closing div, add:

```tsx
{totalUsers > 20 && (
  <div className='flex items-center justify-between text-sm text-muted-foreground'>
    <span>
      {t('Page {{current}} of {{total}}', {
        current: currentPage,
        total: Math.ceil(totalUsers / 20),
      })}
      {' '}({t('{{count}} users total', { count: totalUsers })})
    </span>
    <div className='flex items-center gap-2'>
      <Button
        variant='outline'
        size='sm'
        onClick={() => setCurrentPage(p => p - 1)}
        disabled={currentPage <= 1 || loading}
      >
        {t('Previous')}
      </Button>
      <Button
        variant='outline'
        size='sm'
        onClick={() => setCurrentPage(p => p + 1)}
        disabled={currentPage >= Math.ceil(totalUsers / 20) || loading}
      >
        {t('Next')}
      </Button>
    </div>
  </div>
)}
```

- [ ] **Step 9: Commit**

```bash
git add web/default/src/features/organizations/components/organization-users-table.tsx
git commit -m "feat: org users table — search input, sortable headers, pagination"
```

---

### Task 4: i18n keys + build + deploy

**Files:**
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/kr.json`

- [ ] **Step 1: Add keys to en.json**

Append inside the `"translation"` object (before the final `}`):

```json
"Search by username or display name": "Search by username or display name",
"Page {{current}} of {{total}}": "Page {{current}} of {{total}}",
"{{count}} users total": "{{count}} users total",
"Previous": "Previous",
"Next": "Next"
```

Note: `"Previous"` and `"Next"` may already exist — grep first:
```bash
grep '"Previous"\|"Next"' web/default/src/i18n/locales/en.json
```
Only add if missing.

- [ ] **Step 2: Add Korean translations to kr.json**

Append the same keys with Korean values:

```json
"Search by username or display name": "사용자명 또는 표시 이름으로 검색",
"Page {{current}} of {{total}}": "{{total}}페이지 중 {{current}}페이지",
"{{count}} users total": "총 {{count}}명",
"Previous": "이전",
"Next": "다음"
```

Again, check if `"Previous"` and `"Next"` already exist in kr.json and skip if so.

- [ ] **Step 3: Build frontend**

```bash
cd /home/molla/new-api/web/default && bun run build 2>&1 | tail -3
```

Expected: no TypeScript or build errors.

- [ ] **Step 4: Build backend**

```bash
cd /home/molla/new-api && go build -o new-api .
```

Expected: binary produced.

- [ ] **Step 5: Deploy**

```bash
sudo systemctl stop new-api && sudo cp /home/molla/new-api/new-api /usr/local/bin/new-api && sudo systemctl start new-api
```

- [ ] **Step 6: Verify**

```bash
sudo systemctl status new-api | head -3
```

Expected: `active (running)`

- [ ] **Step 7: Commit**

```bash
git add web/default/src/i18n/locales/en.json web/default/src/i18n/locales/kr.json
git commit -m "feat: i18n keys for org users search/sort/pagination"
```
