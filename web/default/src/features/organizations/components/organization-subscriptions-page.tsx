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
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Pencil, RefreshCw, Save, UserPlus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import {
  formatQuota,
  formatTimestamp,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  assignOrganizationUserSubscription,
  cancelOrganizationUserSubscription,
  createOrganizationSubscriptionPlan,
  disableOrganizationSubscriptionPlan,
  getOrganizationSubscriptionPlans,
  getOrganizationUserSubscriptions,
  getOrganizationUsers,
  getOrganizations,
  updateOrganizationSubscriptionPlan,
} from '../api'
import {
  getAssignableOrganizationSubscriptionUsers,
  getVisibleActiveOrganizationSubscriptionRecords,
} from '../lib/organization-subscription-utils'
import type {
  Organization,
  OrganizationSubscriptionDurationUnit,
  OrganizationSubscriptionPlan,
  OrganizationSubscriptionPlanPayload,
  OrganizationUser,
  OrganizationUserSubscriptionRecord,
} from '../types'

type PlanFormState = {
  title: string
  subtitle: string
  duration_unit: OrganizationSubscriptionDurationUnit
  duration_value: string
  custom_seconds: string
  quota_amount: string
  enabled: boolean
}

const emptyForm: PlanFormState = {
  title: '',
  subtitle: '',
  duration_unit: 'month',
  duration_value: '1',
  custom_seconds: '0',
  quota_amount: '1',
  enabled: true,
}

function toPlanPayload(form: PlanFormState): OrganizationSubscriptionPlanPayload {
  return {
    title: form.title.trim(),
    subtitle: form.subtitle.trim(),
    duration_unit: form.duration_unit,
    duration_value: Number(form.duration_value || 1),
    custom_seconds:
      form.duration_unit === 'custom' ? Number(form.custom_seconds || 0) : 0,
    enabled: form.enabled,
    upgrade_group: '',
    total_amount: parseQuotaFromDollars(Number(form.quota_amount || 0)),
    quota_reset_period: 'never',
    quota_reset_custom_seconds: 0,
  }
}

function formFromPlan(plan: OrganizationSubscriptionPlan): PlanFormState {
  return {
    title: plan.title,
    subtitle: plan.subtitle || '',
    duration_unit: plan.duration_unit,
    duration_value: String(plan.duration_value || 1),
    custom_seconds: String(plan.custom_seconds || 0),
    quota_amount: String(quotaUnitsToDollars(plan.total_amount || 0)),
    enabled: plan.enabled,
  }
}

function getSubscriptionStatusKey(status: string): string {
  switch (status) {
    case 'active':
      return 'Subscription status active'
    case 'cancelled':
      return 'Subscription status cancelled'
    case 'expired':
      return 'Subscription status expired'
    default:
      return status
  }
}

function formatPlanDuration(
  plan: OrganizationSubscriptionPlan,
  t: (key: string) => string
): string {
  if (plan.duration_unit === 'custom') {
    return `${plan.custom_seconds} ${t('seconds')}`
  }
  return `${plan.duration_value} ${t(plan.duration_unit)}`
}

