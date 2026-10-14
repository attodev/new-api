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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createJSONStorage } from 'zustand/middleware'

import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { DiscountBadge } from '../components/discount-badge'
import { ModelCard } from '../components/model-card'
import { ModelDetailsContent } from '../components/model-details'
import { ModelPriceCell } from '../components/model-price-cell'
import { PricingSidebar } from '../components/pricing-sidebar'
import { discountBadgeClasses } from '../lib/discount-color'
import {
  formatFixedPrice,
  formatGroupPrice,
  formatPrice,
  formatRequestPrice,
} from '../lib/price'
import type { PricingModel } from '../types'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))

function pricingModel(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'example-model',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 3,
    enable_groups: ['default'],
    group_ratio: { default: 1 },
    ...overrides,
  }
}

const originalStorage = useSystemConfigStore.persist.getOptions().storage
beforeEach(() => {
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
    key: (index: number) => [...storage.keys()][index] ?? null,
    get length() {
      return storage.size
    },
  })
  useSystemConfigStore.persist.setOptions({
    storage: createJSONStorage(() => localStorage),
  })
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
  })
})
afterEach(() => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useSystemConfigStore.persist.setOptions({ storage: originalStorage })
})

describe('discount display', () => {
  it('maps percent ranges to heat colours', () => {
    expect(discountBadgeClasses(1)).toMatch(/emerald/)
    expect(discountBadgeClasses(10)).toMatch(/emerald/)
    expect(discountBadgeClasses(15)).toMatch(/amber/)
    expect(discountBadgeClasses(20)).toMatch(/amber/)
    expect(discountBadgeClasses(30)).toMatch(/orange/)
    expect(discountBadgeClasses(40)).toMatch(/orange/)
    expect(discountBadgeClasses(50)).toMatch(/red/)
    expect(discountBadgeClasses(60)).toMatch(/red/)
  })

  it('applies the discount percent to the formatted price', () => {
    const m = pricingModel()
    const full = formatPrice(m, 'input', 'M')
    const discounted = formatPrice(
      m,
      'input',
      'M',
      false,
      1,
      1,
      undefined,
      true,
      50
    )
    expect(full).toBe('$2')
    expect(discounted).toBe('$1')
    expect(formatPrice(m, 'input', 'M', false, 1, 1, undefined, true, 0)).toBe(
      full
    )
  })

  it('applies the discount to group, fixed and request prices', () => {
    const tokenModel = pricingModel()
    const ratios = { default: 1 }
    expect(
      formatGroupPrice(
        tokenModel,
        'default',
        'input',
        'M',
        false,
        1,
        1,
        ratios,
        25
      )
    ).toBe('$1.5')
    const requestModel = pricingModel({ quota_type: 1, model_price: 0.04 })
    expect(
      formatFixedPrice(requestModel, 'default', false, 1, 1, ratios, 25)
    ).toBe('$0.03')
    expect(
      formatRequestPrice(requestModel, false, 1, 1, undefined, true, 25)
    ).toBe('$0.03')
  })

  it('renders a percent-off badge only for a positive percent', () => {
    const { rerender } = render(<DiscountBadge percent={25} />)
    expect(screen.getByText('25% off')).toHaveClass(/amber/)
    rerender(<DiscountBadge percent={0} />)
    expect(screen.queryByText(/% off/)).not.toBeInTheDocument()
    rerender(<DiscountBadge />)
    expect(screen.queryByText(/% off/)).not.toBeInTheDocument()
  })

  it('strikes the list price on a discounted model card', () => {
    const { container } = render(
      <ModelCard model={pricingModel({ discount_percent: 25 })} tokenUnit='M' />
    )
    expect(screen.getByText('25% off')).toBeVisible()
    const struck = [...container.querySelectorAll('s')].map(
      (s) => s.textContent
    )
    expect(struck).toEqual(expect.arrayContaining(['$2', '$6']))
    expect(screen.getByText('$1.5')).toBeVisible()
    expect(screen.getByText('$4.5')).toBeVisible()
  })

  it.each([
    ['zero', 0],
    ['absent', undefined],
  ])('shows plain prices on a card when the discount is %s', (_, percent) => {
    const { container } = render(
      <ModelCard
        model={pricingModel({ discount_percent: percent })}
        tokenUnit='M'
      />
    )
    expect(container.querySelector('s')).toBeNull()
    expect(screen.queryByText(/% off/)).not.toBeInTheDocument()
    expect(screen.getByText('$2')).toBeVisible()
    expect(screen.getByText('$6')).toBeVisible()
  })

  it.each([
    ['zero', 0],
    ['absent', undefined],
  ])(
    'shows plain prices in the table cell when the discount is %s',
    (_, percent) => {
      const { container } = render(
        <ModelPriceCell
          model={pricingModel({ discount_percent: percent })}
          options={{ tokenUnit: 'M' }}
        />
      )
      expect(container.querySelector('s')).toBeNull()
      expect(screen.queryByText(/% off/)).not.toBeInTheDocument()
      expect(screen.getByText('2')).toBeVisible()
      expect(screen.getByText('6')).toBeVisible()
    }
  )

  it('shows only the badge on a dynamic-priced model card', () => {
    const { container } = render(
      <ModelCard
        model={pricingModel({
          discount_percent: 25,
          billing_mode: 'tiered_expr',
          billing_expr: 'tier("standard", p * 2 + c * 6)',
        })}
        tokenUnit='M'
      />
    )
    expect(screen.getByText('25% off')).toBeVisible()
    expect(container.querySelector('s')).toBeNull()
  })

  it('strikes the list price in the table cell', () => {
    const { container } = render(
      <ModelPriceCell
        model={pricingModel({ discount_percent: 50 })}
        options={{ tokenUnit: 'M' }}
      />
    )
    expect(screen.getByText('50% off')).toBeVisible()
    const struck = [...container.querySelectorAll('s')].map(
      (s) => s.textContent
    )
    expect(struck).toEqual(['2', '6'])
    expect(screen.getByText('1')).toBeVisible()
  })

  it('shows only the badge in the table cell of a dynamic-priced model', () => {
    const { container } = render(
      <ModelPriceCell
        model={pricingModel({
          discount_percent: 50,
          billing_mode: 'tiered_expr',
          billing_expr: 'tier("standard", p * 2 + c * 6)',
        })}
        options={{ tokenUnit: 'M' }}
      />
    )
    expect(screen.getByText('50% off')).toBeVisible()
    expect(container.querySelector('s')).toBeNull()
  })

  it("badges a vendor with the vendor's own percent", () => {
    render(
      <PricingSidebar
        quotaTypeFilter='all'
        endpointTypeFilter='all'
        vendorFilter='all'
        groupFilter='all'
        tagFilter='all'
        onQuotaTypeChange={vi.fn()}
        onEndpointTypeChange={vi.fn()}
        onVendorChange={vi.fn()}
        onGroupChange={vi.fn()}
        onTagChange={vi.fn()}
        vendors={[
          { id: 1, name: 'Vendor A', discount_percent: 10 },
          { id: 2, name: 'Vendor B' },
        ]}
        groups={[]}
        tags={[]}
        models={[
          pricingModel({ vendor_name: 'Vendor A', discount_percent: 25 }),
          pricingModel({ id: 2, vendor_name: 'Vendor B' }),
        ]}
        hasActiveFilters={false}
        onClearFilters={vi.fn()}
      />
    )
    const vendorA = screen.getByRole('button', { name: /Vendor A/ })
    expect(vendorA).toHaveTextContent('10% off')
    expect(screen.queryByText('25% off')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Vendor B/ })
    ).not.toHaveTextContent('% off')
  })

  it('shows discounted base and per-group prices in the model details', () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: { data: { groups: [] } } })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    })
    render(
      <QueryClientProvider client={client}>
        <ModelDetailsContent
          model={pricingModel({
            discount_percent: 25,
            group_ratio: { default: 2 },
          })}
          groupRatio={{ default: 2 }}
          usableGroup={{ default: { desc: '', ratio: 2 } }}
          endpointMap={{}}
          autoGroups={[]}
          priceRate={1}
          usdExchangeRate={1}
          tokenUnit='M'
        />
      </QueryClientProvider>
    )
    expect(screen.getByText('25% off')).toBeVisible()
    // Base price: $2 list input at ratio 1, 25% off.
    expect(screen.getByText('Base Price').parentElement).toHaveTextContent(
      '$1.5'
    )
    // Group table: $2 x group ratio 2 = $4 list input, 25% off.
    const row = screen
      .getAllByRole('row')
      .find((r) => within(r).queryByText('2x'))
    expect(row).toHaveTextContent('$3')
    expect(row).not.toHaveTextContent('$4')
    client.clear()
  })
})
