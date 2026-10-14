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

import { useTranslation } from 'react-i18next'

import { formatUseTime } from '@/lib/format'

import type { LogOtherData } from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

/**
 * Splits a request's end-to-end time into the model-provider share and the
 * gateway overhead. Rendered only when all three timing keys were recorded
 * (absent for old logs, hijacked WebSocket sessions, and requests that never
 * reached an upstream).
 */
export function TimingBreakdownSection(props: {
  other: LogOtherData | null | undefined
  showTiming: boolean
}) {
  const { t } = useTranslation()
  const other = props.other
  if (
    !props.showTiming ||
    other?.e2e_ms == null ||
    other.llm_ms == null ||
    other.gateway_ms == null
  ) {
    return null
  }
  const share = other.e2e_ms > 0 ? (other.gateway_ms / other.e2e_ms) * 100 : 0
  return (
    <DetailSection label={t('Timing Breakdown')}>
      <DetailRow
        label={t('Model Provider')}
        value={formatUseTime(other.llm_ms / 1000)}
        mono
      />
      <DetailRow
        label={t('Gateway Overhead')}
        value={`${formatUseTime(other.gateway_ms / 1000)} (${share.toFixed(1)}%)`}
        mono
      />
    </DetailSection>
  )
}
