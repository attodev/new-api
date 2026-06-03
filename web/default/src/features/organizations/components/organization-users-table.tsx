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
import { Power, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getOrganizationUsers, updateOrganizationUser } from '../api'
import type { OrganizationUser } from '../types'

const USER_STATUS_ENABLED = 1
const USER_STATUS_DISABLED = 2

export function OrganizationUsersTable() {
  const { t } = useTranslation()
  const [users, setUsers] = useState<OrganizationUser[]>([])
  const [loading, setLoading] = useState(false)
  const [savingId, setSavingId] = useState<number | null>(null)

  async function loadUsers() {
    setLoading(true)
    try {
      const res = await getOrganizationUsers({ page: 1, size: 20 })
      if (res.success && res.data?.items) {
        setUsers(res.data.items)
      } else {
        toast.error(res.message || t('Failed to load organization users'))
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadUsers()
  }, [])

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
    if (!Number.isFinite(quota) || quota === user.quota) return
    await saveUser(user, { quota })
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

      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full min-w-[720px] text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='px-3 py-2 text-left font-medium'>
                {t('Username')}
              </th>
              <th className='px-3 py-2 text-left font-medium'>{t('Role')}</th>
              <th className='px-3 py-2 text-left font-medium'>
                {t('Status')}
              </th>
              <th className='px-3 py-2 text-left font-medium'>{t('Quota')}</th>
              <th className='px-3 py-2 text-right font-medium'>
                {t('Actions')}
              </th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.id} className='border-t'>
                <td className='px-3 py-2'>
                  <div className='font-medium'>{user.username}</div>
                  {user.display_name && (
                    <div className='text-xs text-muted-foreground'>
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
                  <Input
                    className='w-32'
                    type='number'
                    defaultValue={user.quota}
                    disabled={savingId === user.id}
                    onBlur={(event) => {
                      const value = Number(event.currentTarget.value)
                      void saveQuota(user, value)
                    }}
                  />
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
            ))}
            {!loading && users.length === 0 && (
              <tr>
                <td
                  className='px-3 py-8 text-center text-muted-foreground'
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
