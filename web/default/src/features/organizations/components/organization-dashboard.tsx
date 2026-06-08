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
import {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { VChart } from '@visactor/react-vchart'
import {
  Activity,
  BarChart3,
  Bot,
  CalendarDays,
  Database,
  MousePointerClick,
  UserRound,
  Users,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatCompactNumber, formatQuota } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { VCHART_OPTION } from '@/lib/vchart'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { ConsumptionDistributionChart } from '@/features/dashboard/components/models/consumption-distribution-chart'
import { ModelCharts } from '@/features/dashboard/components/models/model-charts'
import { processUserChartData } from '@/features/dashboard/lib'
import type {
  ProcessedUserChartData,
  QuotaDataItem,
} from '@/features/dashboard/types'
import {
  getOrganizationDashboard,
  getOrganizations,
} from '@/features/organizations/api'
import type {
  OrganizationDashboardData,
  OrganizationDashboardRangePreset,
  OrganizationDashboardTopModel,
  OrganizationDashboardTopUser,
} from '@/features/organizations/types'
import {
  getLastSelectedOrganizationId,
  resolveAvailableOrganizationId,
  saveLastSelectedOrganizationId,
} from '@/features/organizations/lib/organization-selection'

type RangeOption = {
  value: Exclude<OrganizationDashboardRangePreset, 'custom'>
  labelKey: string
}

const RANGE_OPTIONS: RangeOption[] = [
  { value: 'today', labelKey: 'Today' },
  { value: '7d', labelKey: 'Last 7 days' },
  { value: '30d', labelKey: 'Last 30 days' },
]

const ORGANIZATION_USER_CHARTS: {
  value: string
  labelKey: string
  specKey: keyof ProcessedUserChartData
}[] = [
  {
    value: 'rank',
    labelKey: 'User Consumption Ranking',
    specKey: 'spec_user_rank',
  },
  {
    value: 'trend',
    labelKey: 'User Consumption Trend',
    specKey: 'spec_user_trend',
  },
]

const TOP_USER_LIMIT_OPTIONS = [5, 10, 20, 50]

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

function getRangeTimestamps(preset: RangeOption['value']) {
  const now = new Date()
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)

  if (preset === '7d') {
    start.setDate(start.getDate() - 6)
  } else if (preset === '30d') {
    start.setDate(start.getDate() - 29)
  }

  return {
    start_timestamp: Math.floor(start.getTime() / 1000),
    end_timestamp: Math.floor(now.getTime() / 1000),
  }
}

function dateToTimestamp(value: string) {
  const date = new Date(`${value}T00:00:00`)
  if (Number.isNaN(date.getTime())) return 0

  return Math.floor(date.getTime() / 1000)
}

function toModelUsageQuotaDataItems(
  dashboard: OrganizationDashboardData,
  fallbackTimestamp: number
): QuotaDataItem[] {
  const modelUsage = dashboard.model_usage ?? []
  if (modelUsage.length > 0) {
    return modelUsage.map((item, index) => ({
      id: index,
      model_name: item.model,
      created_at: dateToTimestamp(item.date),
      count: item.requests,
      quota: item.quota,
      token_used: 0,
    }))
  }

  return dashboard.top_models.map((item, index) => ({
    id: index,
    model_name: item.model,
    created_at: fallbackTimestamp,
    count: item.requests,
    quota: item.quota,
    token_used: 0,
  }))
}

function toConsumptionQuotaDataItems(
  dashboard: OrganizationDashboardData
): QuotaDataItem[] {
  const modelUsage = dashboard.model_usage ?? []
  if (modelUsage.length > 0) {
    return modelUsage.map((item, index) => ({
      id: index,
      model_name: item.model,
      created_at: dateToTimestamp(item.date),
      count: item.requests,
      quota: item.quota,
      token_used: 0,
    }))
  }

  return dashboard.daily_usage.map((item, index) => ({
    id: index,
    model_name: 'Organization',
    created_at: dateToTimestamp(item.date),
    count: item.requests,
    quota: item.quota,
    token_used: 0,
  }))
}

