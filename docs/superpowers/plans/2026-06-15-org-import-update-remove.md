# Org Import Update-Existing + Remove-Absent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enhance org user import so existing members are updated (not skipped), and add an optional "remove absent" mode that clears org membership for members not in the file.

**Architecture:** Two changes to `service/org_user_export.go`: (1) replace the "same org → skip" branch with a field-update branch, (2) add a `removeAbsent bool` parameter that, when true, removes members absent from the file after processing all rows. Controller passes `remove_absent` form field. Frontend adds a checkbox and shows updated/removed counts.

**Tech Stack:** Go + GORM, excelize/v2, React 19 + TypeScript, i18next

---

### Task 1: Update backend service — update existing members + remove absent

**Files:**
- Modify: `service/org_user_export.go`

- [ ] **Step 1: Replace `OrgImportResult` struct**

In `service/org_user_export.go`, replace the existing `OrgImportResult` struct with:

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

- [ ] **Step 2: Update `ImportOrgUsersFromFile` signature**

Change the function signature to:

```go
func ImportOrgUsersFromFile(f *excelize.File, organizationId int, removeAbsent bool) (*OrgImportResult, error) {
```

And update result initialization:

```go
result := &OrgImportResult{
    UpdatedUsernames: []string{},
    RemovedUsernames: []string{},
    Errors:           []string{},
}
```

Also add a set to track usernames present in the file (needed for removeAbsent):

```go
presentUsernames := map[string]bool{}
```

- [ ] **Step 3: Replace the "same org → skip" branch with update logic**

Find the block:
```go
if existingUser.OrganizationId == organizationId {
    result.Skipped++
    result.SkippedUsernames = append(result.SkippedUsernames, username)
    continue
}
```

Replace with:
```go
if existingUser.OrganizationId == organizationId {
    // Update fields for existing member
    updates := map[string]interface{}{}

    if dn := getCell(row, "display_name"); dn != "" {
        updates["display_name"] = dn
    }
    if role := orgRole; role != "" && role != existingUser.OrganizationRole {
        updates["organization_role"] = role
    }
    if grp := getCell(row, "group"); grp != "" {
        updates["group"] = grp
    }
    if st := getCell(row, "status"); st != "" {
        updates["status"] = stringToStatus(st)
    }
    // remark: always overwrite (explicit clear allowed)
    updates["remark"] = getCell(row, "remark")

    if len(updates) > 0 {
        if err := model.DB.Model(&model.User{}).Where("id = ?", existingUser.Id).Updates(updates).Error; err != nil {
            result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to update: %v", lineNum, username, err))
            presentUsernames[username] = true
            continue
        }
    }

    // Handle quota change
    if qs := getCell(row, "quota"); qs != "" {
        if newQuota, err := strconv.Atoi(qs); err == nil {
            delta := newQuota - int(existingUser.Quota)
            if delta > 0 {
                _ = model.IncreaseUserQuota(existingUser.Id, delta, true)
            } else if delta < 0 {
                _ = model.DecreaseUserQuota(existingUser.Id, -delta, true)
            }
        }
    }

    if err := model.InvalidateUserCache(existingUser.Id); err != nil {
        common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + username)
    }
    presentUsernames[username] = true
    result.Updated++
    result.UpdatedUsernames = append(result.UpdatedUsernames, username)
    continue
}
```

- [ ] **Step 4: Add `presentUsernames` tracking to the assign and create branches**

In the "assign existing user (no org)" branch, add after `result.Assigned++`:
```go
presentUsernames[username] = true
```

In the "create new user" branch, add after `result.Created++`:
```go
presentUsernames[username] = true
```

Also add at the top of the per-row loop (after username empty check passes) so error-skipped rows are NOT in presentUsernames — the tracking should only happen on successful rows, which the `continue` statements after errors ensure naturally.

- [ ] **Step 5: Add removeAbsent logic after the row loop**

After the `for rowNum, row := range rows[1:] { ... }` loop, add:

```go
if removeAbsent {
    var currentMembers []model.User
    if err := model.DB.
        Where("organization_id = ? AND organization_role != ?", organizationId, model.OrganizationRoleOwner).
        Find(&currentMembers).Error; err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("remove-absent: failed to query members: %v", err))
    } else {
        for _, member := range currentMembers {
            if presentUsernames[member.Username] {
                continue
            }
            if err := model.DB.Model(&model.User{}).
                Select("organization_id", "organization_role").
                Where("id = ?", member.Id).
                Updates(model.User{OrganizationId: 0, OrganizationRole: ""}).Error; err != nil {
                result.Errors = append(result.Errors, fmt.Sprintf("remove-absent (%s): failed to remove: %v", member.Username, err))
                continue
            }
            if err := model.InvalidateUserCache(member.Id); err != nil {
                common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + member.Username)
            }
            result.Removed++
            result.RemovedUsernames = append(result.RemovedUsernames, member.Username)
        }
    }
}
```

- [ ] **Step 6: Verify `model.OrganizationRoleOwner` constant exists**

Run:
```bash
grep -r "OrganizationRoleOwner\|OrganizationRoleMember" /home/molla/new-api/model/ | head -10
```

