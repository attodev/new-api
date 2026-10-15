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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm, type UseFormReturn } from 'react-hook-form'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ClearPaymentCredentialsButton } from '../payment-credentials-clear-fork'

const mutateAsync = vi.hoisted(() => vi.fn())

vi.mock('../../hooks/use-update-option', () => ({
  useUpdateOption: () => ({ mutateAsync, isPending: false }),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

type Values = Record<string, string>

const initialValues: Values = {
  StripeApiSecret: 'sk_live_x',
  StripeWebhookSecret: 'whsec_x',
  StripePriceId: 'price_x',
  PayPalClientId: 'pp_id',
  PayPalClientSecret: 'pp_secret',
  PayPalWebhookID: 'pp_hook',
  CreemApiKey: 'creem',
}

let formRef: UseFormReturn<Values>
let initialRef: { current: Values }

function Harness({ provider }: { provider: 'stripe' | 'paypal' }) {
  const form = useForm<Values>({ defaultValues: initialValues })
  formRef = form
  initialRef = { current: { ...initialValues } }
  return (
    <ClearPaymentCredentialsButton
      provider={provider}
      form={form}
      initialRef={initialRef}
    />
  )
}

describe('ClearPaymentCredentialsButton', () => {
  beforeEach(() => {
    mutateAsync.mockReset()
    mutateAsync.mockResolvedValue({ success: true })
    vi.mocked(toast.success).mockReset()
  })

  it('clears the three Stripe options after confirmation', async () => {
    const user = userEvent.setup()
    render(<Harness provider='stripe' />)

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    expect(
      screen.getByText(
        'Clear all Stripe credentials and disable Stripe payments?'
      )
    ).toBeTruthy()
    expect(mutateAsync).not.toHaveBeenCalled()

    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Clear',
      })
    )

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(3))
    expect(mutateAsync.mock.calls.map((c) => c[0])).toEqual([
      { key: 'StripeApiSecret', value: '' },
      { key: 'StripeWebhookSecret', value: '' },
      { key: 'StripePriceId', value: '' },
    ])
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith('Credentials cleared')
    )
    expect(formRef.getValues('StripeApiSecret')).toBe('')
    expect(formRef.getValues('StripeWebhookSecret')).toBe('')
    expect(formRef.getValues('StripePriceId')).toBe('')
    expect(initialRef.current.StripeApiSecret).toBe('')
    expect(initialRef.current.StripeWebhookSecret).toBe('')
    expect(initialRef.current.StripePriceId).toBe('')
    // other providers are untouched
    expect(formRef.getValues('PayPalClientSecret')).toBe('pp_secret')
    expect(initialRef.current.PayPalClientSecret).toBe('pp_secret')
  })

  it('clears the three PayPal options after confirmation', async () => {
    const user = userEvent.setup()
    render(<Harness provider='paypal' />)

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    expect(
      screen.getByText(
        'Clear all PayPal credentials and disable PayPal payments?'
      )
    ).toBeTruthy()
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Clear',
      })
    )

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(3))
    expect(mutateAsync.mock.calls.map((c) => c[0])).toEqual([
      { key: 'PayPalClientId', value: '' },
      { key: 'PayPalClientSecret', value: '' },
      { key: 'PayPalWebhookID', value: '' },
    ])
    expect(formRef.getValues('PayPalClientSecret')).toBe('')
    expect(initialRef.current.PayPalWebhookID).toBe('')
    expect(formRef.getValues('StripeApiSecret')).toBe('sk_live_x')
  })

  it('does nothing when cancelled', async () => {
    const user = userEvent.setup()
    render(<Harness provider='stripe' />)

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Cancel',
      })
    )

    expect(mutateAsync).not.toHaveBeenCalled()
    expect(formRef.getValues('StripeApiSecret')).toBe('sk_live_x')
    expect(initialRef.current.StripeApiSecret).toBe('sk_live_x')
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('keeps form values and shows no success toast when a save fails', async () => {
    mutateAsync
      .mockResolvedValueOnce({ success: true })
      .mockRejectedValueOnce(new Error('boom'))
    const user = userEvent.setup()
    render(<Harness provider='stripe' />)

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Clear',
      })
    )

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(2))
    expect(formRef.getValues('StripeApiSecret')).toBe('sk_live_x')
    expect(initialRef.current.StripeApiSecret).toBe('sk_live_x')
    expect(toast.success).not.toHaveBeenCalled()
  })
})
