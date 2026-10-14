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

import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { TimingBreakdownSection } from '../components/dialogs/timing-breakdown-section'
import { effectiveUseTimeSec } from '../lib/timing-breakdown'
import type { LogOtherData } from '../types'

afterEach(cleanup)

const full: LogOtherData = { e2e_ms: 3200, llm_ms: 2400, gateway_ms: 800 }

describe('TimingBreakdownSection', () => {
  it('renders provider and gateway rows with the gateway share', () => {
    render(<TimingBreakdownSection other={full} showTiming />)
    expect(screen.getByText('Timing Breakdown')).toBeInTheDocument()
    expect(screen.getByText('Model Provider')).toBeInTheDocument()
    expect(screen.getByText('Gateway Overhead')).toBeInTheDocument()
    expect(screen.getByText(/25\.0%/)).toBeInTheDocument()
  })

  it('hides the section when the keys are absent', () => {
    render(<TimingBreakdownSection other={{}} showTiming />)
    expect(screen.queryByText('Timing Breakdown')).toBeNull()
  })

  it('hides the section when no upstream call was timed', () => {
    render(<TimingBreakdownSection other={{ e2e_ms: 3200 }} showTiming />)
    expect(screen.queryByText('Timing Breakdown')).toBeNull()
  })

  it('hides the section for non-timing log types', () => {
    render(<TimingBreakdownSection other={full} showTiming={false} />)
    expect(screen.queryByText('Timing Breakdown')).toBeNull()
  })

  it('guards a zero e2e_ms against division by zero', () => {
    render(
      <TimingBreakdownSection
        other={{ e2e_ms: 0, llm_ms: 0, gateway_ms: 0 }}
        showTiming
      />
    )
    expect(screen.getByText(/\(0\.0%\)/)).toBeInTheDocument()
  })

  it('renders nothing for null other', () => {
    render(<TimingBreakdownSection other={null} showTiming />)
    expect(screen.queryByText('Timing Breakdown')).toBeNull()
  })
})

describe('effectiveUseTimeSec', () => {
  it('prefers the precise e2e_ms', () => {
    expect(effectiveUseTimeSec(full, 3)).toBe(3.2)
  })

  it('falls back to use_time for old logs', () => {
    expect(effectiveUseTimeSec({}, 3)).toBe(3)
    expect(effectiveUseTimeSec(null, 3)).toBe(3)
    expect(effectiveUseTimeSec(undefined, 3)).toBe(3)
  })

  it('keeps an explicit zero e2e_ms', () => {
    expect(effectiveUseTimeSec({ e2e_ms: 0 }, 3)).toBe(0)
  })
})
