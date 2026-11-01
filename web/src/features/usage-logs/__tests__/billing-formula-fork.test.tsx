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
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { BillingFormula } from '../components/dialogs/billing-formula-fork'
import { buildBillingFormula } from '../lib/billing-formula-fork'
import type { LogOtherData } from '../types'

afterEach(cleanup)

const usd = (v: number) => `$${Number(v.toFixed(7))}`

describe('buildBillingFormula (fork: O6)', () => {
  it('writes one line per component with group ratio and discount factors', () => {
    const lines = buildBillingFormula(
      { prompt_tokens: 13, completion_tokens: 4, quota: 59 },
      {
        model_ratio: 2,
        completion_ratio: 5,
        group_ratio: 0.9,
        discount_multiplier: 0.75,
        discount_source: 'model',
      } as LogOtherData,
      usd
    )
    expect(lines).toEqual([
      'Input: 13 / 1M × $4 × Group Ratio 0.9 × Model discount 0.75 = $0.0000351',
      'Output: 4 / 1M × $20 × Group Ratio 0.9 × Model discount 0.75 = $0.000054',
    ])
  })

  it('omits ratio factors equal to 1 and uses the user ratio when set', () => {
    const lines = buildBillingFormula(
      { prompt_tokens: 1000, completion_tokens: 0, quota: 1 },
      {
        model_ratio: 0.5,
        completion_ratio: 2,
        group_ratio: 1,
        user_group_ratio: 0.5,
      } as LogOtherData,
      usd
    )
    expect(lines).toEqual(['Input: 1,000 / 1M × $1 × User Ratio 0.5 = $0.0005'])
  })

  it('subtracts cache reads from OpenAI-semantic prompt tokens but not from Claude', () => {
    const base = {
      model_ratio: 1,
      completion_ratio: 1,
      cache_ratio: 0.1,
      cache_tokens: 400,
    }
    const openai = buildBillingFormula(
      { prompt_tokens: 1000, completion_tokens: 0, quota: 1 },
      base as LogOtherData,
      usd
    )
    const claude = buildBillingFormula(
      { prompt_tokens: 1000, completion_tokens: 0, quota: 1 },
      { ...base, claude: true } as LogOtherData,
      usd
    )
    expect(openai?.[0]).toBe('Input: 600 / 1M × $2 = $0.0012')
    expect(claude?.[0]).toBe('Input: 1,000 / 1M × $2 = $0.002')
    expect(openai?.[1]).toBe('Cache Read: 400 / 1M × $0.2 = $0.00008')
  })

  it('prices OpenAI 12-hour cache writes apart from the 30-minute ones', () => {
    // cache_creation_tokens counts 30-minute and 12-hour writes together;
    // OpenAI-semantic prompt tokens include both reads and writes.
    const lines = buildBillingFormula(
      { prompt_tokens: 10_000, completion_tokens: 100, quota: 1 },
      {
        model_ratio: 1,
        completion_ratio: 4,
        cache_ratio: 0.1,
        cache_tokens: 2000,
        cache_creation_tokens: 3000,
        cache_creation_ratio: 1.25,
        cache_creation_tokens_12h: 1000,
        cache_creation_ratio_12h: 2,
      } as LogOtherData,
      usd
    )
    expect(lines).toEqual([
      'Input: 5,000 / 1M × $2 = $0.01',
      'Cache Read: 2,000 / 1M × $0.2 = $0.0004',
      'Cache Write (30m): 2,000 / 1M × $2.5 = $0.005',
      'Cache Write (12h): 1,000 / 1M × $4 = $0.004',
      'Output: 100 / 1M × $8 = $0.0008',
    ])
  })

  it('omits the 30-minute line when every cache write is a 12-hour one', () => {
    const lines = buildBillingFormula(
      { prompt_tokens: 1500, completion_tokens: 0, quota: 1 },
      {
        model_ratio: 1,
        cache_creation_tokens: 500,
        cache_creation_ratio: 1.25,
        cache_creation_tokens_12h: 500,
        cache_creation_ratio_12h: 2,
        group_ratio: 0.5,
      } as LogOtherData,
      usd
    )
    expect(lines).toEqual([
      'Input: 1,000 / 1M × $2 × Group Ratio 0.5 = $0.001',
      'Cache Write (12h): 500 / 1M × $4 × Group Ratio 0.5 = $0.001',
    ])
  })

  it('keeps a single cache write line without 12-hour writes', () => {
    const base = {
      model_ratio: 1,
      cache_creation_tokens: 300,
      cache_creation_ratio: 1.25,
    }
    const openai = buildBillingFormula(
      { prompt_tokens: 1000, completion_tokens: 0, quota: 1 },
      base as LogOtherData,
      usd
    )
    expect(openai).toEqual([
      'Input: 700 / 1M × $2 = $0.0014',
      'Cache Write: 300 / 1M × $2.5 = $0.00075',
    ])
    // Claude input_tokens already exclude cache writes.
    const claude = buildBillingFormula(
      { prompt_tokens: 1000, completion_tokens: 0, quota: 1 },
      { ...base, claude: true } as LogOtherData,
      usd
    )
    expect(claude).toEqual([
      'Input: 1,000 / 1M × $2 = $0.002',
      'Cache Write: 300 / 1M × $2.5 = $0.00075',
    ])
  })

  it('does not count Claude 5m/1h cache writes twice', () => {
    const lines = buildBillingFormula(
      { prompt_tokens: 100, completion_tokens: 0, quota: 1 },
      {
        claude: true,
        model_ratio: 1,
        cache_creation_tokens: 600,
        cache_creation_ratio: 1.25,
        cache_creation_tokens_5m: 200,
        cache_creation_ratio_5m: 1.25,
        cache_creation_tokens_1h: 400,
        cache_creation_ratio_1h: 2,
      } as LogOtherData,
      usd
    )
    expect(lines).toEqual([
      'Input: 100 / 1M × $2 = $0.0002',
      'Cache Write (5m): 200 / 1M × $2.5 = $0.0005',
      'Cache Write (1h): 400 / 1M × $4 = $0.0016',
    ])
  })

  it('shows a single line for per-call pricing and nothing for tiered expressions', () => {
    expect(
      buildBillingFormula(
        { quota: 1 },
        { model_price: 0.002, group_ratio: 0.9 } as LogOtherData,
        usd
      )
    ).toEqual(['$0.002 × Group Ratio 0.9 = $0.0018'])
    expect(
      buildBillingFormula(
        { prompt_tokens: 10, quota: 1 },
        { billing_mode: 'tiered_expr', model_ratio: 1 } as LogOtherData,
        usd
      )
    ).toBeNull()
  })
})

describe('BillingFormula component', () => {
  it('is collapsed until the toggle is clicked', () => {
    render(
      <BillingFormula
        log={{ prompt_tokens: 13, completion_tokens: 4, quota: 59 }}
        other={
          {
            model_ratio: 2,
            completion_ratio: 5,
            group_ratio: 0.9,
          } as LogOtherData
        }
        fmtAmount={usd}
      />
    )
    expect(screen.queryByText(/^Input:/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Show formula' }))
    expect(
      screen.getByText('Input: 13 / 1M × $4 × Group Ratio 0.9 = $0.0000468')
    ).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Hide formula' })).toBeTruthy()
  })
})
