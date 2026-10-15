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
import * as React from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'

import { useUpdateOption } from '../hooks/use-update-option'

// fork: O11 (team 23d10ffb4) - wipe a payment provider's stored credentials.

const PROVIDER_KEYS = {
  stripe: ['StripeApiSecret', 'StripeWebhookSecret', 'StripePriceId'],
  paypal: ['PayPalClientId', 'PayPalClientSecret', 'PayPalWebhookID'],
} as const

type CredentialKey = (typeof PROVIDER_KEYS)[keyof typeof PROVIDER_KEYS][number]

type ClearPaymentCredentialsButtonProps = {
  provider: keyof typeof PROVIDER_KEYS
  // Structural so the section's private form-values type needs no export.
  form: { setValue: (name: CredentialKey, value: string) => void }
  initialRef: { current: Partial<Record<CredentialKey, string>> }
}

export function ClearPaymentCredentialsButton(
  props: ClearPaymentCredentialsButtonProps
) {
  const { provider, form, initialRef } = props
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [open, setOpen] = React.useState(false)
  const [clearing, setClearing] = React.useState(false)

  const question =
    provider === 'stripe'
      ? t('Clear all Stripe credentials and disable Stripe payments?')
      : t('Clear all PayPal credentials and disable PayPal payments?')

  const handleConfirm = async () => {
    setClearing(true)
    try {
      for (const key of PROVIDER_KEYS[provider]) {
        await updateOption.mutateAsync({ key, value: '' })
        // Sync per key so a mid-sequence failure leaves the form matching the
        // server for the keys that were already cleared.
        form.setValue(key, '')
        initialRef.current[key] = ''
      }
      toast.success(t('Credentials cleared'))
    } catch {
      // useUpdateOption already surfaced the server error.
    } finally {
      setClearing(false)
      setOpen(false)
    }
  }

  return (
    <div className='flex gap-2'>
      <Button
        type='button'
        variant='outline'
        onClick={() => setOpen(true)}
        disabled={clearing}
      >
        {t('Clear')}
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={t('Clear')}
        desc={question}
        confirmText={t('Clear')}
        destructive
        isLoading={clearing}
        handleConfirm={handleConfirm}
      />
    </div>
  )
}
