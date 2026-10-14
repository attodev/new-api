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
import { render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import type { ReactElement } from 'react'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import { formatQuota } from '@/lib/format'

import type { UserWalletData } from '../../types'
import { WalletStatsCard } from '../wallet-stats-card'

const i18n = createInstance()

const user: UserWalletData = {
  id: 2,
  username: 'member',
  quota: 100,
  used_quota: 20,
  request_count: 7,
  aff_quota: 0,
  aff_history_quota: 0,
  aff_count: 0,
  group: 'default',
}

beforeEach(async () => {
  await i18n.use(initReactI18next).init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })
})

function renderWithI18n(element: ReactElement) {
  return render(<I18nextProvider i18n={i18n}>{element}</I18nextProvider>)
}

describe('WalletStatsCard balanceDisplay', () => {
  it('replaces the balance figure while keeping usage and request stats', () => {
    renderWithI18n(
      <WalletStatsCard
        user={user}
        balanceDisplay={{
          value: 'Organization plan in use',
          description: 'Balance is managed by the assigned organization plan',
        }}
      />
    )

    expect(screen.getByText('Organization plan in use')).toBeInTheDocument()
    expect(
      screen.getByText('Balance is managed by the assigned organization plan')
    ).toBeInTheDocument()
    expect(screen.queryByText('Remaining quota')).not.toBeInTheDocument()
    expect(screen.getByText('Total Usage')).toBeInTheDocument()
    expect(screen.getByText('API Requests')).toBeInTheDocument()
    expect(screen.getByText('7')).toBeInTheDocument()
  })

  it('shows the balance figure when no balanceDisplay is given', () => {
    renderWithI18n(<WalletStatsCard user={user} />)

    expect(screen.getByText('Remaining quota')).toBeInTheDocument()
    expect(
      screen.queryByText('Organization plan in use')
    ).not.toBeInTheDocument()
    expect(screen.getByText(formatQuota(user.quota))).toBeInTheDocument()
  })
})
