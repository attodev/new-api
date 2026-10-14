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
import { zodResolver } from '@hookform/resolvers/zod'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { z } from 'zod'

import { getVendors } from '@/features/models/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { positiveIntegerSchema } from '../../utils/numeric-field'
import { discountMapValidation } from '../discount-utils'
import { GroupRatioForm } from '../group-ratio-form'
import { ModelDiscountVisualEditor } from '../model-discount-visual-editor'
import { validateJsonString } from '../utils'
import { VendorDiscountVisualEditor } from '../vendor-discount-visual-editor'

vi.mock('@/features/models/api', () => ({ getVendors: vi.fn() }))

const mockedGetVendors = vi.mocked(getVendors)

function withQueryClient(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function vendorsResponse(names: string[]) {
  return {
    success: true,
    data: {
      items: names.map((name, index) => ({ id: index + 1, name })),
      total: names.length,
      page: 1,
      page_size: 1000,
    },
  } as Awaited<ReturnType<typeof getVendors>>
}

beforeEach(() => {
  mockedGetVendors.mockReset()
  mockedGetVendors.mockResolvedValue(vendorsResponse([]))
})

describe('ModelDiscountVisualEditor', () => {
  it('emits the JSON map when a row is added', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<ModelDiscountVisualEditor value='{}' onChange={onChange} />)
    await user.click(screen.getByRole('button', { name: 'Add model' }))
    const name = screen.getByRole('textbox', { name: 'Model name' })
    await user.clear(name)
    await user.type(name, 'gpt-4')
    const percent = screen.getByRole('spinbutton', { name: 'Discount (%)' })
    await user.clear(percent)
    await user.type(percent, '25')
    expect(JSON.parse(onChange.mock.lastCall?.[0])).toEqual({ 'gpt-4': 25 })
  })

  it('rejects values outside 0-100', () => {
    render(
      <ModelDiscountVisualEditor value='{"gpt-4":101}' onChange={vi.fn()} />
    )
    expect(
      screen.getByText('Discount percentages must be between 0 and 100.')
    ).toBeInTheDocument()
    expect(
      screen.getByRole('spinbutton', { name: 'Discount (%)' })
    ).toHaveAttribute('aria-invalid', 'true')
  })

  it('keeps an out-of-range value instead of clamping it so the form can reject it', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <ModelDiscountVisualEditor value='{"gpt-4":10}' onChange={onChange} />
    )
    const percent = screen.getByRole('spinbutton', { name: 'Discount (%)' })
    await user.clear(percent)
    await user.type(percent, '101')
    expect(JSON.parse(onChange.mock.lastCall?.[0])).toEqual({ 'gpt-4': 101 })
    expect(
      screen.getByText('Discount percentages must be between 0 and 100.')
    ).toBeInTheDocument()
  })

  it('removes a row', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <ModelDiscountVisualEditor
        value='{"gpt-4":10,"gpt-4o":20}'
        onChange={onChange}
      />
    )
    await user.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    expect(JSON.parse(onChange.mock.lastCall?.[0])).toEqual({ 'gpt-4o': 20 })
  })
})

describe('VendorDiscountVisualEditor', () => {
  it('falls back to a free-text vendor name when no vendors are available', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      withQueryClient(
        <VendorDiscountVisualEditor value='{}' onChange={onChange} />
      )
    )
    await waitFor(() => expect(mockedGetVendors).toHaveBeenCalled())
    await user.click(screen.getByRole('button', { name: 'Add vendor' }))
    const name = screen.getByRole('textbox', { name: 'Vendor' })
    await user.clear(name)
    await user.type(name, 'OpenAI')
    const percent = screen.getByRole('spinbutton', { name: 'Discount (%)' })
    await user.clear(percent)
    await user.type(percent, '10')
    expect(JSON.parse(onChange.mock.lastCall?.[0])).toEqual({ OpenAI: 10 })
  })

  it('pre-selects the first vendor not yet configured', async () => {
    mockedGetVendors.mockResolvedValue(vendorsResponse(['OpenAI', 'Anthropic']))
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      withQueryClient(
        <VendorDiscountVisualEditor value='{"OpenAI":10}' onChange={onChange} />
      )
    )
    await waitFor(() =>
      expect(screen.getAllByRole('combobox', { name: 'Vendor' })).toHaveLength(
        1
      )
    )
    await user.click(screen.getByRole('button', { name: 'Add vendor' }))
    expect(JSON.parse(onChange.mock.lastCall?.[0])).toEqual({
      OpenAI: 10,
      Anthropic: 0,
    })
  })
})

const discountField = (kind: 'model' | 'vendor') =>
  z
    .string()
    .refine(
      (value) => validateJsonString(value, discountMapValidation(kind)).valid,
      'invalid discount map'
    )

