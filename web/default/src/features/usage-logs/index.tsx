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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useSidebarConfig } from '@/hooks/use-sidebar-config'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { SectionPageLayout } from '@/components/layout'
import type { NavGroup } from '@/components/layout/types'
import { getOrganizations } from '@/features/organizations/api'
import {
  getLastSelectedOrganizationId,
  resolveAvailableOrganizationId,
  saveLastSelectedOrganizationId,
} from '@/features/organizations/lib/organization-selection'
import { CacheStatsDialog } from '@/features/system-settings/general/channel-affinity/cache-stats-dialog'
import { UserInfoDialog } from './components/dialogs/user-info-dialog'
import {
  UsageLogsProvider,
  useUsageLogsContext,
} from './components/usage-logs-provider'
import { UsageLogsTable } from './components/usage-logs-table'
import {
  isUsageLogsSectionId,
  USAGE_LOGS_DEFAULT_SECTION,
  type UsageLogsSectionId,
} from './section-registry'
import type { UsageLogsScope } from './types'

const TASK_LOG_SECTIONS = ['drawing', 'task'] as const

const SECTION_META: Record<UsageLogsSectionId, { titleKey: string }> = {
  common: {
    titleKey: 'Common Logs',
  },
  drawing: {
    titleKey: 'Drawing Logs',
  },
  task: {
    titleKey: 'Task Logs',
  },
}

type UsageLogsRouteTo =
  | '/usage-logs/$section'
  | '/organization/usage-logs/$section'

interface UsageLogsProps {
  scope?: UsageLogsScope
}

function getUsageLogsRouteTo(scope: UsageLogsScope): UsageLogsRouteTo {
  return scope === 'organization'
    ? '/organization/usage-logs/$section'
    : '/usage-logs/$section'
}

function UsageLogsContent({ scope = 'user' }: UsageLogsProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const currentUser = useAuthStore((s) => s.auth.user)
  const isRootOrganizationScope =
    scope === 'organization' && (currentUser?.role ?? 0) >= ROLE.SUPER_ADMIN
  const [selectedOrganizationId, setSelectedOrganizationId] = useState(
    getLastSelectedOrganizationId
  )
  const params = useParams({ strict: false }) as { section?: string }
  const routeTo = getUsageLogsRouteTo(scope)
  const basePath =
    scope === 'organization' ? '/organization/usage-logs' : '/usage-logs'
  const activeCategory: UsageLogsSectionId =
    params.section && isUsageLogsSectionId(params.section)
      ? params.section
      : USAGE_LOGS_DEFAULT_SECTION
  const {
    selectedUserId,
    userInfoDialogOpen,
    setUserInfoDialogOpen,
    affinityTarget,
    affinityDialogOpen,
    setAffinityDialogOpen,
  } = useUsageLogsContext()
  const tabNavGroups = useMemo<NavGroup[]>(
    () => [
      {
        title: 'Task Logs',
        items: TASK_LOG_SECTIONS.map((section) => ({
          title: SECTION_META[section].titleKey,
          url: `${basePath}/${section}`,
        })),
      },
    ],
    [basePath]
  )
  const filteredTabGroups = useSidebarConfig(tabNavGroups)
  const organizationsQuery = useQuery({
    queryKey: ['organizations', 'usage-logs-selector'],
    queryFn: () => getOrganizations({ page: 1, size: 100 }),
    enabled: isRootOrganizationScope,
  })
  const organizations = organizationsQuery.data?.data?.items ?? []
  const selectedOrganizationIdNumber = Number(selectedOrganizationId)
  const selectedOrganizationName = organizations.find(
    (organization) => organization.id === selectedOrganizationIdNumber
  )?.name
  const organizationId =
    isRootOrganizationScope && selectedOrganizationIdNumber > 0
      ? selectedOrganizationIdNumber
      : undefined

  useEffect(() => {
    if (!isRootOrganizationScope || organizations.length === 0) return

    const nextOrganizationId = resolveAvailableOrganizationId(
      selectedOrganizationId,
      organizations
    )
    if (!nextOrganizationId || nextOrganizationId === selectedOrganizationId) {
      return
    }

    setSelectedOrganizationId(nextOrganizationId)
    saveLastSelectedOrganizationId(nextOrganizationId)
  }, [isRootOrganizationScope, organizations, selectedOrganizationId])

  const handleOrganizationChange = useCallback((organizationId: string) => {
    setSelectedOrganizationId(organizationId)
    saveLastSelectedOrganizationId(organizationId)
  }, [])

  const visibleSections = useMemo(
    () =>
      (filteredTabGroups[0]?.items ?? [])
        .map((item) => {
          if (!('url' in item) || typeof item.url !== 'string') return null
          return item.url.split('/').pop() ?? null
        })
        .filter((section): section is UsageLogsSectionId =>
          Boolean(section && isUsageLogsSectionId(section))
        ),
    [filteredTabGroups]
  )

  const handleSectionChange = useCallback(
    (section: string) => {
      void navigate({
        to: routeTo,
        params: { section: section as UsageLogsSectionId },
      })
    },
    [navigate, routeTo]
  )

  const pageTitleKey =
    scope === 'organization'
      ? activeCategory === 'common'
        ? 'Organization Usage Logs'
        : 'Organization Task Logs'
      : activeCategory === 'common'
        ? SECTION_META.common.titleKey
        : SECTION_META.task.titleKey
  const showTaskSwitcher =
    activeCategory !== 'common' && visibleSections.length > 1

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t(pageTitleKey)}
        </SectionPageLayout.Title>
        {isRootOrganizationScope ? (
          <SectionPageLayout.Actions>
            <Select
              value={selectedOrganizationId}
              onValueChange={handleOrganizationChange}
            >
              <SelectTrigger className='min-w-44 max-w-64'>
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
          </SectionPageLayout.Actions>
        ) : null}
        <SectionPageLayout.Content>
          <div className='space-y-4'>
            {showTaskSwitcher && (
              <Tabs value={activeCategory} onValueChange={handleSectionChange}>
                <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
                  {visibleSections.map((section) => (
                    <TabsTrigger key={section} value={section}>
                      {t(SECTION_META[section].titleKey)}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
            )}
            <UsageLogsTable
              logCategory={activeCategory}
              scope={scope}
              routeTo={routeTo}
              organizationId={organizationId}
              waitForOrganization={isRootOrganizationScope}
            />
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <UserInfoDialog
        userId={selectedUserId}
        open={userInfoDialogOpen}
        onOpenChange={setUserInfoDialogOpen}
      />

      <CacheStatsDialog
        open={affinityDialogOpen}
        onOpenChange={setAffinityDialogOpen}
        target={
          affinityTarget
            ? {
                rule_name: affinityTarget.rule_name || '',
                using_group:
                  affinityTarget.using_group ||
                  affinityTarget.selected_group ||
                  '',
                key_hint: affinityTarget.key_hint || '',
                key_fp: affinityTarget.key_fp || '',
              }
            : null
        }
      />
    </>
  )
}

export function UsageLogs({ scope = 'user' }: UsageLogsProps) {
  return (
    <UsageLogsProvider>
      <UsageLogsContent scope={scope} />
    </UsageLogsProvider>
  )
}