If the constant is defined as a string literal `"owner"` elsewhere, use the same pattern. If `model.OrganizationRoleOwner` doesn't exist, use the string `"owner"` directly in the query.

- [ ] **Step 7: Build to verify**

```bash
cd /home/molla/new-api && go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add service/org_user_export.go
git commit -m "feat: org import — update existing members + remove absent option"
```

---

### Task 2: Update controller to pass removeAbsent

**Files:**
- Modify: `controller/organization.go` (line ~744)

- [ ] **Step 1: Pass `removeAbsent` to service call**

In `ImportOrganizationUsers` (around line 744), change:

```go
result, err := service.ImportOrgUsersFromFile(f, organizationId)
```

to:

```go
removeAbsent := c.PostForm("remove_absent") == "true"
result, err := service.ImportOrgUsersFromFile(f, organizationId, removeAbsent)
```

- [ ] **Step 2: Build**

```bash
cd /home/molla/new-api && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add controller/organization.go
git commit -m "feat: org import controller — pass remove_absent flag"
```

---

### Task 3: Update frontend types and API

**Files:**
- Modify: `web/default/src/features/organizations/types.ts`
- Modify: `web/default/src/features/organizations/api.ts`

- [ ] **Step 1: Update `OrgImportResult` in types.ts**

Replace:
```typescript
export interface OrgImportResult {
  assigned: number
  created: number
  skipped: number
  skipped_usernames: string[]
  errors: string[]
}
```

With:
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

- [ ] **Step 2: Update `importOrgUsers` in api.ts**

Change signature and body from:
```typescript
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

To:
```typescript
export async function importOrgUsers(
  file: File,
  organizationId?: number,
  removeAbsent?: boolean
): Promise<OrgImportResult> {
  const params = organizationId ? `?organization_id=${organizationId}` : ''
  const form = new FormData()
  form.append('file', file)
  if (removeAbsent) form.append('remove_absent', 'true')
  const res = await api.post(`/api/organization/users/import${params}`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return res.data.data as OrgImportResult
}
```

- [ ] **Step 3: Commit**

```bash
git add web/default/src/features/organizations/types.ts web/default/src/features/organizations/api.ts
git commit -m "feat: org import frontend — update OrgImportResult type + removeAbsent param"
```

---

### Task 4: Update OrgUsersImportDialog component

**Files:**
- Modify: `web/default/src/features/organizations/components/org-users-import-dialog.tsx`

- [ ] **Step 1: Add removeAbsent state and checkbox**

Add state after the existing `useState` declarations:
```typescript
const [removeAbsent, setRemoveAbsent] = useState(false)
```

In the JSX, after the file select button and before the result block, add:
```tsx
<label className='flex items-center gap-2 text-sm cursor-pointer'>
  <input
    type='checkbox'
    checked={removeAbsent}
    onChange={(e) => setRemoveAbsent(e.target.checked)}
    disabled={loading}
    className='h-4 w-4'
  />
  {t('Remove members not in file')}
</label>
```

- [ ] **Step 2: Pass removeAbsent to importOrgUsers call**

Change:
```typescript
const res = await importOrgUsers(file, organizationId)
```
To:
```typescript
const res = await importOrgUsers(file, organizationId, removeAbsent)
```

- [ ] **Step 3: Update result display**

Replace the entire result block:
```tsx
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
    {result.updated > 0 && (
      <p className='text-blue-600'>
        ✓ {t('{{count}} members updated', { count: result.updated })}
      </p>
    )}
    {result.removed > 0 && (
      <p className='text-amber-600'>
        ✓ {t('{{count}} members removed from org', { count: result.removed })}: {result.removed_usernames.join(', ')}
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
```

- [ ] **Step 4: Reset removeAbsent on close**

In `handleClose`, add `setRemoveAbsent(false)` alongside the other resets:
```typescript
const handleClose = () => {
  if (result) onSuccess()
  setFile(null)
  setResult(null)
  setError(null)
  setRemoveAbsent(false)
  onOpenChange(false)
}
```

- [ ] **Step 5: Run i18n sync to register new keys**

```bash
cd /home/molla/new-api/web/default && bun run i18n:sync
```

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/organizations/components/org-users-import-dialog.tsx web/default/src/i18n/locales/
git commit -m "feat: org import dialog — remove absent checkbox + updated/removed result display"
```

---

### Task 5: Build, deploy, verify

**Files:** none (deployment only)

- [ ] **Step 1: Build frontend**

```bash
cd /home/molla/new-api/web/default && bun run build
```

Expected: no errors.

- [ ] **Step 2: Build backend**

```bash
cd /home/molla/new-api && go build -o new-api .
```

Expected: binary produced.

- [ ] **Step 3: Deploy**

```bash
sudo systemctl stop new-api && sudo cp /home/molla/new-api/new-api /usr/local/bin/new-api && sudo systemctl start new-api
```

- [ ] **Step 4: Verify service running**

```bash
sudo systemctl status new-api | head -5
```

Expected: `active (running)`

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "chore: deploy org import update/remove-absent feature"
```
