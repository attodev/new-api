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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { TopupInfo } from '@/features/wallet/types'
import { formatQuota } from '@/lib/format'

import { OrganizationWallet } from '../organization-wallet'

const api = vi.hoisted(() => ({
  calculateOrganizationAmount: vi.fn(),
  calculateOrganizationPayPalAmount: vi.fn(),
  calculateOrganizationStripeAmount: vi.fn(),
  calculateOrganizationWaffoAmount: vi.fn(),
  calculateOrganizationWaffoPancakeAmount: vi.fn(),
  getOrganizationBillingHistory: vi.fn(),
  getOrganizationWallet: vi.fn(),
  requestOrganizationCreemPayment: vi.fn(),
  requestOrganizationPayPalPayment: vi.fn(),
  requestOrganizationPayment: vi.fn(),
  requestOrganizationStripePayment: vi.fn(),
  requestOrganizationWaffoPancakePayment: vi.fn(),
  requestOrganizationWaffoPayment: vi.fn(),
}))

const topup = vi.hoisted(() => ({ info: null as TopupInfo | null }))

vi.mock('../../api', () => api)
vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: null, loading: false, error: null }),
}))
vi.mock('@/hooks/use-system-config', () => ({
  useSystemConfig: () => ({
    currency: { quotaDisplayType: 'USD', usdExchangeRate: 1 },
  }),
}))
vi.mock('@/features/wallet/hooks/use-topup-info', () => ({
  useTopupInfo: () => ({
    topupInfo: topup.info,
    presetAmounts: [],
    loading: false,
  }),
}))

const ORGANIZATION_QUOTA = 7_654_321

function topupInfo(): TopupInfo {
  return {
    enable_online_topup: true,
    enable_stripe_topup: false,
    enable_paypal_topup: true,
    paypal_min_topup: 1,
    pay_methods: [
      { name: 'Alipay', type: 'alipay', min_topup: 0 },
      { name: 'PayPal', type: 'paypal', min_topup: 0 },
    ],
    min_topup: 10,
    stripe_min_topup: 0,
    amount_options: [],
    discount: {},
    enable_redemption: true,
  }
}

beforeEach(() => {
  for (const fn of Object.values(api)) fn.mockReset()
  topup.info = topupInfo()
  api.getOrganizationWallet.mockResolvedValue({
    success: true,
    data: {
      id: 3,
      name: 'Acme Labs',
      description: '',
      status: 1,
      owner_user_id: 1,
      quota: ORGANIZATION_QUOTA,
      used_quota: 0,
    },
  })
  api.calculateOrganizationAmount.mockResolvedValue({
    success: true,
    message: 'success',
    data: '10.00',
  })
  api.calculateOrganizationPayPalAmount.mockResolvedValue({
    success: true,
    message: 'success',
    data: '12.50',
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  window.history.replaceState({}, '', '/')
})

describe('OrganizationWallet', () => {
  it('shows the organization balance and quotes with the organization calculator', async () => {
    render(<OrganizationWallet />)

    expect(
      await screen.findByText(formatQuota(ORGANIZATION_QUOTA))
    ).toBeInTheDocument()
    expect(screen.getByText('Acme Labs')).toBeInTheDocument()
    await waitFor(() =>
      expect(api.calculateOrganizationAmount).toHaveBeenCalledWith({
        amount: 10,
      })
    )
  })

  it('hides the redemption code form even when redemption is enabled', async () => {
    render(<OrganizationWallet />)

    await screen.findByText(formatQuota(ORGANIZATION_QUOTA))
    expect(
      screen.queryByPlaceholderText('Enter your redemption code')
    ).not.toBeInTheDocument()
  })

  it('pays with PayPal through the organization endpoint in the current window', async () => {
    const open = vi.fn()
    vi.stubGlobal('open', open)
    const payLink = `${window.location.origin}/#paypal-approve`
    api.requestOrganizationPayPalPayment.mockResolvedValue({
      success: true,
      message: 'success',
      data: { pay_link: payLink },
    })
    render(<OrganizationWallet />)

    await screen.findByText(formatQuota(ORGANIZATION_QUOTA))
    fireEvent.click(screen.getByRole('button', { name: 'PayPal' }))
    await waitFor(() =>
      expect(api.calculateOrganizationPayPalAmount).toHaveBeenCalledWith({
        amount: 10,
      })
    )
    fireEvent.click(
      await screen.findByRole('button', { name: 'Confirm Payment' })
    )

    await waitFor(() =>
      expect(api.requestOrganizationPayPalPayment).toHaveBeenCalledWith({
        amount: 10,
        payment_method: 'paypal',
      })
    )
    await waitFor(() => expect(window.location.hash).toBe('#paypal-approve'))
    expect(open).not.toHaveBeenCalled()
  })

  it('shows the backend error text when the organization endpoint rejects the payment', async () => {
    api.requestOrganizationPayment.mockResolvedValue({
      message: 'error',
      data: 'Only the organization owner can top up',
    })
    const toast = await import('sonner')
    const errorSpy = vi.spyOn(toast.toast, 'error')
    render(<OrganizationWallet />)

    await screen.findByText(formatQuota(ORGANIZATION_QUOTA))
    fireEvent.click(screen.getByRole('button', { name: 'Alipay' }))
    fireEvent.click(
      await screen.findByRole('button', { name: 'Confirm Payment' })
    )

    await waitFor(() =>
      expect(errorSpy).toHaveBeenCalledWith(
        'Only the organization owner can top up'
      )
    )
    errorSpy.mockRestore()
  })

  it('quotes Waffo with the organization Waffo calculator', async () => {
    topup.info = {
      ...topupInfo(),
      pay_methods: [],
      enable_paypal_topup: false,
      enable_waffo_topup: true,
      waffo_min_topup: 0,
      waffo_pay_methods: [{ name: 'Waffo Card' }],
    }
    api.calculateOrganizationWaffoAmount.mockResolvedValue({
      success: true,
      message: 'success',
      data: '9.50',
    })
    render(<OrganizationWallet />)

    await screen.findByText(formatQuota(ORGANIZATION_QUOTA))
    await waitFor(() =>
      expect(api.calculateOrganizationWaffoAmount).toHaveBeenCalledWith({
        amount: 10,
      })
    )
    expect(api.calculateOrganizationAmount).not.toHaveBeenCalled()
  })

  it('rejects a Waffo payment below the minimum top-up without calling the endpoint', async () => {
    topup.info = {
      ...topupInfo(),
      enable_waffo_topup: true,
      waffo_min_topup: 0,
      waffo_pay_methods: [{ name: 'Waffo Card' }],
    }
    const toast = await import('sonner')
    const errorSpy = vi.spyOn(toast.toast, 'error')
    render(<OrganizationWallet />)

    await screen.findByText(formatQuota(ORGANIZATION_QUOTA))
    fireEvent.change(screen.getByRole('spinbutton'), {
      target: { value: '5' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Waffo Card' }))

    await waitFor(() =>
      expect(errorSpy).toHaveBeenCalledWith('Minimum topup amount: 10')
    )
    expect(api.requestOrganizationWaffoPayment).not.toHaveBeenCalled()
    errorSpy.mockRestore()
  })
})