const schema = z.object({
  GroupRatio: z.string(),
  TopupGroupRatio: z.string(),
  UserUsableGroups: z.string(),
  GroupGroupRatio: z.string(),
  AutoGroups: z.string(),
  MaxTokenAutoGroups: positiveIntegerSchema('Enter a positive integer'),
  DefaultUseAutoGroup: z.boolean(),
  GroupSpecialUsableGroup: z.string(),
  ModelDiscount: discountField('model'),
  VendorDiscount: discountField('vendor'),
})

const defaults = {
  GroupRatio: '{"default":1}',
  TopupGroupRatio: '{}',
  UserUsableGroups: '{"default":"Standard access"}',
  GroupGroupRatio: '{}',
  AutoGroups: '["default"]',
  MaxTokenAutoGroups: 5,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
  ModelDiscount: '{}',
  VendorDiscount: '{}',
}

function Fixture(props: {
  onSave: (values: typeof defaults) => Promise<void>
  initial?: Partial<typeof defaults>
}) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  const form = useForm({
    defaultValues: { ...defaults, ...props.initial },
    resolver: zodResolver(schema),
  })
  return withQueryClient(
    <SettingsPageProvider actionsContainer={actions}>
      <div ref={setActions} />
      <GroupRatioForm form={form} onSave={props.onSave} isSaving={false} />
    </SettingsPageProvider>
  )
}

describe('discount map validation', () => {
  it.each([
    ['{}', true],
    ['{"gpt-4":0,"gpt-4o":100,"o1":12.5}', true],
    ['{"gpt-4":101}', false],
    ['{"gpt-4":-1}', false],
    ['{"gpt-4":"10"}', false],
    ['{"gpt-4":null}', false],
    ['[]', false],
    ['', false],
  ])('%s -> %s', (value, valid) => {
    expect(
      validateJsonString(value, discountMapValidation('model')).valid
    ).toBe(valid)
  })
})

describe('group settings discounts tab', () => {
  it('saves model and vendor discounts and keeps them while switching sections', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(<Fixture onSave={onSave} />)

    await user.click(screen.getByRole('tab', { name: 'Discounts' }))
    await user.click(screen.getByRole('button', { name: 'Add model' }))
    const name = screen.getByRole('textbox', { name: 'Model name' })
    await user.clear(name)
    await user.type(name, 'gpt-4')
    const [modelPercent] = screen.getAllByRole('spinbutton', {
      name: 'Discount (%)',
    })
    await user.clear(modelPercent)
    await user.type(modelPercent, '25')
    await user.click(screen.getByRole('button', { name: 'Add vendor' }))
    const vendor = screen.getByRole('textbox', { name: 'Vendor' })
    await user.clear(vendor)
    await user.type(vendor, 'OpenAI')
    const vendorPercent = screen.getAllByRole('spinbutton', {
      name: 'Discount (%)',
    })[1]
    await user.clear(vendorPercent)
    await user.type(vendorPercent, '10')

    await user.click(screen.getByRole('tab', { name: 'Pricing groups' }))
    await user.click(screen.getByRole('tab', { name: 'Discounts' }))
    expect(screen.getByRole('textbox', { name: 'Model name' })).toHaveValue(
      'gpt-4'
    )
    expect(screen.getByRole('textbox', { name: 'Vendor' })).toHaveValue(
      'OpenAI'
    )

    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    const saved = onSave.mock.calls[0][0]
    expect(JSON.parse(saved.ModelDiscount)).toEqual({ 'gpt-4': 25 })
    expect(JSON.parse(saved.VendorDiscount)).toEqual({ OpenAI: 10 })
  })

  it('blocks saving an out-of-range discount and opens the discounts section', async () => {
    const user = userEvent.setup()
    const onSave = vi.fn(async (_values: typeof defaults) => {})
    render(
      <Fixture onSave={onSave} initial={{ ModelDiscount: '{"gpt-4":101}' }} />
    )
    await user.click(
      screen.getByRole('button', { name: 'Save group settings' })
    )
    await waitFor(() =>
      expect(screen.getByRole('tab', { name: 'Discounts' })).toHaveAttribute(
        'aria-selected',
        'true'
      )
    )
    expect(
      screen.getByText('Discount percentages must be between 0 and 100.')
    ).toBeVisible()
    expect(onSave).not.toHaveBeenCalled()
  })

  it('shows both discount maps in JSON mode', async () => {
    const user = userEvent.setup()
    render(
      <Fixture
        onSave={vi.fn(async () => {})}
        initial={{
          ModelDiscount: '{"gpt-4":25}',
          VendorDiscount: '{"OpenAI":10}',
        }}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
    expect(screen.getByText('Model discount')).toBeVisible()
    expect(screen.getByText('Vendor discount')).toBeVisible()
  })
})
