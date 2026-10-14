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
// fork: O6 — model/vendor discount settings for the group pricing page.
import { useState } from 'react'
import type { Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import type { GroupFormValues } from './group-ratio-form'
import { ModelDiscountVisualEditor } from './model-discount-visual-editor'
import { VendorDiscountVisualEditor } from './vendor-discount-visual-editor'

type DiscountField = 'ModelDiscount' | 'VendorDiscount'

type DiscountSettingsPanelProps = {
  active: boolean
  modelDiscount: string
  vendorDiscount: string
  onChange: (field: DiscountField, value: string) => void
}

// Mounts the editors the first time the Discounts section opens (the vendor
// list is fetched only then) and keeps them mounted afterwards so unfinished
// rows survive switching sections.
export function DiscountSettingsPanel({
  active,
  modelDiscount,
  vendorDiscount,
  onChange,
}: DiscountSettingsPanelProps) {
  const [opened, setOpened] = useState(active)
  if (active && !opened) setOpened(true)
  if (!opened) return null

  return (
    <div className='grid min-w-0 gap-5'>
      <ModelDiscountVisualEditor
        value={modelDiscount}
        onChange={(json) => onChange('ModelDiscount', json)}
      />
      <VendorDiscountVisualEditor
        value={vendorDiscount}
        onChange={(json) => onChange('VendorDiscount', json)}
      />
    </div>
  )
}

export function DiscountJsonFields({
  control,
}: {
  control: Control<GroupFormValues>
}) {
  const { t } = useTranslation()
  return (
    <>
      <DiscountJsonField
        control={control}
        name='ModelDiscount'
        label={t('Model discount')}
        description={t(
          'JSON map of model name → discount percentage (0–100), for example'
        )}
        example='{ "gpt-4o": 10 }'
      />
      <DiscountJsonField
        control={control}
        name='VendorDiscount'
        label={t('Vendor discount')}
        description={t(
          'JSON map of vendor name → discount percentage (0–100), for example'
        )}
        example='{ "OpenAI": 10 }'
      />
    </>
  )
}

function DiscountJsonField(props: {
  control: Control<GroupFormValues>
  name: DiscountField
  label: string
  description: string
  example: string
}) {
  return (
    <FormField
      control={props.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            <JsonCodeEditor
              value={field.value}
              onChange={field.onChange}
              name={field.name}
              onBlur={field.onBlur}
              textareaRef={field.ref}
              heightClassName='h-40 min-h-40 max-h-40'
            />
          </FormControl>
          <FormDescription>
            {props.description} {props.example}.
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
