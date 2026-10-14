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
import { Link } from '@tanstack/react-router'
import {
  BarChart3,
  Building2,
  RefreshCw,
  Save,
  UserPlus,
  Wallet,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  assignOrganizationUser,
  createOrganization,
  deleteOrganization,
  exportOrgUsers,
  getAssignableOrganizationUsers,
  getOrganizationProfile,
  getOrganizationUsers,
  getOrganizationUserSubscriptions,
  getAllOrganizations,
  removeOrganizationUserMembership,
  updateOrganization,
  updateOrganizationUser,
} from '@/features/organizations/api'
import {
  getOrganizationQuotaControlState,
  shouldDisableQuotaForOrganizationSubscription,
} from '@/features/organizations/lib/organization-subscription-utils'
import type {
  Organization,
  OrganizationRole,
  OrganizationUpdatePayload,
  OrganizationUser,
  OrganizationUserSubscriptionRecord,
} from '@/features/organizations/types'
import { getUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import {
  formatQuota,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import {
  ORGANIZATION_ROLE,
  hasOrganizationAdminRole,
  hasOrganizationOwnerRole,
} from '@/lib/organization-roles'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { OrgUsersImportDialog } from './org-users-import-dialog'

type AssignableRole = Exclude<OrganizationRole, 'owner'>

type QuotaChange = {
  type: 'quota'
  userId: number
  newQuota: number
  originalQuota: number
}
type StatusChange = { type: 'status'; userId: number; newStatus: number }
type RoleChange = { type: 'role'; userId: number; newRole: AssignableRole }
type PendingChange =
  | QuotaChange
  | StatusChange
  | { type: 'remove'; userId: number }
  | RoleChange

const USER_STATUS_ENABLED = 1
const USER_STATUS_DISABLED = 2
// The membership endpoint never accepts `owner`; ownership is set when the
// organization is created.
const ASSIGNABLE_ROLES: AssignableRole[] = [
  ORGANIZATION_ROLE.MEMBER,
  ORGANIZATION_ROLE.ADMIN,
]
const QUICK_QUOTA_AMOUNTS = [1, 5, 10, 100]
const PAGE_SIZE = 20

function errorMessage(error: unknown, fallback: string): string {
  const message = (error as { response?: { data?: { message?: string } } })
    ?.response?.data?.message
  if (message) return message
  return error instanceof Error && error.message ? error.message : fallback
}

function SortIcon(props: {
  col: string
  orderBy: string
  orderDir: 'asc' | 'desc'
}) {
  if (props.orderBy !== props.col) {
    return <span className='text-muted-foreground ml-1 opacity-40'>↕</span>
  }
  return <span className='ml-1'>{props.orderDir === 'asc' ? '↑' : '↓'}</span>
}

export function OrganizationUsersTable() {
  const { t } = useTranslation()
  const currentUser = useAuthStore((s) => s.auth.user)
  const [users, setUsers] = useState<OrganizationUser[]>([])
  const [subscriptionRecords, setSubscriptionRecords] = useState<
    OrganizationUserSubscriptionRecord[]
  >([])
  const [organizations, setOrganizations] = useState<Organization[]>([])
  // Root is not bound to a single org, so it must pick which org's users to
  // view; the backend requires organization_id for root requests.
  const [organizationId, setOrganizationId] = useState<number | null>(null)
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

  const currentUserId = currentUser?.id
  const currentOrganizationRole = currentUser?.organization_role
  const isRoot = (currentUser?.role ?? 0) >= ROLE.SUPER_ADMIN
  const isOrganizationOwner = hasOrganizationOwnerRole(currentOrganizationRole)
  const isOrganizationAdmin = hasOrganizationAdminRole(currentOrganizationRole)
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const canManageOrganizationUsers = isOrganizationAdmin || isRoot
  // Member mutations go through the organization-admin endpoints, which act
  // only for an admin of the organization; root views members read-only here.
  const canEditMembers = isOrganizationAdmin
  // Excel import/export accept root with an explicit organization_id, so root
  // can transfer the selected organization's members; admins use their own.
  const transferOrganizationId = isRoot
    ? (organizationId ?? undefined)
    : undefined
  const canTransferMembers = isRoot
    ? organizationId !== null
    : isOrganizationAdmin
  const [currentPage, setCurrentPage] = useState(1)
  const [totalUsers, setTotalUsers] = useState(0)
  const [keyword, setKeyword] = useState('')
  const [orderBy, setOrderBy] = useState('id')
  const [orderDir, setOrderDir] = useState<'asc' | 'desc'>('asc')
  const [keywordInput, setKeywordInput] = useState('')

  // Admins cannot modify their own row, and only the owner may act on the
  // owner — whose row is then their own — so the owner row is never editable.
  function isRowEditable(user: OrganizationUser) {
    return (
      canEditMembers &&
      user.id !== currentUserId &&
      user.organization_role !== ORGANIZATION_ROLE.OWNER
    )
  }

  function setPendingChange(change: PendingChange) {
    setPendingChanges((prev) => [
      ...prev.filter(
        (c) => c.userId !== change.userId || c.type !== change.type
      ),
      change,
    ])
  }

  function removePendingChange(userId: number, type: PendingChange['type']) {
    setPendingChanges((prev) =>
      prev.filter((c) => !(c.userId === userId && c.type === type))
    )
  }

  const loadUsers = useCallback(async () => {
    if (!canManageOrganizationUsers) {
      setUsers([])
      setSubscriptionRecords([])
      return
    }
    // Root must choose an organization first; without it the backend rejects
    // the request with "organization_id is required".
    if (isRoot && !organizationId) {
      setUsers([])
      setSubscriptionRecords([])
      setTotalUsers(0)
      return
    }
    const organizationParam = isRoot ? organizationId : undefined
    setLoading(true)
    try {
      const [userRes, subscriptionRes] = await Promise.all([
        getOrganizationUsers({
          page: currentPage,
          size: PAGE_SIZE,
          keyword: keyword || undefined,
          order_by: orderBy,
          order_dir: orderDir,
          organization_id: organizationParam,
        }),
        getOrganizationUserSubscriptions({
          organization_id: organizationParam,
        }),
      ])
      if (userRes.success && userRes.data?.items) {
        setUsers(userRes.data.items)
        setTotalUsers(userRes.data.total)
      } else {
        toast.error(userRes.message || t('Failed to load organization users'))
      }
      setSubscriptionRecords(
        subscriptionRes.success ? subscriptionRes.data || [] : []
      )
    } catch (error: unknown) {
      toast.error(errorMessage(error, t('Failed to load organization users')))
    } finally {
      setLoading(false)
    }
  }, [
    canManageOrganizationUsers,
    currentPage,
    isRoot,
    keyword,
    orderBy,
    orderDir,
    organizationId,
    t,
  ])

  const loadOrganizations = useCallback(async () => {
    if (!isRoot) {
      setOrganizations([])
      return
    }
    try {
      const res = await getAllOrganizations()
      const items = res.data?.items
      if (res.success && items) {
        setOrganizations(items)
        setOrganizationId(
          (current) => current ?? (items.length ? items[0].id : null)
        )
      } else {
        toast.error(res.message || t('Failed to load organizations'))
      }
    } catch (error: unknown) {
      toast.error(errorMessage(error, t('Failed to load organizations')))
    }
  }, [isRoot, t])

  const loadOrganizationProfile = useCallback(async () => {
    if (!currentOrganizationRole) {
      setOrganizationProfile(null)
      return
    }
    try {
      const res = await getOrganizationProfile()
      setOrganizationProfile(res.success && res.data ? res.data : null)
    } catch {
      setOrganizationProfile(null)
    }
  }, [currentOrganizationRole])

  const loadCandidateUsers = useCallback(async () => {
    if (!isRoot && !isOrganizationAdmin) return
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
    } catch (error: unknown) {
      toast.error(errorMessage(error, t('Failed to load users')))
    } finally {
      setLoadingCandidates(false)
    }
  }, [isOrganizationAdmin, isRoot, t])

  useEffect(() => {
    void loadUsers()
  }, [loadUsers])

  useEffect(() => {
    const timer = setTimeout(() => {
      setKeyword(keywordInput)
      setCurrentPage(1)
    }, 300)
    return () => clearTimeout(timer)
  }, [keywordInput])

  useEffect(() => {
    void loadOrganizations()
  }, [loadOrganizations])

  useEffect(() => {
    void loadOrganizationProfile()
  }, [loadOrganizationProfile, currentUser?.organization_id])

  useEffect(() => {
    void loadCandidateUsers()
  }, [loadCandidateUsers])

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
      const matched = candidateUsers.find((u) => u.username === username)
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
      await exportOrgUsers(transferOrganizationId)
    } catch (error: unknown) {
      toast.error(errorMessage(error, t('Export failed')))
    } finally {
      setExporting(false)
    }
  }

  async function handleDeleteOrganization(organization: Organization) {
    try {
      await deleteOrganization(organization.id)
      toast.success(t('Organization deleted'))
      await loadOrganizations()
    } catch (error: unknown) {
      toast.error(errorMessage(error, t('Failed to delete organization')))
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
    setPendingChange({
      type: 'quota',
      userId: user.id,
      newQuota,
      originalQuota: user.quota,
    })
  }

  function handleQuotaAdd(user: OrganizationUser, amount: number) {
    const quotaChange = pendingChanges.find(
      (c): c is QuotaChange => c.type === 'quota' && c.userId === user.id
    )
    const base = quotaChange ? quotaChange.newQuota : user.quota
    setPendingChange({
      type: 'quota',
      userId: user.id,
      newQuota: base + parseQuotaFromDollars(amount),
      originalQuota: user.quota,
    })
  }

  function handleMarkRemove(userId: number) {
    setPendingChanges((prev) => [
      ...prev.filter((c) => c.userId !== userId),
      { type: 'remove', userId },
    ])
    setSelectedUserIds((prev) => {
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
      pendingChanges.map(async (change) => {
        try {
          if (change.type === 'quota') {
            const res = await updateOrganizationUser(change.userId, {
              quota: change.newQuota,
            })
            if (!res.success) {
              errors.push(res.message ?? t('Failed to update quota'))
            }
          } else if (change.type === 'status') {
            const res = await updateOrganizationUser(change.userId, {
              status: change.newStatus,
            })
            if (!res.success) {
              errors.push(res.message ?? t('Failed to update status'))
            }
          } else if (change.type === 'remove') {
            const res = await removeOrganizationUserMembership(change.userId)
            if (!res.success) {
              errors.push(res.message ?? t('Failed to remove member'))
            }
          } else {
            const res = await assignOrganizationUser(change.userId, {
              organization_role: change.newRole,
            })
            if (!res.success) {
              errors.push(res.message ?? t('Failed to update role'))
            }
          }
        } catch (error: unknown) {
          errors.push(errorMessage(error, t('Unknown error')))
        }
      })
    )
    setSaving(false)
    if (errors.length > 0) {
      errors.forEach((msg) => toast.error(msg))
    } else {
      toast.success(t('Changes saved'))
    }
    setPendingChanges([])
    setSelectedUserIds(new Set())
    await loadUsers()
  }

  function handleBulkDisable() {
    selectedUserIds.forEach((userId) => {
      const user = users.find((u) => u.id === userId)
      if (!user || !isRowEditable(user)) return
      const statusChange = pendingChanges.find(
        (c): c is StatusChange => c.type === 'status' && c.userId === userId
      )
      const currentStatus = statusChange ? statusChange.newStatus : user.status
      if (currentStatus === USER_STATUS_ENABLED) {
        setPendingChange({
          type: 'status',
          userId,
          newStatus: USER_STATUS_DISABLED,
        })
      }
    })
  }

  function handleBulkRemove() {
    selectedUserIds.forEach((userId) => {
      handleMarkRemove(userId)
    })
  }

  const selectableUsers = users.filter(isRowEditable)

  function handleSelectAll(checked: boolean) {
    setSelectedUserIds(
      checked ? new Set(selectableUsers.map((u) => u.id)) : new Set()
    )
  }

  function handleSelectUser(userId: number, checked: boolean) {
    setSelectedUserIds((prev) => {
      const next = new Set(prev)
      if (checked) next.add(userId)
      else next.delete(userId)
      return next
    })
  }

  function handleSort(col: string) {
    if (orderBy === col) {
      setOrderDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setOrderBy(col)
      setOrderDir('asc')
    }
    setCurrentPage(1)
  }

  const pendingCounts = {
    quota: pendingChanges.filter((c) => c.type === 'quota').length,
    status: pendingChanges.filter((c) => c.type === 'status').length,
    remove: pendingChanges.filter((c) => c.type === 'remove').length,
    role: pendingChanges.filter((c) => c.type === 'role').length,
  }
  const totalPages = Math.ceil(totalUsers / PAGE_SIZE)

  return (
    <div className='space-y-4'>
      <OrgUsersImportDialog
        open={importDialogOpen}
        onOpenChange={setImportDialogOpen}
        onSuccess={() => void loadUsers()}
        organizationId={transferOrganizationId}
      />
      <div className='flex items-center justify-between gap-3'>
        <h1 className='text-xl font-semibold'>{t('Organization Users')}</h1>
        <div className='flex items-center gap-2'>
          {canTransferMembers && (
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
          <Button
            variant='outline'
            onClick={() => void loadUsers()}
            disabled={loading}
          >
            <RefreshCw />
            {t('Refresh')}
          </Button>
        </div>
      </div>

      {isRoot && (
        <div className='flex flex-wrap items-center gap-2'>
          <span className='text-muted-foreground text-sm'>
            {t('Organization')}
          </span>
          <NativeSelect
            value={organizationId ?? ''}
            onChange={(event) =>
              setOrganizationId(Number(event.currentTarget.value) || null)
            }
            className='w-full sm:w-72'
          >
            {organizations.length === 0 && (
              <NativeSelectOption value=''>{t('No data')}</NativeSelectOption>
            )}
            {organizations.map((org) => (
              <NativeSelectOption key={org.id} value={org.id}>
                {org.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
      )}

      {(isRoot || isOrganizationAdmin) && (
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

          {isOrganizationAdmin && (
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
                  {t('Add')}
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
              {isOrganizationAdmin && (
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
                      {t('Organization quota')}:{' '}
                      {formatQuota(organization.quota)}
                      {' · '}
                      {t('Organization used quota')}:{' '}
                      {formatQuota(organization.used_quota)}
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
                    <AlertDialogTrigger
                      render={
                        <Button type='button' variant='destructive' size='sm' />
                      }
                    >
                      {t('Delete')}
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>
                          {t('Delete Organization')}
                        </AlertDialogTitle>
                        <AlertDialogDescription>
                          {t(
                            'Are you sure you want to delete organization "{{name}}"? This action cannot be undone.',
                            { name: organization.name }
                          )}
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                        <AlertDialogAction
                          variant='destructive'
                          onClick={() =>
                            void handleDeleteOrganization(organization)
                          }
                        >
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
        <div className='border-border bg-muted/60 flex items-center justify-between rounded-md border px-4 py-2 text-sm'>
          <span className='text-foreground'>
            {[
              pendingCounts.quota > 0 &&
                t('{{count}} quota changes', { count: pendingCounts.quota }),
              pendingCounts.status > 0 &&
                t('{{count}} status changes', { count: pendingCounts.status }),
              pendingCounts.role > 0 &&
                t('{{count}} role changes', { count: pendingCounts.role }),
              pendingCounts.remove > 0 &&
                t('{{count}} pending removal', {
                  count: pendingCounts.remove,
                }),
            ]
              .filter(Boolean)
              .join(' · ')}
          </span>
          <div className='flex gap-2'>
            <Button
              size='sm'
              onClick={() => void handleSavePending()}
              disabled={saving}
            >
              {t('Save')}
            </Button>
            <Button
              size='sm'
              variant='outline'
              onClick={handleCancelPending}
              disabled={saving}
            >
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
              onClick={() => {
                setKeywordInput('')
                setKeyword('')
                setCurrentPage(1)
              }}
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
            <div className='bg-border h-4 w-px' />
            <Button size='sm' variant='outline' onClick={handleBulkDisable}>
              {t('Disable')}
            </Button>
            {isOrganizationAdmin && (
              <Button
                size='sm'
                variant='outline'
                onClick={handleBulkRemove}
                className='border-destructive text-destructive'
                title={t(
                  'Removed members leave the organization and their member quota is cleared.'
                )}
              >
                {t('Remove from organization')}
              </Button>
            )}
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
                  aria-label={t('Select all')}
                  disabled={selectableUsers.length === 0}
                  checked={
                    selectableUsers.length > 0 &&
                    selectableUsers.every((u) => selectedUserIds.has(u.id))
                  }
                  onChange={(e) => handleSelectAll(e.target.checked)}
                  className='cursor-pointer disabled:cursor-not-allowed'
                />
              </th>
              <th
                className='cursor-pointer px-3 py-2 text-left font-medium select-none'
                onClick={() => handleSort('username')}
              >
                {t('Username')}
                <SortIcon
                  col='username'
                  orderBy={orderBy}
                  orderDir={orderDir}
                />
              </th>
              <th
                className='cursor-pointer px-3 py-2 text-left font-medium select-none'
                onClick={() => handleSort('organization_role')}
              >
                {t('Role')}
                <SortIcon
                  col='organization_role'
                  orderBy={orderBy}
                  orderDir={orderDir}
                />
              </th>
              <th
                className='cursor-pointer px-3 py-2 text-left font-medium select-none'
                onClick={() => handleSort('status')}
              >
                {t('Status')}
                <SortIcon col='status' orderBy={orderBy} orderDir={orderDir} />
              </th>
              <th
                className='cursor-pointer px-3 py-2 text-left font-medium select-none'
                onClick={() => handleSort('quota')}
              >
                {t('Quota')}
                <SortIcon col='quota' orderBy={orderBy} orderDir={orderDir} />
              </th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => {
              const isOwner = user.organization_role === ORGANIZATION_ROLE.OWNER
              const editable = isRowEditable(user)
              const pendingRemove = pendingChanges.find(
                (c) => c.type === 'remove' && c.userId === user.id
              )
              const pendingStatus = pendingChanges.find(
                (c): c is StatusChange =>
                  c.type === 'status' && c.userId === user.id
              )
              const pendingQuota = pendingChanges.find(
                (c): c is QuotaChange =>
                  c.type === 'quota' && c.userId === user.id
              )
              const pendingRole = pendingChanges.find(
                (c): c is RoleChange =>
                  c.type === 'role' && c.userId === user.id
              )
              const quotaControlState = getOrganizationQuotaControlState(
                user.id,
                user.quota,
                subscriptionRecords
              )
              const hasActivePlan = quotaControlState.hasActivePlan
              const effectiveStatus = pendingStatus
                ? pendingStatus.newStatus
                : user.status
              const quotaDisabled =
                !editable || !!pendingRemove || quotaControlState.disabled
              let lockedReason: string | undefined
              if (isOwner) lockedReason = t('Owner cannot be modified')
              else if (user.id === currentUserId) {
                lockedReason = t('You cannot modify your own membership')
              }

              return (
                <tr key={user.id} className='border-t'>
                  <td className='px-3 py-2'>
                    <input
                      type='checkbox'
                      aria-label={t('Select {{name}}', {
                        name: user.username,
                      })}
                      disabled={!editable}
                      checked={editable && selectedUserIds.has(user.id)}
                      onChange={(e) =>
                        editable && handleSelectUser(user.id, e.target.checked)
                      }
                      className={
                        editable
                          ? 'cursor-pointer'
                          : 'cursor-not-allowed opacity-30'
                      }
                      title={lockedReason}
                    />
                  </td>
                  <td className='px-3 py-2'>
                    <div
                      className={`font-medium${pendingRemove ? ' text-muted-foreground line-through' : ''}`}
                    >
                      {user.username}
                    </div>
                    {user.display_name && (
                      <div className='text-muted-foreground text-xs'>
                        {user.display_name}
                      </div>
                    )}
                    {pendingRemove && (
                      <div className='text-destructive text-xs'>
                        {t('Pending removal')}
                      </div>
                    )}
                    {!pendingRemove && pendingStatus && (
                      <div className='text-xs text-orange-600'>
                        {pendingStatus.newStatus === USER_STATUS_DISABLED
                          ? t('Pending disable')
                          : t('Pending enable')}
                      </div>
                    )}
                    {!pendingRemove && !pendingStatus && pendingQuota && (
                      <div className='text-xs text-amber-700'>
                        {t('Quota modified')}
                      </div>
                    )}
                  </td>
                  <td className='px-3 py-2'>
                    {!editable || pendingRemove || !isOrganizationOwner ? (
                      <span
                        className={
                          pendingRemove
                            ? 'text-muted-foreground line-through'
                            : ''
                        }
                      >
                        {t(user.organization_role)}
                      </span>
                    ) : (
                      <Select
                        items={ASSIGNABLE_ROLES.map((r) => ({
                          value: r,
                          label: t(r),
                        }))}
                        value={
                          pendingRole
                            ? pendingRole.newRole
                            : user.organization_role
                        }
                        onValueChange={(value) => {
                          if (value === null) return
                          const newRole = value as AssignableRole
                          if (newRole === user.organization_role) {
                            removePendingChange(user.id, 'role')
                          } else {
                            setPendingChange({
                              type: 'role',
                              userId: user.id,
                              newRole,
                            })
                          }
                        }}
                      >
                        <SelectTrigger
                          className='h-7 w-28 text-xs'
                          aria-label={t('Role')}
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {ASSIGNABLE_ROLES.map((r) => (
                              <SelectItem key={r} value={r}>
                                {t(r)}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    )}
                    {pendingRole && !pendingRemove && (
                      <div className='text-xs text-amber-700'>
                        {t('Role modified')}
                      </div>
                    )}
                  </td>
                  <td
                    className={`px-3 py-2${pendingRemove ? ' text-muted-foreground line-through' : ''}`}
                  >
                    {effectiveStatus === USER_STATUS_ENABLED
                      ? t('Enabled')
                      : t('Disabled')}
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
                            pendingQuota
                              ? pendingQuota.newQuota
                              : quotaControlState.displayQuota
                          )}
                          placeholder={t('Quota amount')}
                          aria-label={t('Quota amount')}
                          title={lockedReason}
                          disabled={quotaDisabled}
                          onBlur={(event) => {
                            if (
                              editable &&
                              !shouldDisableQuotaForOrganizationSubscription(
                                user.id,
                                subscriptionRecords
                              )
                            ) {
                              handleQuotaChange(user, event.currentTarget.value)
                            }
                          }}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter') {
                              event.currentTarget.blur()
                            }
                          }}
                        />
                        <span className='text-muted-foreground text-xs'>
                          {currencyLabel}
                        </span>
                        {pendingQuota && (
                          <span className='text-muted-foreground text-xs'>
                            {t('(was {{amount}})', {
                              amount: quotaUnitsToDollars(
                                pendingQuota.originalQuota
                              ).toFixed(2),
                            })}
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
                              disabled={quotaDisabled}
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
      {totalUsers > PAGE_SIZE && (
        <div className='text-muted-foreground flex items-center justify-between text-sm'>
          <span>
            {t('Page {{current}} of {{total}}', {
              current: currentPage,
              total: totalPages,
            })}{' '}
            ({t('{{count}} users total', { count: totalUsers })})
          </span>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => setCurrentPage((p) => p - 1)}
              disabled={currentPage <= 1 || loading}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              onClick={() => setCurrentPage((p) => p + 1)}
              disabled={currentPage >= totalPages || loading}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