function toUserQuotaDataItems(dashboard: OrganizationDashboardData) {
  const userUsage = dashboard.user_usage ?? []
  if (userUsage.length > 0) {
    return userUsage.map((item, index) => ({
      id: index,
      user_id: item.user_id,
      username: item.display_name || item.username,
      created_at: dateToTimestamp(item.date),
      count: item.requests,
      quota: item.quota,
      token_used: 0,
    }))
  }

  return dashboard.top_users.map((item, index) => ({
    id: index,
    user_id: item.user_id,
    username: item.display_name || item.username,
    created_at: dateToTimestamp(dashboard.daily_usage[0]?.date ?? ''),
    count: item.period_requests,
    quota: item.period_quota,
    token_used: 0,
  }))
}

function isDashboardEmpty(data?: OrganizationDashboardData) {
  if (!data) return true

  return (
    data.summary.period_quota === 0 &&
    data.summary.period_requests === 0 &&
    data.daily_usage.length === 0 &&
    data.top_users.length === 0 &&
    data.top_models.length === 0 &&
    (data.model_usage?.length ?? 0) === 0 &&
    (data.user_usage?.length ?? 0) === 0
  )
}

function DashboardSkeleton() {
  return (
    <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className='h-28 rounded-xl' />
        ))}
      </div>
      <Skeleton className='h-[320px] rounded-xl' />
      <div className='grid gap-4 lg:grid-cols-2'>
        <Skeleton className='h-[300px] rounded-xl' />
        <Skeleton className='h-[300px] rounded-xl' />
      </div>
    </div>
  )
}

function SummaryCard({
  title,
  value,
  icon: Icon,
}: {
  title: string
  value: string
  icon: typeof Wallet
}) {
  return (
    <Card size='sm' className='min-w-0'>
      <CardHeader className='flex-row items-center justify-between gap-3'>
        <CardTitle className='text-muted-foreground truncate text-sm font-medium'>
          {title}
        </CardTitle>
        <div className='bg-muted text-muted-foreground flex size-8 shrink-0 items-center justify-center rounded-lg'>
          <Icon className='size-4' />
        </div>
      </CardHeader>
      <CardContent>
        <div className='truncate text-xl font-semibold tabular-nums sm:text-2xl'>
          {value}
        </div>
      </CardContent>
    </Card>
  )
}