export function OrganizationSubscriptionsPage() {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const isRoot = (user?.role ?? 0) >= ROLE.SUPER_ADMIN
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [organizationId, setOrganizationId] = useState<number | null>(null)
  const [plans, setPlans] = useState<OrganizationSubscriptionPlan[]>([])
  const [records, setRecords] = useState<OrganizationUserSubscriptionRecord[]>(
    []
  )
  const [users, setUsers] = useState<OrganizationUser[]>([])
  const [form, setForm] = useState<PlanFormState>(emptyForm)
  const [editingPlanId, setEditingPlanId] = useState<number | null>(null)
  const [selectedUserId, setSelectedUserId] = useState('')
  const [selectedPlanId, setSelectedPlanId] = useState('')
  const [loading, setLoading] = useState(false)

  const activePlans = useMemo(
    () => plans.filter((plan) => plan.enabled),
    [plans]
  )
  const activeRecords = useMemo(
    () => getVisibleActiveOrganizationSubscriptionRecords(records),
    [records]
  )
  const assignableUsers = useMemo(
    () => getAssignableOrganizationSubscriptionUsers(users, records),
    [records, users]
  )

  const loadOrganizations = useCallback(async () => {
    if (!isRoot) return
    const res = await getOrganizations({ page: 1, size: 100 })
    if (res.success && res.data?.items?.length) {
      setOrganizations(res.data.items)
      setOrganizationId((current) => current ?? res.data!.items[0].id)
    }
  }, [isRoot])

  const loadData = useCallback(async () => {
    if (isRoot && !organizationId) return
    try {
      setLoading(true)
      const params = { organization_id: organizationId }
      const [planRes, recordRes, userRes] = await Promise.all([
        getOrganizationSubscriptionPlans(params),
        getOrganizationUserSubscriptions(params),
        getOrganizationUsers({
          page: 1,
          size: 500,
          organization_id: organizationId,
        }),
      ])
      if (planRes.success) setPlans(planRes.data || [])
      if (recordRes.success) setRecords(recordRes.data || [])
      if (userRes.success) setUsers(userRes.data?.items || [])
    } catch (error) {
      console.error('Failed to load organization subscriptions:', error)
      toast.error(t('Failed to load organization subscriptions'))
    } finally {
      setLoading(false)
    }
  }, [isRoot, organizationId, t])

  useEffect(() => {
    void loadOrganizations()
  }, [loadOrganizations])

  useEffect(() => {
    void loadData()
  }, [loadData])

  const submitPlan = async () => {
    const payload = toPlanPayload(form)
    if (!payload.title) {
      toast.error(t('Plan title is required'))
      return
    }
    const res = editingPlanId
      ? await updateOrganizationSubscriptionPlan(
          editingPlanId,
          payload,
          organizationId
        )
      : await createOrganizationSubscriptionPlan(payload, organizationId)
    if (!res.success) {
      toast.error(res.message || t('Failed to save organization plan'))
      return
    }
    toast.success(t('Organization plan saved'))
    setForm(emptyForm)
    setEditingPlanId(null)
    await loadData()
  }

  const assignPlan = async () => {
    const userId = Number(selectedUserId)
    const planId = Number(selectedPlanId)
    if (!userId || !planId) {
      toast.error(t('Select a user and plan'))
      return
    }
    const res = await assignOrganizationUserSubscription(
      userId,
      planId,
      organizationId
    )
    if (!res.success) {
      toast.error(res.message || t('Failed to assign organization plan'))
      return
    }
    toast.success(t('Organization plan assigned'))
    setSelectedUserId('')
    setSelectedPlanId('')
    await loadData()
  }

  const cancelPlan = async (userId: number) => {
    const res = await cancelOrganizationUserSubscription(userId, organizationId)
    if (!res.success) {
      toast.error(res.message || t('Failed to cancel organization plan'))
      return
    }
    toast.success(t('Organization plan cancelled'))
    await loadData()
  }

  const disablePlan = async (planId: number) => {
    const res = await disableOrganizationSubscriptionPlan(planId, organizationId)
    if (!res.success) {
      toast.error(res.message || t('Failed to disable organization plan'))
      return
    }
    toast.success(t('Organization plan disabled'))
    await loadData()
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Organization Subscription')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
          <div className='flex flex-wrap items-center justify-between gap-3'>
            {isRoot && (
              <NativeSelect
                value={organizationId ?? ''}
                onChange={(event) =>
                  setOrganizationId(Number(event.currentTarget.value) || null)
                }
                className='w-full sm:w-72'
              >
                {organizations.map((org) => (
                  <NativeSelectOption key={org.id} value={org.id}>
                    {org.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            )}
            <Button
              variant='outline'
              size='sm'
              onClick={() => void loadData()}
              disabled={loading}
            >
              <RefreshCw className='size-4' />
              {t('Refresh')}
            </Button>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>{t('Plan configuration')}</CardTitle>
              <CardDescription>
                {t('Create organization-only plans for member limits.')}
              </CardDescription>
              <CardAction>
                {editingPlanId && (
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => {
                      setEditingPlanId(null)
                      setForm(emptyForm)
                    }}
                  >
                    <X className='size-4' />
                    {t('Cancel')}
                  </Button>
                )}
              </CardAction>
            </CardHeader>
            <CardContent className='space-y-3'>
              <div className='grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]'>
                <Field label={t('Plan title')}>
                  <Input
                    value={form.title}
                    onChange={(event) =>
                      setForm((prev) => ({
                        ...prev,
                        title: event.target.value,
                      }))
                    }
                  />
                </Field>
                <Field label={t('Subtitle')}>
                  <Input
                    value={form.subtitle}
                    onChange={(event) =>
                      setForm((prev) => ({
                        ...prev,
                        subtitle: event.target.value,
                      }))
                    }
                  />
                </Field>
                <div className='flex items-end'>
                  <Button onClick={() => void submitPlan()}>
                    <Save className='size-4' />
                    {editingPlanId ? t('Update') : t('Create')}
                  </Button>
                </div>
              </div>
              <div className='grid gap-3 md:grid-cols-3'>
                <Field label={t('Quota amount')}>
                  <Input
                    type='number'
                    min='0'
                    step='0.01'
                    value={form.quota_amount}
                    onChange={(event) =>
                      setForm((prev) => ({
                        ...prev,
                        quota_amount: event.target.value,
                      }))
                    }
                  />
                </Field>
                <Field label={t('Duration')}>
                  <div className='flex gap-2'>
                    <Input
                      type='number'
                      min='1'
                      value={form.duration_value}
                      onChange={(event) =>
                        setForm((prev) => ({
                          ...prev,
                          duration_value: event.target.value,
                        }))
                      }
                    />
                    <NativeSelect
                      value={form.duration_unit}
                      onChange={(event) =>
                        setForm((prev) => ({
                          ...prev,
                          duration_unit: event.target
                            .value as OrganizationSubscriptionDurationUnit,
                          custom_seconds:
                            event.target.value === 'custom'
                              ? prev.custom_seconds === '0'
                                ? '3600'
                                : prev.custom_seconds
                              : '0',
                        }))
                      }
                      className='w-32 shrink-0'
                    >
                      {['month', 'day', 'hour', 'year', 'custom'].map(
                        (unit) => (
                          <NativeSelectOption key={unit} value={unit}>
                            {t(unit)}
                          </NativeSelectOption>
                        )
                      )}
                    </NativeSelect>
                  </div>
                </Field>
                <Field label={t('Custom seconds')}>
                  <Input
                    type='number'
                    min='1'
                    disabled={form.duration_unit !== 'custom'}
                    value={form.custom_seconds}
                    onChange={(event) =>
                      setForm((prev) => ({
                        ...prev,
                        custom_seconds: event.target.value,
                      }))
                    }
                  />
                </Field>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('Organization plans')}</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Plan')}</TableHead>
                    <TableHead>{t('Quota')}</TableHead>
                    <TableHead>{t('Duration')}</TableHead>
                    <TableHead className='w-36'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {plans.map((plan) => (
                    <TableRow key={plan.id}>
                      <TableCell>
                        <div className='flex items-center gap-2'>
                          <span className='font-medium'>{plan.title}</span>
                          {!plan.enabled && (
                            <span className='text-muted-foreground text-xs'>
                              {t('Disabled')}
                            </span>
                          )}
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          {plan.subtitle || `#${plan.id}`}
                        </div>
                      </TableCell>
                      <TableCell>
                        {plan.total_amount > 0
                          ? formatQuota(plan.total_amount)
                          : t('Unlimited')}
                      </TableCell>
                      <TableCell>{formatPlanDuration(plan, t)}</TableCell>
                      <TableCell>
                        <div className='flex gap-1'>
                          <Button
                            variant='ghost'
                            size='icon'
                            onClick={() => {
                              setEditingPlanId(plan.id)
                              setForm(formFromPlan(plan))
                            }}
                          >
                            <Pencil className='size-4' />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon'
                            onClick={() => void disablePlan(plan.id)}
                          >
                            <X className='size-4' />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t('Member plan assignment')}</CardTitle>
              <CardDescription>
                {t('Members without a plan continue to use allocated quota.')}
              </CardDescription>
            </CardHeader>
            <CardContent className='flex flex-col gap-4'>
              <div className='grid gap-2 md:grid-cols-[1fr_1fr_auto]'>
                <NativeSelect
                  value={selectedUserId}
                  onChange={(event) => setSelectedUserId(event.target.value)}
                  className='w-full'
                >
                  <NativeSelectOption value=''>{t('Select user')}</NativeSelectOption>
                  {assignableUsers.map((member) => (
                    <NativeSelectOption key={member.id} value={member.id}>
                      {member.display_name || member.username}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <NativeSelect
                  value={selectedPlanId}
                  onChange={(event) => setSelectedPlanId(event.target.value)}
                  className='w-full'
                >
                  <NativeSelectOption value=''>{t('Select plan')}</NativeSelectOption>
                  {activePlans.map((plan) => (
                    <NativeSelectOption key={plan.id} value={plan.id}>
                      {plan.title}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <Button onClick={() => void assignPlan()}>
                  <UserPlus className='size-4' />
                  {t('Assign')}
                </Button>
              </div>

              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('User')}</TableHead>
                    <TableHead>{t('Plan')}</TableHead>
                    <TableHead>{t('Used')}</TableHead>
                    <TableHead>{t('Expires')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead className='w-28'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {activeRecords.map((record) => {
                    const sub = record.subscription
                    const member = record.user
                    const plan = record.plan
                    const remain =
                      sub.amount_total > 0
                        ? Math.max(sub.amount_total - sub.amount_used, 0)
                        : 0
                    return (
                      <TableRow key={sub.id}>
                        <TableCell>
                          {member?.display_name || member?.username || sub.user_id}
                        </TableCell>
                        <TableCell>{plan?.title || `#${sub.plan_id}`}</TableCell>
                        <TableCell>
                          {sub.amount_total > 0
                            ? `${formatQuota(sub.amount_used)} / ${formatQuota(
                                sub.amount_total
                              )} (${formatQuota(remain)})`
                            : formatQuota(sub.amount_used)}
                        </TableCell>
                        <TableCell>{formatTimestamp(sub.end_time)}</TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              sub.status === 'active' ? 'default' : 'secondary'
                            }
                          >
                            {t(getSubscriptionStatusKey(sub.status))}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {sub.status === 'active' && (
                            <Button
                              variant='ghost'
                              size='icon'
                              onClick={() => void cancelPlan(sub.user_id)}
                            >
                              <X className='size-4' />
                            </Button>
                          )}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function Field(props: { label: string; children: ReactNode }) {
  return (
    <div className='grid gap-1.5'>
      <Label>{props.label}</Label>
      {props.children}
    </div>
  )
}
