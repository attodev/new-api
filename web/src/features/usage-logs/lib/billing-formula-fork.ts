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

import type { LogOtherData } from '../types'

// fork: O6 — human-readable billing formula for the log detail dialog
// (team 67ebf4f5c, 95764079d, d32b89894). One line per billed component:
// "Input: 13 / 1M × $4 × Group Ratio 0.9 × Model discount 0.75 = $0.0000351".
// Ratio factors equal to 1 are omitted. Tiered-expression billing has its own
// breakdown and returns null here.

export type BillingFormulaLog = {
  prompt_tokens?: number
  completion_tokens?: number
  quota: number
}

export function buildBillingFormula(
  log: BillingFormulaLog,
  other: LogOtherData,
  fmtAmount: (usd: number) => string
): string[] | null {
  if (other.billing_mode === 'tiered_expr') return null

  const isPerCall =
    other.model_price != null &&
    other.model_price >= 0 &&
    other.model_price !== -1

  const userGR = other.user_group_ratio
  const isUserGR = userGR != null && Number.isFinite(userGR) && userGR !== -1
  const groupRatio = isUserGR ? (userGR as number) : (other.group_ratio ?? 1)
  const ratioLabel = isUserGR ? 'User Ratio' : 'Group Ratio'

  const discountMul = other.discount_multiplier ?? 1
  let discountLabel = 'Discount'
  if (other.discount_source === 'vendor') discountLabel = 'Vendor discount'
  else if (other.discount_source === 'model') discountLabel = 'Model discount'
  const off =
    discountMul > 0 && discountMul !== 1
      ? ` × ${discountLabel} ${Number(discountMul.toFixed(4))}`
      : ''
  const ratioFactor = groupRatio !== 1 ? ` × ${ratioLabel} ${groupRatio}` : ''
  const factors = `${ratioFactor}${off}`
  const mul = groupRatio * discountMul

  if (isPerCall) {
    const price = other.model_price as number
    return [`${fmtAmount(price)}${factors} = ${fmtAmount(price * mul)}`]
  }

  if (other.model_ratio == null) return null

  const basePrice = other.model_ratio * 2.0
  const completionRatio = other.completion_ratio ?? 0
  const cacheRatio = other.cache_ratio ?? 1
  const imageRatio = other.image_ratio ?? 1
  const audioInputSeparatePrice = other.audio_input_seperate_price ?? false

  const inputTokens = log.prompt_tokens ?? 0
  const completionTokens = log.completion_tokens ?? 0
  const cacheReadTokens = other.cache_tokens ?? 0
  const cacheWrite = other.cache_creation_tokens ?? 0
  const cacheWrite5m = other.cache_creation_tokens_5m ?? 0
  const cacheWrite1h = other.cache_creation_tokens_1h ?? 0
  const audioInputTokens = other.audio_input_token_count ?? 0
  const imageTokens = other.image && other.image_output ? other.image_output : 0
  const audioDeduct = audioInputSeparatePrice ? audioInputTokens : 0

  // Claude reports input_tokens without the cached part; OpenAI-semantic
  // usage includes it, so only the latter subtracts cache reads.
  const textInput =
    other.claude === true
      ? Math.max(inputTokens - audioDeduct - imageTokens, 0)
      : Math.max(inputTokens - cacheReadTokens - audioDeduct - imageTokens, 0)

  const lines: string[] = []
  const perMillion = (label: string, tokens: number, price: number) => {
    const amount = (tokens / 1_000_000) * price * mul
    lines.push(
      `${label}: ${tokens.toLocaleString()} / 1M × ${fmtAmount(price)}${factors} = ${fmtAmount(amount)}`
    )
  }
  const perThousand = (label: string, count: number, price: number) => {
    const amount = (count / 1000) * price * mul
    lines.push(
      `${label}: ${count} / 1K × ${fmtAmount(price)}${factors} = ${fmtAmount(amount)}`
    )
  }

  if (textInput > 0) {
    perMillion('Input', textInput, basePrice)
  }
  if (cacheReadTokens > 0) {
    perMillion('Cache Read', cacheReadTokens, basePrice * cacheRatio)
  }
  if (cacheWrite > 0 && other.cache_creation_ratio != null) {
    perMillion(
      'Cache Write',
      cacheWrite,
      basePrice * other.cache_creation_ratio
    )
  }
  if (cacheWrite5m > 0 && other.cache_creation_ratio_5m != null) {
    perMillion(
      'Cache Write (5m)',
      cacheWrite5m,
      basePrice * other.cache_creation_ratio_5m
    )
  }
  if (cacheWrite1h > 0 && other.cache_creation_ratio_1h != null) {
    perMillion(
      'Cache Write (1h)',
      cacheWrite1h,
      basePrice * other.cache_creation_ratio_1h
    )
  }
  if (imageTokens > 0) {
    perMillion('Image Input', imageTokens, basePrice * imageRatio)
  }
  if (
    audioInputSeparatePrice &&
    audioInputTokens > 0 &&
    other.audio_input_price != null
  ) {
    perMillion('Audio Input', audioInputTokens, other.audio_input_price)
  }
  if (completionTokens > 0) {
    perMillion('Output', completionTokens, basePrice * completionRatio)
  }
  if (
    other.web_search &&
    (other.web_search_call_count ?? 0) > 0 &&
    other.web_search_price
  ) {
    perThousand(
      'Web Search',
      other.web_search_call_count as number,
      other.web_search_price
    )
  }
  if (
    other.file_search &&
    (other.file_search_call_count ?? 0) > 0 &&
    other.file_search_price
  ) {
    perThousand(
      'File Search',
      other.file_search_call_count as number,
      other.file_search_price
    )
  }
  if (other.image_generation_call && other.image_generation_call_price) {
    const price = other.image_generation_call_price
    lines.push(
      `Image Generation: 1 × ${fmtAmount(price)}${factors} = ${fmtAmount(price * mul)}`
    )
  }

  return lines.length > 0 ? lines : null
}