function OrganizationUserCharts({
  data,
  loading,
}: {
  data: QuotaDataItem[]
  loading?: boolean
}) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const [themeReady, setThemeReady] = useState(false)
  const [topUserLimit, setTopUserLimit] = useState(10)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }

    updateTheme()
  }, [resolvedTheme])

  const chartData = useMemo(
    () =>
      processUserChartData(
        loading ? [] : data,
        'day',
        t,
        topUserLimit,
        customization.preset
      ),
    [
      data,
      loading,
      t,
      topUserLimit,
      customization.preset,
      customization.radius,
    ]
  )

  return (
    <div className='space-y-3'>
      <div className='flex items-center gap-1.5 overflow-x-auto pb-1 sm:gap-2'>
        <Tabs
          value={String(topUserLimit)}
          onValueChange={(value) => setTopUserLimit(Number(value))}
          className='shrink-0'
        >
          <TabsList>
            <span className='text-muted-foreground px-2 text-xs font-medium whitespace-nowrap'>
              {t('Top Users')}
            </span>
            {TOP_USER_LIMIT_OPTIONS.map((limit) => (
              <TabsTrigger
                key={limit}
                value={String(limit)}
                className='px-2.5 text-xs'
              >
                {t('Top {{count}}', { count: limit })}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <div className='grid gap-4 xl:grid-cols-2'>
        {ORGANIZATION_USER_CHARTS.map((chart) => {
          const spec = chartData[chart.specKey]

          return (
            <div
              key={chart.value}
              className='overflow-hidden rounded-lg border'
            >
              <div className='flex w-full items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
                <Users className='text-muted-foreground/60 size-4' />
                <div className='text-sm font-semibold'>{t(chart.labelKey)}</div>
              </div>

              <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
                {loading ? (
                  <Skeleton className='h-full w-full' />
                ) : (
                  themeReady &&
                  spec && (
                    <VChart
                      key={`organization-user-${chart.value}-${topUserLimit}-${resolvedTheme}-${customization.preset}`}
                      spec={{
                        ...spec,
                        theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                        background: 'transparent',
                      }}
                      option={VCHART_OPTION}
                    />
                  )
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function TopUsersTable({ users }: { users: OrganizationDashboardTopUser[] }) {
  const { t } = useTranslation()

  return (
    <Card>
      <CardHeader className='border-b'>
        <div className='flex min-w-0 items-center gap-2'>
          <UserRound className='text-muted-foreground size-4 shrink-0' />
          <CardTitle className='truncate'>{t('Top users')}</CardTitle>
        </div>
      </CardHeader>
      <CardContent className='p-0'>
        {users.length === 0 ? (
          <Empty className='min-h-[220px] border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Users className='size-4' />
              </EmptyMedia>
              <EmptyTitle>{t('No usage data')}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('User')}</TableHead>
                <TableHead className='text-right'>{t('Requests')}</TableHead>
                <TableHead className='text-right'>
                  {t('Period usage')}
                </TableHead>
                <TableHead className='text-right'>
                  {t('Current quota')}
                </TableHead>
                <TableHead className='text-right'>{t('Used quota')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.map((user) => (
                <TableRow key={user.user_id}>
                  <TableCell className='max-w-56'>
                    <div className='min-w-0'>
                      <div className='truncate font-medium'>
                        {user.display_name || user.username}
                      </div>
                      {user.display_name ? (
                        <div className='text-muted-foreground truncate text-xs'>
                          {user.username}
                        </div>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell className='text-right'>
                    {formatCompactNumber(user.period_requests)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {formatQuota(user.period_quota)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {formatQuota(user.quota)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {formatQuota(user.used_quota)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function TopModelsTable({
  models,
}: {
  models: OrganizationDashboardTopModel[]
}) {
  const { t } = useTranslation()

  return (
    <Card>
      <CardHeader className='border-b'>
        <div className='flex min-w-0 items-center gap-2'>
          <Bot className='text-muted-foreground size-4 shrink-0' />
          <CardTitle className='truncate'>{t('Top models')}</CardTitle>
        </div>
      </CardHeader>
      <CardContent className='p-0'>
        {models.length === 0 ? (
          <Empty className='min-h-[220px] border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Bot className='size-4' />
              </EmptyMedia>
              <EmptyTitle>{t('No usage data')}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Model')}</TableHead>
                <TableHead className='text-right'>{t('Requests')}</TableHead>
                <TableHead className='text-right'>{t('Quota')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {models.map((model) => (
                <TableRow key={model.model}>
                  <TableCell className='max-w-72'>
                    <span className='block truncate font-medium'>
                      {model.model}
                    </span>
                  </TableCell>
                  <TableCell className='text-right'>
                    {formatCompactNumber(model.requests)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {formatQuota(model.quota)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

export function OrganizationDashboard() {
  const { t } = useTranslation()
  const currentUser = useAuthStore((s) => s.auth.user)
  const isRoot = (currentUser?.role ?? 0) >= ROLE.SUPER_ADMIN
  const [rangePreset, setRangePreset] = useState<RangeOption['value']>('7d')
  const [selectedOrganizationId, setSelectedOrganizationId] = useState(
    getLastSelectedOrganizationId
  )

  const range = useMemo(() => getRangeTimestamps(rangePreset), [rangePreset])
  const organizationsQuery = useQuery({
    queryKey: ['organizations', 'dashboard-selector'],
    queryFn: () => getOrganizations({ page: 1, size: 100 }),
    enabled: isRoot,
  })
  const organizations = organizationsQuery.data?.data?.items ?? []
  const selectedOrganizationIdNumber = Number(selectedOrganizationId)
  const selectedOrganizationName = organizations.find(
    (organization) => organization.id === selectedOrganizationIdNumber
  )?.name
  const organizationId =
    isRoot && selectedOrganizationIdNumber > 0
      ? selectedOrganizationIdNumber
      : undefined
  const canLoadDashboard = !isRoot || organizationId !== undefined

  useEffect(() => {
    if (!isRoot || organizations.length === 0) return

    const nextOrganizationId = resolveAvailableOrganizationId(
      selectedOrganizationId,
      organizations
    )
    if (!nextOrganizationId || nextOrganizationId === selectedOrganizationId) {
      return
    }

    setSelectedOrganizationId(nextOrganizationId)
    saveLastSelectedOrganizationId(nextOrganizationId)
  }, [isRoot, organizations, selectedOrganizationId])

  const handleOrganizationChange = useCallback((organizationId: string) => {
    setSelectedOrganizationId(organizationId)
    saveLastSelectedOrganizationId(organizationId)
  }, [])

  const query = useQuery({
    queryKey: [
      'organization-dashboard',
      organizationId,
      rangePreset,
      range.start_timestamp,
      range.end_timestamp,
    ],
    queryFn: () =>
      getOrganizationDashboard({
        ...range,
        preset: rangePreset,
        organization_id: organizationId,
      }),
    enabled: canLoadDashboard,
  })

  const dashboard = query.data?.success ? query.data.data : undefined
  const dashboardLoadMessage =
    query.data?.success === false ? query.data.message : undefined
  const hasDashboardLoadError = query.isError || query.data?.success === false
  const consumptionChartData = dashboard
    ? toConsumptionQuotaDataItems(dashboard)
    : []
  const modelChartData = dashboard
    ? toModelUsageQuotaDataItems(dashboard, range.end_timestamp)
    : []
  const userChartData = dashboard ? toUserQuotaDataItems(dashboard) : []
  let content: ReactNode

  if (organizationsQuery.isLoading || query.isLoading) {
    content = <DashboardSkeleton />
  } else if (isRoot && organizations.length === 0) {
    content = (
      <div className='mx-auto w-full max-w-7xl'>
        <Empty className='min-h-[360px] border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <Users className='size-4' />
            </EmptyMedia>
            <EmptyTitle>{t('No Data')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      </div>
    )
  } else if (hasDashboardLoadError) {
    content = (
      <div className='mx-auto w-full max-w-7xl'>
        <Empty className='min-h-[360px] border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <BarChart3 className='size-4' />
            </EmptyMedia>
            <EmptyTitle>{t('Failed to load dashboard')}</EmptyTitle>
            {dashboardLoadMessage ? (
              <EmptyDescription>{dashboardLoadMessage}</EmptyDescription>
            ) : null}
          </EmptyHeader>
        </Empty>
      </div>
    )
  } else if (dashboard === undefined || isDashboardEmpty(dashboard)) {
    content = (
      <div className='mx-auto w-full max-w-7xl'>
        <Empty className='min-h-[360px] border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <BarChart3 className='size-4' />
            </EmptyMedia>
            <EmptyTitle>{t('No usage data')}</EmptyTitle>
          </EmptyHeader>
        </Empty>
      </div>
    )
  } else {
    content = (
      <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
          <SummaryCard
            title={t('Organization balance')}
            value={formatQuota(dashboard.organization.quota)}
            icon={Wallet}
          />
          <SummaryCard
            title={t('Organization used quota')}
            value={formatQuota(dashboard.organization.used_quota)}
            icon={Database}
          />
          <SummaryCard
            title={t('Period usage')}
            value={formatQuota(dashboard.summary.period_quota)}
            icon={Activity}
          />
          <SummaryCard
            title={t('Period requests')}
            value={formatCompactNumber(dashboard.summary.period_requests)}
            icon={MousePointerClick}
          />
        </div>

        <div className='grid gap-4 xl:grid-cols-2'>
          <ConsumptionDistributionChart
            data={consumptionChartData}
            loading={query.isFetching}
            timeGranularity='day'
          />
          <ModelCharts
            data={modelChartData}
            loading={query.isFetching}
            timeGranularity='day'
          />
        </div>

        <OrganizationUserCharts
          data={userChartData}
          loading={query.isFetching}
        />

        <div className='grid gap-4 lg:grid-cols-2'>
          <TopUsersTable users={dashboard.top_users} />
          <TopModelsTable models={dashboard.top_models} />
        </div>
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Organization dashboard')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {isRoot ? (
          <Select
            value={selectedOrganizationId}
            onValueChange={handleOrganizationChange}
          >
            <SelectTrigger className='min-w-44 max-w-64'>
              <Users className='size-4' />
              <SelectValue placeholder={t('Organization')}>
                {selectedOrganizationName}
              </SelectValue>
            </SelectTrigger>
            <SelectContent align='end'>
              {organizations.map((organization) => (
                <SelectItem
                  key={organization.id}
                  value={String(organization.id)}
                >
                  {organization.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
        <Button
          variant='outline'
          render={
            <Link to='/organization'>
              <Users data-icon='inline-start' />
              {t('Users')}
            </Link>
          }
        />
        <Select
          value={rangePreset}
          onValueChange={(value) =>
            setRangePreset(value as RangeOption['value'])
          }
        >
          <SelectTrigger className='min-w-36'>
            <CalendarDays className='size-4' />
            <SelectValue />
          </SelectTrigger>
          <SelectContent align='end'>
            {RANGE_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.labelKey)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
