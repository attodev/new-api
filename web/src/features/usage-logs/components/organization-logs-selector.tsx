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
/**
 * fork: O3 — root's organization picker for the organization-scope usage logs.
 */
import { useTranslation } from 'react-i18next'

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { OrganizationLogsSelection } from '../lib/organization-logs-selection'

export function OrganizationLogsSelector(props: {
  selection: OrganizationLogsSelection
}) {
  const { t } = useTranslation()
  const { organizations, selectedOrganizationId, onChange } = props.selection
  const selectedName = organizations.find(
    (organization) => String(organization.id) === selectedOrganizationId
  )?.name

  return (
    <Select
      items={organizations.map((organization) => ({
        value: String(organization.id),
        label: organization.name,
      }))}
      value={selectedOrganizationId || null}
      onValueChange={(value) => value !== null && onChange(value)}
    >
      <SelectTrigger
        className='max-w-64 min-w-44'
        aria-label={t('Organization')}
      >
        <SelectValue placeholder={t('Organization')}>
          {selectedName}
        </SelectValue>
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {organizations.map((organization) => (
            <SelectItem key={organization.id} value={String(organization.id)}>
              {organization.name}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
