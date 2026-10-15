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
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { UserQuotaDialog } from '../user-quota-dialog'

vi.mock('../../api', () => ({ adjustUserQuota: vi.fn() }))

function renderDialog() {
  return render(
    <UserQuotaDialog
      open
      onOpenChange={() => {}}
      userId={7}
      currentQuota={0}
      onSuccess={() => {}}
    />
  )
}

describe('UserQuotaDialog quick amounts', () => {
  const previousConfig = useSystemConfigStore.getState().config

  beforeEach(() => {
    useSystemConfigStore.getState().setConfig({
      currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
    })
  })

  afterEach(() => {
    useSystemConfigStore.getState().setConfig(previousConfig)
  })

  it('offers fixed amounts that fill the amount input', async () => {
    renderDialog()
    expect(screen.getByText('Quick amount')).toBeInTheDocument()
    for (const label of ['$1', '$5', '$10', '$100']) {
      expect(screen.getByRole('button', { name: label })).toBeInTheDocument()
    }
    await userEvent.click(screen.getByRole('button', { name: '$10' }))
    expect(screen.getByRole('spinbutton')).toHaveValue(10)
  })

  it('hides the block when quota is displayed in tokens', () => {
    useSystemConfigStore.getState().setConfig({
      currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'TOKENS' },
    })
    renderDialog()
    expect(screen.queryByText('Quick amount')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: '$10' })
    ).not.toBeInTheDocument()
  })
})
