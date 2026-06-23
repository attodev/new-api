/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  BarChart3,
  Building2,
  RefreshCw,
  Save,
  UserPlus,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import {
  formatQuota,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import {
  ORGANIZATION_ROLE,
  hasOrganizationOwnerRole,
} from '@/lib/organization-roles'
import { ROLE } from '@/lib/roles'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'
import {
  assignOrganizationUser,
  createOrganization,
  deleteOrganization,
  exportOrgUsers,
  getAssignableOrganizationUsers,
  getOrganizationProfile,
  getOrganizationUsers,
  getOrganizationUserSubscriptions,
  getOrganizations,
  removeOrganizationUserMembership,
  updateOrganization,
  updateOrganizationUser,
} from '../api'
import {
  getActiveOrganizationSubscriptionUserIds,
  getOrganizationQuotaControlState,
  shouldDisableQuotaForOrganizationSubscription,
} from '../lib/organization-subscription-utils'
import { OrgUsersImportDialog } from './org-users-import-dialog'
import type {
  Organization,
  OrganizationRole,
  OrganizationUpdatePayload,
  OrganizationUser,
  OrganizationUserSubscriptionRecord,
} from '../types'

type PendingChange =
  | { type: 'quota'; userId: number; newQuota: number; originalQuota: number }
  | { type: 'status'; userId: number; newStatus: number }
  | { type: 'remove'; userId: number }
  | { type: 'role'; userId: number; newRole: OrganizationRole }

const USER_STATUS_ENABLED = 1
const USER_STATUS_DISABLED = 2
const ORGANIZATION_ROLES: OrganizationRole[] = [
  ORGANIZATION_ROLE.MEMBER,
  ORGANIZATION_ROLE.ADMIN,
  ORGANIZATION_ROLE.OWNER,
]
const ASSIGNABLE_ROLES: OrganizationRole[] = [
  ORGANIZATION_ROLE.MEMBER,
  ORGANIZATION_ROLE.ADMIN,
]
const QUICK_QUOTA_AMOUNTS = [1, 5, 10, 100]

export function OrganizationUsersTable() {
  const { t } = useTranslation()
  const currentUser = useAuthStore((s) => s.auth.user)
  const [users, setUsers] = useState<OrganizationUser[]>([])
  const [subscriptionRecords, setSubscriptionRecords] = useState<
    OrganizationUserSubscriptionRecord[]
  >([])
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [organizationProfile, setOrganizationProfile] =
    useState<Organization | null>(null)
  const [candidateUsers, setCandidateUsers] = useState<
    Array<User | OrganizationUser>
  >([])
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [loading, setLoading] = useState(false)
  const [loadingCandidates, setLoadingCandidates] = useState(false)
  const [pendingChanges, setPendingChanges] = useState<PendingChange[]>([])
  const [selectedUserIds, setSelectedUserIds] = useState<Set<number>>(new Set())
  const [saving, setSaving] = useState(false)
  const [creating, setCreating] = useState(false)
  const [assigning, setAssigning] = useState(false)
  const [organizationName, setOrganizationName] = useState('')
  const [organizationDescription, setOrganizationDescription] = useState('')
  const [organizationQuotaAmount, setOrganizationQuotaAmount] = useState('')
  const [editingOrganization, setEditingOrganization] =
    useState<Organization | null>(null)
  const [ownerUserId, setOwnerUserId] = useState('')
  const [assignUsername, setAssignUsername] = useState('')

  const isRoot = (currentUser?.role ?? 0) >= ROLE.SUPER_ADMIN
  const isOrganizationOwner = hasOrganizationOwnerRole(
    currentUser?.organization_role
  )
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const canManageOrganizationUsers =
    Boolean(currentUser?.organization_id) || isOrganizationOwner
  const [currentPage, setCurrentPage] = useState(1)
  const [totalUsers, setTotalUsers] = useState(0)
  const [keyword, setKeyword] = useState('')
  const [orderBy, setOrderBy] = useState('id')
  const [orderDir, setOrderDir] = useState<'asc' | 'desc'>('asc')
  const [keywordInput, setKeywordInput] = useState('')

  function setPendingChange(change: PendingChange) {
    setPendingChanges(prev => {
      const filtered = prev.filter(c => {
        if (c.userId !== change.userId) return true
        if (c.type !== change.type) return true
        return false
      })
      return [...filtered, change]
    })
  }

  function removePendingChange(userId: number, type: PendingChange['type']) {
    setPendingChanges(prev =>
      prev.filter(c => !(c.userId === userId && c.type === type))
    )
  }

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

  async function loadOrganizations() {
    if (!isRoot) {
      setOrganizations([])
      return
    }
    try {
      const res = await getOrganizations({ page: 1, size: 50 })
      if (res.success && res.data?.items) {
        setOrganizations(res.data.items)
      } else {
        toast.error(res.message || t('Failed to load organizations'))
      }
    } catch (error: unknown) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load organizations')
      )
    }
  }

  async function loadOrganizationProfile() {
    if (!isOrganizationOwner && !currentUser?.organization_role) {
      setOrganizationProfile(null)
      return
    }
    try {
      const res = await getOrganizationProfile()
      if (res.success && res.data) {
        setOrganizationProfile(res.data)
      }
    } catch {
      setOrganizationProfile(null)
    }
  }

  async function loadCandidateUsers() {
    if (!isRoot && !isOrganizationOwner) return
    setLoadingCandidates(true)
    try {
      const res = isRoot
        ? await getUsers({ p: 1, page_size: 50 })
        : await getAssignableOrganizationUsers({ page: 1, size: 50 })
      if (res.success && res.data?.items) {
        const items = res.data.items.filter((user) => {
          if (user.role >= ROLE.ADMIN) return false
          if (isRoot) return (user.organization_id ?? 0) === 0
          return true
        })
        setCandidateUsers(items)
      } else {
        toast.error(res.message || t('Failed to load users'))
      }
    } finally {
      setLoadingCandidates(false)
    }
  }

  useEffect(() => {
    void loadUsers()
  }, [canManageOrganizationUsers, currentPage, keyword, orderBy, orderDir])

  useEffect(() => {
    const timer = setTimeout(() => {
      setKeyword(keywordInput)
      setCurrentPage(1)
    }, 300)
    return () => clearTimeout(timer)
  }, [keywordInput])

  useEffect(() => {
    void loadOrganizations()
  }, [isRoot])

  useEffect(() => {
    void loadOrganizationProfile()
  }, [currentUser?.organization_id, currentUser?.organization_role])

  useEffect(() => {
    void loadCandidateUsers()
  }, [isRoot, isOrganizationOwner])

  async function handleCreateOrganization() {
    const parsedOwnerUserId = Number(ownerUserId)
    if (!organizationName.trim() || !Number.isInteger(parsedOwnerUserId)) {
      toast.error(t('Organization name and owner are required'))
      return
    }

    setCreating(true)
    try {
      const res = await createOrganization({
        name: organizationName.trim(),
        description: organizationDescription.trim(),
        owner_user_id: parsedOwnerUserId,
        quota: organizationQuotaAmount.trim()
          ? parseQuotaFromDollars(Number(organizationQuotaAmount))
          : 0,
      })
      if (res.success) {
        toast.success(t('Organization created'))
        setOrganizationName('')
        setOrganizationDescription('')
        setOrganizationQuotaAmount('')
        await Promise.all([loadCandidateUsers(), loadOrganizations()])
      } else {
        toast.error(res.message || t('Failed to create organization'))
      }
    } finally {
      setCreating(false)
    }
  }

  async function handleAssignUser() {
    const username = assignUsername.trim()
    if (!username) {
      toast.error(t('Username is required'))
      return
    }

    setAssigning(true)
    try {
      const matched = candidateUsers.find(u => u.username === username)
      if (!matched) {
        toast.error(t('User not found'))
        return
      }
      const res = await assignOrganizationUser(matched.id, {
        organization_role: ORGANIZATION_ROLE.MEMBER,
      })
      if (res.success) {
        toast.success(t('Organization member assigned'))
        setAssignUsername('')
        await Promise.all([loadUsers(), loadCandidateUsers()])
      } else {
        toast.error(res.message || t('Failed to assign organization member'))
      }
    } finally {
      setAssigning(false)
    }
  }

  function handleRoleChange(userId: number, newRole: OrganizationRole) {
    setPendingChange({ type: 'role', userId, newRole })
  }

  function handleStartEditOrganization(organization: Organization) {
    setEditingOrganization(organization)
    setOrganizationName(organization.name)
    setOrganizationDescription(organization.description ?? '')
    setOrganizationQuotaAmount(String(quotaUnitsToDollars(organization.quota)))
  }

  function handleCancelEditOrganization() {
    setEditingOrganization(null)
    setOrganizationName('')
    setOrganizationDescription('')
    setOrganizationQuotaAmount('')
  }

  async function handleSaveOrganization() {
    if (!editingOrganization) return
    if (!organizationName.trim()) {
      toast.error(t('Organization name cannot be empty'))
      return
    }

    const payload: OrganizationUpdatePayload = {}
    if (organizationName.trim() !== editingOrganization.name) {
      payload.name = organizationName.trim()
    }
    if (organizationDescription !== (editingOrganization.description ?? '')) {
      payload.description = organizationDescription
    }
    const newQuota = organizationQuotaAmount.trim()
      ? parseQuotaFromDollars(Number(organizationQuotaAmount))
      : editingOrganization.quota
    if (newQuota !== editingOrganization.quota) {
      payload.quota = newQuota
    }

    if (Object.keys(payload).length === 0) {
      handleCancelEditOrganization()
      return
    }

    setCreating(true)
    try {
      const res = await updateOrganization(editingOrganization.id, payload)
      if (res.success) {
        toast.success(t('Organization updated'))
        handleCancelEditOrganization()
        await Promise.all([loadOrganizations(), loadOrganizationProfile()])
      } else {
        toast.error(res.message || t('Failed to update organization'))
      }
    } finally {
      setCreating(false)
    }
  }

  async function handleExportOrgUsers() {
    setExporting(true)
    try {
      await exportOrgUsers()
    } catch (e: any) {
      toast.error(e?.response?.data?.message ?? t('Export failed'))
    } finally {
      setExporting(false)
    }
  }

  async function handleDeleteOrganization(organization: Organization) {
    try {
      await deleteOrganization(organization.id)
      toast.success(t('Organization deleted'))
      await loadOrganizations()
    } catch (e: any) {
      toast.error(e?.response?.data?.message ?? t('Failed to delete organization'))
    }
  }

  function handleQuotaChange(user: OrganizationUser, displayValue: string) {
    const value = Number(displayValue)
    if (!Number.isFinite(value)) return
    const newQuota = parseQuotaFromDollars(value)
    if (newQuota === user.quota) {
      removePendingChange(user.id, 'quota')
      return
    }
    setPendingChange({ type: 'quota', userId: user.id, newQuota, originalQuota: user.quota })
  }

  function handleQuotaAdd(user: OrganizationUser, amount: number) {
    const quotaChange = pendingChanges.find(c => c.type === 'quota' && c.userId === user.id) as
      | { type: 'quota'; userId: number; newQuota: number; originalQuota: number }
      | undefined
    const base = quotaChange ? quotaChange.newQuota : user.quota
    const newQuota = base + parseQuotaFromDollars(amount)
    setPendingChange({ type: 'quota', userId: user.id, newQuota, originalQuota: user.quota })
  }

  function handleMarkRemove(userId: number) {
    setPendingChanges(prev => [
      ...prev.filter(c => c.userId !== userId),
      { type: 'remove', userId },
    ])
    setSelectedUserIds(prev => {
      const next = new Set(prev)
      next.delete(userId)
      return next
    })
  }

  function handleCancelPending() {
    setPendingChanges([])
    setSelectedUserIds(new Set())
  }

  async function handleSavePending() {
    if (pendingChanges.length === 0) return
    setSaving(true)
    const errors: string[] = []
    await Promise.all(
      pendingChanges.map(async change => {
        try {
          if (change.type === 'quota') {
            const res = await updateOrganizationUser(change.userId, { quota: change.newQuota })
            if (!res.success) errors.push(res.message ?? t('Failed to update quota'))
          } else if (change.type === 'status') {
            const res = await updateOrganizationUser(change.userId, { status: change.newStatus })
            if (!res.success) errors.push(res.message ?? t('Failed to update status'))
          } else if (change.type === 'remove') {
            const res = await removeOrganizationUserMembership(change.userId)
            if (!res.success) errors.push(res.message ?? t('Failed to remove member'))
          } else if (change.type === 'role') {
            const res = await assignOrganizationUser(change.userId, { organization_role: change.newRole })
            if (!res.success) errors.push(res.message ?? t('Failed to update role'))
          }
        } catch (e: unknown) {
          errors.push(e instanceof Error ? e.message : t('Unknown error'))
        }
      })
    )
    setSaving(false)
    if (errors.length > 0) {
      errors.forEach(msg => toast.error(msg))
    } else {
      toast.success(t('Changes saved'))
    }
    setPendingChanges([])
    setSelectedUserIds(new Set())
    await loadUsers()
  }

  function handleBulkDisable() {
    selectedUserIds.forEach(userId => {
      const user = users.find(u => u.id === userId)
      if (!user) return
      const statusChange = pendingChanges.find(c => c.type === 'status' && c.userId === userId) as
        | { type: 'status'; userId: number; newStatus: number }
        | undefined
      const currentStatus = statusChange ? statusChange.newStatus : user.status
      if (currentStatus === USER_STATUS_ENABLED) {
        setPendingChange({ type: 'status', userId, newStatus: USER_STATUS_DISABLED })
      }
    })
  }

  function handleBulkRemove() {
    selectedUserIds.forEach(userId => {
      handleMarkRemove(userId)
    })
  }

  function handleSelectAll(checked: boolean) {
    if (checked) {
      const selectableIds = users
        .filter(u => u.organization_role !== ORGANIZATION_ROLE.OWNER)
        .map(u => u.id)
      setSelectedUserIds(new Set(selectableIds))
    } else {
      setSelectedUserIds(new Set())
    }
  }

  function handleSelectUser(userId: number, checked: boolean) {
    setSelectedUserIds(prev => {
      const next = new Set(prev)
      if (checked) next.add(userId)
      else next.delete(userId)
      return next
    })
  }

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

  return (
    <div className='space-y-4'>
      <OrgUsersImportDialog
        open={importDialogOpen}
        onOpenChange={setImportDialogOpen}
        onSuccess={() => void loadUsers()}
      />
      <div className='flex items-center justify-between gap-3'>
        <h1 className='text-xl font-semibold'>{t('Organization Users')}</h1>
        <div className='flex items-center gap-2'>
          {isOrganizationOwner && (
            <>
              <Button
                variant='outline'
                onClick={() => void handleExportOrgUsers()}
                disabled={exporting}
              >
                {t('Export')}
              </Button>
              <Button
                variant='outline'
                onClick={() => setImportDialogOpen(true)}
              >
                {t('Import')}
              </Button>
            </>
          )}
          <Button variant='outline' onClick={() => void loadUsers()} disabled={loading}>
            <RefreshCw />
            {t('Refresh')}
          </Button>
        </div>
      </div>

      {(isRoot || isOrganizationOwner) && (
        <div className='grid gap-3 lg:grid-cols-2'>
          {isRoot && (
            <div className='space-y-3 rounded-md border p-3'>
              <div className='flex items-center gap-2 text-sm font-medium'>
                <Building2 className='size-4' />
                {editingOrganization
                  ? t('Edit Organization')
                  : t('Create Organization')}
              </div>
              <div className='grid gap-2 sm:grid-cols-2'>
                <Input
                  value={organizationName}
                  onChange={(event) =>
                    setOrganizationName(event.currentTarget.value)
                  }
                  placeholder={t('Organization Name')}
                />
                <Input
                  value={organizationDescription}
                  onChange={(event) =>
                    setOrganizationDescription(event.currentTarget.value)
                  }
                  placeholder={t('Description')}
                />
                <Input
                  value={organizationQuotaAmount}
                  onChange={(event) =>
                    setOrganizationQuotaAmount(event.currentTarget.value)
                  }
                  inputMode={tokensOnly ? 'numeric' : 'decimal'}
                  placeholder={t('Initial organization quota')}
                />
              </div>
              <div className='flex flex-wrap items-center gap-2'>
                {!editingOrganization && (
                  <Select
                    items={candidateUsers.map((user) => ({
                      value: String(user.id),
                      label: user.username,
                    }))}
                    value={ownerUserId}
                    onValueChange={(value) =>
                      value !== null && setOwnerUserId(value)
                    }
                    disabled={loadingCandidates || candidateUsers.length === 0}
                  >
                    <SelectTrigger className='min-w-56 flex-1'>
                      <SelectValue placeholder={t('Owner User')} />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {candidateUsers.map((user) => (
                          <SelectItem key={user.id} value={String(user.id)}>
                            {user.username} #{user.id}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                )}
                {editingOrganization ? (
                  <>
                    <Button
                      onClick={() => void handleSaveOrganization()}
                      disabled={creating}
                    >
                      <Save />
                      {t('Save')}
                    </Button>
                    <Button
                      variant='outline'
                      onClick={handleCancelEditOrganization}
                      disabled={creating}
                    >
                      {t('Cancel')}
                    </Button>
                  </>
                ) : (
                  <Button
                    onClick={() => void handleCreateOrganization()}
                    disabled={creating || !ownerUserId}
                  >
                    <Building2 />
                    {t('Create')}
                  </Button>
                )}
              </div>
            </div>
          )}

          {isOrganizationOwner && (
            <div className='space-y-3 rounded-md border p-3'>
              <div className='flex items-center gap-2 text-sm font-medium'>
                <UserPlus className='size-4' />
                {t('Assign Organization Member')}
              </div>
              <div className='flex flex-wrap items-center gap-2'>
                <Input
                  className='min-w-56 flex-1'
                  placeholder={t('Username')}
                  value={assignUsername}
                  onChange={(e) => setAssignUsername(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void handleAssignUser()
                  }}
                  disabled={assigning}
                />
                <Button
                  onClick={() => void handleAssignUser()}
                  disabled={assigning || !assignUsername.trim()}
                >
                  <UserPlus />
                  추가
                </Button>
              </div>
            </div>
          )}
        </div>
      )}

      {organizationProfile && (
        <div className='grid gap-3 rounded-md border p-3 sm:grid-cols-[1fr_auto]'>
          <div>
            <div className='flex items-center gap-2 text-sm font-medium'>
              <Wallet className='size-4' />
              {t('Organization wallet')}
            </div>
            <div className='text-muted-foreground text-xs'>
              {organizationProfile.name}
            </div>
          </div>
          <div className='flex flex-col gap-3 sm:items-end'>
            <div className='grid gap-2 text-sm sm:grid-cols-2'>
              <div>
                <div className='text-muted-foreground text-xs'>
                  {t('Organization quota')}
                </div>
                <div className='font-medium'>
                  {formatQuota(organizationProfile.quota)}
                </div>
              </div>
              <div>
                <div className='text-muted-foreground text-xs'>
                  {t('Organization used quota')}
                </div>
                <div className='font-medium'>
                  {formatQuota(organizationProfile.used_quota)}
                </div>
              </div>
            </div>
            <div className='flex flex-wrap justify-end gap-2'>
              <Button
                variant='outline'
                size='sm'
                render={<Link to='/organization/dashboard' />}
              >
                <BarChart3 />
                {t('Usage dashboard')}
              </Button>
              {isOrganizationOwner && (
                <Button
                  variant='outline'
                  size='sm'
                  render={<Link to='/wallet' />}
                >
                  <Wallet />
                  {t('Top up organization wallet')}
                </Button>
              )}
            </div>
          </div>
        </div>
      )}

      {isRoot && organizations.length > 0 && (
        <div className='space-y-3 rounded-md border p-3'>
          <div className='flex items-center gap-2 text-sm font-medium'>
            <Wallet className='size-4' />
            {t('Manage organization quota')}
          </div>
          <div className='grid gap-2 md:grid-cols-2'>
            {organizations.map((organization) => (
              <div key={organization.id} className='rounded-md border p-3'>
                <div className='flex items-start justify-between gap-2'>
                  <div>
                    <div className='font-medium'>{organization.name}</div>
                    {organization.description && (
                      <div className='text-muted-foreground text-xs'>
                        {organization.description}
                      </div>
                    )}
                    <div className='text-muted-foreground text-xs'>
                      {t('Organization quota')}: {formatQuota(organization.quota)}
                      {' · '}
                      {t('Organization used quota')}: {formatQuota(organization.used_quota)}
                    </div>
                  </div>
                  <span className='text-muted-foreground text-xs'>
                    #{organization.id}
                  </span>
                </div>
                <div className='mt-3 flex items-center gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => handleStartEditOrganization(organization)}
                  >
                    <Save />
                    {t('Edit')}
                  </Button>
                  <AlertDialog>
                    <AlertDialogTrigger asChild>
                      <Button type='button' variant='destructive' size='sm'>
                        {t('Delete')}
                      </Button>
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>{t('Delete Organization')}</AlertDialogTitle>
                        <AlertDialogDescription>
                          {t('Are you sure you want to delete organization "{{name}}"? This action cannot be undone.', { name: organization.name })}
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                        <AlertDialogAction onClick={() => void handleDeleteOrganization(organization)}>
                          {t('Delete')}
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {pendingChanges.length > 0 && (
        <div className='flex items-center justify-between rounded-md border border-border bg-muted/60 px-4 py-2 text-sm'>
          <span className='text-foreground'>
            {[
              pendingChanges.filter(c => c.type === 'quota').length > 0 &&
                t('{{count}} quota changes', { count: pendingChanges.filter(c => c.type === 'quota').length }),
              pendingChanges.filter(c => c.type === 'status').length > 0 &&
                t('{{count}} status changes', { count: pendingChanges.filter(c => c.type === 'status').length }),
              pendingChanges.filter(c => c.type === 'remove').length > 0 &&
                t('{{count}} pending removal', { count: pendingChanges.filter(c => c.type === 'remove').length }),
            ]
              .filter(Boolean)
              .join(' · ')}
          </span>
          <div className='flex gap-2'>
            <Button size='sm' onClick={() => void handleSavePending()} disabled={saving}>
              {t('Save')}
            </Button>
            <Button size='sm' variant='outline' onClick={handleCancelPending} disabled={saving}>
              {t('Cancel')}
            </Button>
          </div>
        </div>
      )}
      <div className='flex flex-wrap items-center justify-between gap-2'>
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
        {selectedUserIds.size > 0 && (
          <div className='flex items-center gap-2 text-sm'>
            <span className='text-muted-foreground'>
              {t('{{count}} selected', { count: selectedUserIds.size })}
            </span>
            <div className='h-4 w-px bg-border' />
            <Button size='sm' variant='outline' onClick={handleBulkDisable}>
              {t('Disable')}
            </Button>
            <Button size='sm' variant='outline' onClick={handleBulkRemove} className='border-destructive text-destructive'>
              {t('조직에서 제거')}
            </Button>
          </div>
        )}
      </div>

      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full min-w-[720px] text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='w-10 px-3 py-2'>
                <input
                  type='checkbox'
                  checked={
                    users.filter(u => u.organization_role !== ORGANIZATION_ROLE.OWNER).length > 0 &&
                    users
                      .filter(u => u.organization_role !== ORGANIZATION_ROLE.OWNER)
                      .every(u => selectedUserIds.has(u.id))
                  }
                  onChange={e => handleSelectAll(e.target.checked)}
                  className='cursor-pointer'
                />
              </th>
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
            </tr>
          </thead>
          <tbody>
            {users.map((user) => {
              const isOwner = user.organization_role === ORGANIZATION_ROLE.OWNER
              const pendingRemove = pendingChanges.find(c => c.type === 'remove' && c.userId === user.id)
              const pendingStatus = pendingChanges.find(c => c.type === 'status' && c.userId === user.id) as
                | { type: 'status'; userId: number; newStatus: number } | undefined
              const pendingQuota = pendingChanges.find(c => c.type === 'quota' && c.userId === user.id) as
                | { type: 'quota'; userId: number; newQuota: number; originalQuota: number } | undefined
              const pendingRole = pendingChanges.find(c => c.type === 'role' && c.userId === user.id) as
                | { type: 'role'; userId: number; newRole: OrganizationRole } | undefined
              const quotaControlState = getOrganizationQuotaControlState(
                user.id,
                user.quota,
                subscriptionRecords
              )
              const hasActivePlan = quotaControlState.hasActivePlan
              const effectiveStatus = pendingStatus ? pendingStatus.newStatus : user.status

              return (
                <tr key={user.id} className='border-t'>
                  <td className='px-3 py-2'>
                    <input
                      type='checkbox'
                      disabled={isOwner}
                      checked={!isOwner && selectedUserIds.has(user.id)}
                      onChange={e => !isOwner && handleSelectUser(user.id, e.target.checked)}
                      className={isOwner ? 'cursor-not-allowed opacity-30' : 'cursor-pointer'}
                      title={isOwner ? t('Owner cannot be modified') : undefined}
                    />
                  </td>
                  <td className='px-3 py-2'>
                    <div className={`font-medium${pendingRemove ? ' line-through text-muted-foreground' : ''}`}>
                      {user.username}
                    </div>
                    {user.display_name && (
                      <div className='text-muted-foreground text-xs'>{user.display_name}</div>
                    )}
                    {pendingRemove && (
                      <div className='text-xs text-destructive'>{t('Pending removal')}</div>
                    )}
                    {!pendingRemove && pendingStatus && (
                      <div className='text-xs text-orange-600'>
                        {pendingStatus.newStatus === USER_STATUS_DISABLED
                          ? t('Pending disable')
                          : t('Pending enable')}
                      </div>
                    )}
                    {!pendingRemove && !pendingStatus && pendingQuota && (
                      <div className='text-xs text-amber-700'>{t('Quota modified')}</div>
                    )}
                  </td>
                  <td className='px-3 py-2'>
                    {isOwner || pendingRemove ? (
                      <span className={pendingRemove ? 'text-muted-foreground line-through' : ''}>
                        {user.organization_role}
                      </span>
                    ) : (
                      <Select
                        items={ASSIGNABLE_ROLES.map(r => ({ value: r, label: r }))}
                        value={pendingRole ? pendingRole.newRole : user.organization_role}
                        onValueChange={value => {
                          if (value !== null) handleRoleChange(user.id, value as OrganizationRole)
                        }}
                      >
                        <SelectTrigger className='h-7 w-28 text-xs'>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {ASSIGNABLE_ROLES.map(r => (
                              <SelectItem key={r} value={r}>{t(r)}</SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    )}
                    {pendingRole && !pendingRemove && (
                      <div className='text-xs text-amber-700'>{t('Role modified')}</div>
                    )}
                  </td>
                  <td className={`px-3 py-2${pendingRemove ? ' text-muted-foreground line-through' : ''}`}>
                    {effectiveStatus === USER_STATUS_ENABLED ? t('Enabled') : t('Disabled')}
                  </td>
                  <td className='px-3 py-2'>
                    <div className='min-w-64 space-y-2'>
                      <div className='text-muted-foreground text-xs'>
                        {t('Current quota balance')}:{' '}
                        {hasActivePlan
                          ? `${formatQuota(0)} (${t('Organization plan in use')})`
                          : formatQuota(quotaControlState.displayQuota)}
                      </div>
                      <div className='flex items-center gap-2'>
                        <Input
                          key={`${user.id}-${user.quota}-${hasActivePlan}-${pendingQuota?.newQuota ?? ''}`}
                          className={`w-32${pendingQuota ? ' border-amber-500' : ''}`}
                          type='number'
                          step={tokensOnly ? 1 : 0.01}
                          min={0}
                          defaultValue={quotaUnitsToDollars(
                            pendingQuota ? pendingQuota.newQuota : quotaControlState.displayQuota
                          )}
                          placeholder={t('Quota amount')}
                          disabled={!!pendingRemove || quotaControlState.disabled}
                          onBlur={(event) => {
                            if (!shouldDisableQuotaForOrganizationSubscription(user.id, subscriptionRecords)) {
                              handleQuotaChange(user, event.currentTarget.value)
                            }
                          }}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter') event.currentTarget.blur()
                          }}
                        />
                        <span className='text-muted-foreground text-xs'>{currencyLabel}</span>
                        {pendingQuota && (
                          <span className='text-muted-foreground text-xs'>
                            ({t('was')} {quotaUnitsToDollars(pendingQuota.originalQuota).toFixed(2)})
                          </span>
                        )}
                      </div>
                      {!tokensOnly && (
                        <div className='flex flex-wrap gap-1'>
                          {QUICK_QUOTA_AMOUNTS.map((amount) => (
                            <Button
                              key={amount}
                              type='button'
                              variant='outline'
                              size='sm'
                              disabled={!!pendingRemove || quotaControlState.disabled}
                              onClick={() => handleQuotaAdd(user, amount)}
                            >
                              +{formatQuota(parseQuotaFromDollars(amount))}
                            </Button>
                          ))}
                        </div>
                      )}
                    </div>
                  </td>
                </tr>
              )
            })}
            {!loading && users.length === 0 && (
              <tr>
                <td
                  className='text-muted-foreground px-3 py-8 text-center'
                  colSpan={5}
                >
                  {t('No data')}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
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
    </div>
  )
}
