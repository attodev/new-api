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
import { useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  BarChart3,
  Building2,
  Power,
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
  getAssignableOrganizationUsers,
  getOrganizationProfile,
  getOrganizationUsers,
  getOrganizationUserSubscriptions,
  getOrganizations,
  updateOrganization,
  updateOrganizationUser,
} from '../api'
import { getActiveOrganizationSubscriptionUserIds } from '../lib/organization-subscription-utils'
import type {
  Organization,
  OrganizationRole,
  OrganizationUpdatePayload,
  OrganizationUser,
  OrganizationUserSubscriptionRecord,
} from '../types'

const USER_STATUS_ENABLED = 1
const USER_STATUS_DISABLED = 2
const ORGANIZATION_ROLES: OrganizationRole[] = [
  ORGANIZATION_ROLE.MEMBER,
  ORGANIZATION_ROLE.ADMIN,
  ORGANIZATION_ROLE.OWNER,
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
  const [loading, setLoading] = useState(false)
  const [loadingCandidates, setLoadingCandidates] = useState(false)
  const [savingId, setSavingId] = useState<number | null>(null)
  const [creating, setCreating] = useState(false)
  const [assigning, setAssigning] = useState(false)
  const [organizationName, setOrganizationName] = useState('')
  const [organizationDescription, setOrganizationDescription] = useState('')
  const [organizationQuotaAmount, setOrganizationQuotaAmount] = useState('')
  const [editingOrganization, setEditingOrganization] =
    useState<Organization | null>(null)
  const [ownerUserId, setOwnerUserId] = useState('')
  const [assignUserId, setAssignUserId] = useState('')
  const [assignRole, setAssignRole] = useState<OrganizationRole>(
    ORGANIZATION_ROLE.MEMBER
  )

  const isRoot = (currentUser?.role ?? 0) >= ROLE.SUPER_ADMIN
  const isOrganizationOwner = hasOrganizationOwnerRole(
    currentUser?.organization_role
  )
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const canManageOrganizationUsers =
    Boolean(currentUser?.organization_id) || isOrganizationOwner
  const activeSubscriptionUserIds = useMemo(
    () => getActiveOrganizationSubscriptionUserIds(subscriptionRecords),
    [subscriptionRecords]
  )

  async function loadUsers() {
    if (!canManageOrganizationUsers) {
      setUsers([])
      setSubscriptionRecords([])
      return
    }
    setLoading(true)
    try {
      const [userRes, subscriptionRes] = await Promise.all([
        getOrganizationUsers({ page: 1, size: 20 }),
        getOrganizationUserSubscriptions(),
      ])
      if (userRes.success && userRes.data?.items) {
        setUsers(userRes.data.items)
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
        setOrganizationQuotaInputs(
          Object.fromEntries(
            res.data.items.map((organization) => [
              organization.id,
              String(quotaUnitsToDollars(organization.quota)),
            ])
          )
        )
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
        if (items.length > 0) {
          const firstId = String(items[0].id)
          if (isRoot && !ownerUserId) setOwnerUserId(firstId)
          if (isOrganizationOwner && !assignUserId) setAssignUserId(firstId)
        }
      } else {
        toast.error(res.message || t('Failed to load users'))
      }
    } finally {
      setLoadingCandidates(false)
    }
  }

  useEffect(() => {
    void loadUsers()
  }, [canManageOrganizationUsers])

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
    const parsedUserId = Number(assignUserId)
    if (!Number.isInteger(parsedUserId)) {
      toast.error(t('User is required'))
      return
    }

    setAssigning(true)
    try {
      const res = await assignOrganizationUser(parsedUserId, {
        organization_role: assignRole,
      })
      if (res.success) {
        toast.success(t('Organization member assigned'))
        await Promise.all([loadUsers(), loadCandidateUsers()])
      } else {
        toast.error(res.message || t('Failed to assign organization member'))
      }
    } finally {
      setAssigning(false)
    }
  }

  async function saveUser(
    user: OrganizationUser,
    payload: Parameters<typeof updateOrganizationUser>[1]
  ) {
    setSavingId(user.id)
    try {
      const res = await updateOrganizationUser(user.id, payload)
      if (res.success) {
        toast.success(t('Organization user updated'))
        await loadUsers()
      } else {
        toast.error(res.message || t('Failed to update organization user'))
      }
    } finally {
      setSavingId(null)
    }
  }

  async function saveQuota(user: OrganizationUser, quota: number) {
    if (activeSubscriptionUserIds.has(user.id)) return
    if (!Number.isFinite(quota) || quota === user.quota) return
    await saveUser(user, { quota })
  }

  async function saveDisplayQuota(user: OrganizationUser, amount: string) {
    if (!amount.trim()) return

    const value = Number(amount)
    if (!Number.isFinite(value)) return

    await saveQuota(user, parseQuotaFromDollars(value))
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

  async function handleDeleteOrganization(organization: Organization) {
    try {
      await deleteOrganization(organization.id)
      toast.success(t('Organization deleted'))
      await loadOrganizations()
    } catch (e: any) {
      toast.error(e?.response?.data?.message ?? t('Failed to delete organization'))
    }
  }

  async function toggleStatus(user: OrganizationUser) {
    await saveUser(user, {
      status:
        user.status === USER_STATUS_ENABLED
          ? USER_STATUS_DISABLED
          : USER_STATUS_ENABLED,
    })
  }

  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-3'>
        <h1 className='text-xl font-semibold'>{t('Organization Users')}</h1>
        <Button
          variant='outline'
          onClick={() => void loadUsers()}
          disabled={loading}
        >
          <RefreshCw />
          {t('Refresh')}
        </Button>
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
                <Select
                  items={candidateUsers.map((user) => ({
                    value: String(user.id),
                    label: user.username,
                  }))}
                  value={assignUserId}
                  onValueChange={(value) =>
                    value !== null && setAssignUserId(value)
                  }
                  disabled={loadingCandidates || candidateUsers.length === 0}
                >
                  <SelectTrigger className='min-w-56 flex-1'>
                    <SelectValue placeholder={t('User')} />
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
                <Select
                  items={ORGANIZATION_ROLES.map((role) => ({
                    value: role,
                    label: role,
                  }))}
                  value={assignRole}
                  onValueChange={(value) =>
                    value !== null && setAssignRole(value as OrganizationRole)
                  }
                >
                  <SelectTrigger className='w-32'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {ORGANIZATION_ROLES.map((role) => (
                        <SelectItem key={role} value={role}>
                          {t(role)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <Button
                  onClick={() => void handleAssignUser()}
                  disabled={assigning || !assignUserId}
                >
                  <UserPlus />
                  {t('Assign')}
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

      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full min-w-[720px] text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='px-3 py-2 text-left font-medium'>
                {t('Username')}
              </th>
              <th className='px-3 py-2 text-left font-medium'>{t('Role')}</th>
              <th className='px-3 py-2 text-left font-medium'>{t('Status')}</th>
              <th className='px-3 py-2 text-left font-medium'>{t('Quota')}</th>
              <th className='px-3 py-2 text-right font-medium'>
                {t('Actions')}
              </th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => {
              const hasActivePlan = activeSubscriptionUserIds.has(user.id)
              return (
                <tr key={user.id} className='border-t'>
                  <td className='px-3 py-2'>
                    <div className='font-medium'>{user.username}</div>
                    {user.display_name && (
                      <div className='text-muted-foreground text-xs'>
                        {user.display_name}
                      </div>
                    )}
                  </td>
                  <td className='px-3 py-2'>{user.organization_role}</td>
                  <td className='px-3 py-2'>
                    {user.status === USER_STATUS_ENABLED
                      ? t('Enabled')
                      : t('Disabled')}
                  </td>
                  <td className='px-3 py-2'>
                    <div className='min-w-64 space-y-2'>
                      <div className='text-muted-foreground text-xs'>
                        {t('Current quota balance')}:{' '}
                        {hasActivePlan
                          ? `${formatQuota(0)} (${t('Organization plan in use')})`
                          : formatQuota(user.quota)}
                      </div>
                      <div className='flex items-center gap-2'>
                        <Input
                          key={`${user.id}-${user.quota}-${hasActivePlan}`}
                          className='w-32'
                          type='number'
                          step={tokensOnly ? 1 : 0.01}
                          min={0}
                          defaultValue={
                            hasActivePlan ? 0 : quotaUnitsToDollars(user.quota)
                          }
                          placeholder={t('Quota amount')}
                          disabled={savingId === user.id || hasActivePlan}
                          onBlur={(event) => {
                            void saveDisplayQuota(
                              user,
                              event.currentTarget.value
                            )
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
                      </div>
                      {!tokensOnly && (
                        <div className='flex flex-wrap gap-1'>
                          {QUICK_QUOTA_AMOUNTS.map((amount) => (
                            <Button
                              key={amount}
                              type='button'
                              variant='outline'
                              size='sm'
                              disabled={savingId === user.id || hasActivePlan}
                              onClick={() =>
                                void saveQuota(
                                  user,
                                  parseQuotaFromDollars(amount)
                                )
                              }
                            >
                              {formatQuota(parseQuotaFromDollars(amount))}
                            </Button>
                          ))}
                        </div>
                      )}
                    </div>
                  </td>
                  <td className='px-3 py-2 text-right'>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => void toggleStatus(user)}
                      disabled={savingId === user.id}
                    >
                      <Power />
                      {user.status === USER_STATUS_ENABLED
                        ? t('Disable')
                        : t('Enable')}
                    </Button>
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
    </div>
  )
}
