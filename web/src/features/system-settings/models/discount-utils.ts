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
// fork: O6 — helpers shared by the model/vendor discount editors.
import { useCallback, useEffect, useMemo, useState } from 'react'

import { safeJsonParse } from '../utils/json-parser'

type DiscountKind = 'model' | 'vendor'

// Rows keep the percent exactly as typed so an out-of-range value stays
// visible and is rejected by the form schema instead of being clamped.
type DiscountRow = {
  _id: string
  name: string
  percent: string
}

export type DiscountEditorProps = {
  value: string
  onChange: (json: string) => void
}

export const discountCardClassName = 'min-w-0 shadow-none'
export const discountHeaderClassName = 'gap-2 border-b'

let discountRowIdCounter = 0
function createDiscountRowId() {
  discountRowIdCounter += 1
  return `dr_${discountRowIdCounter}`
}

function buildDiscountRows(value: string): DiscountRow[] {
  const map = safeJsonParse<Record<string, unknown>>(value, {
    fallback: {},
    silent: true,
  })
  if (!map || typeof map !== 'object' || Array.isArray(map)) return []
  return Object.entries(map).map(([name, percent]) => ({
    _id: createDiscountRowId(),
    name,
    percent: percent === null || percent === undefined ? '' : String(percent),
  }))
}

function percentValue(percent: string): number | null {
  if (!percent.trim()) return null
  const parsed = Number(percent)
  return Number.isFinite(parsed) ? parsed : null
}

export function isPercentInRange(percent: string): boolean {
  const parsed = percentValue(percent)
  return parsed !== null && parsed >= 0 && parsed <= 100
}

function serializeDiscountRows(rows: DiscountRow[]): string {
  const map: Record<string, number | null> = {}
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    map[name] = percentValue(row.percent)
  }
  return JSON.stringify(map, null, 2)
}

function discountSignature(value: string): string {
  return JSON.stringify(safeJsonParse(value, { fallback: {}, silent: true }))
}

// Keeps local rows (including unfinished ones) while the incoming value is
// the one this editor emitted; rebuilds them when the value changes elsewhere
// (for example from the JSON editor).
export function useDiscountRows({ value, onChange }: DiscountEditorProps) {
  const [rows, setRows] = useState<DiscountRow[]>(() =>
    buildDiscountRows(value)
  )

  useEffect(() => {
    const incoming = discountSignature(value)
    setRows((current) =>
      discountSignature(serializeDiscountRows(current)) === incoming
        ? current
        : buildDiscountRows(value)
    )
  }, [value])

  const emitRows = useCallback(
    (nextRows: DiscountRow[]) => {
      setRows(nextRows)
      onChange(serializeDiscountRows(nextRows))
    },
    [onChange]
  )

  const updateRow = useCallback(
    (id: string, field: 'name' | 'percent', next: string) => {
      emitRows(
        rows.map((row) => (row._id === id ? { ...row, [field]: next } : row))
      )
    },
    [emitRows, rows]
  )

  const addRow = useCallback(
    (name: string) => {
      emitRows([...rows, { _id: createDiscountRowId(), name, percent: '0' }])
    },
    [emitRows, rows]
  )

  const removeRow = useCallback(
    (id: string) => {
      emitRows(rows.filter((row) => row._id !== id))
    },
    [emitRows, rows]
  )

  const duplicateNames = useMemo(() => {
    const counts = new Map<string, number>()
    for (const row of rows) {
      const name = row.name.trim()
      if (!name) continue
      counts.set(name, (counts.get(name) ?? 0) + 1)
    }
    return [...counts.entries()]
      .filter(([, count]) => count > 1)
      .map(([name]) => name)
  }, [rows])

  const outOfRange = useMemo(
    () => rows.some((row) => !isPercentInRange(row.percent)),
    [rows]
  )

  return {
    rows,
    updateRow,
    addRow,
    removeRow,
    duplicateNames,
    outOfRange,
  }
}

export function nextPlaceholderName(rows: DiscountRow[], prefix: string) {
  const existingNames = new Set(rows.map((row) => row.name))
  let index = 1
  while (existingNames.has(`${prefix}_${index}`)) index += 1
  return `${prefix}_${index}`
}

export function isDiscountMap(value: unknown): boolean {
  return (
    value !== null &&
    typeof value === 'object' &&
    !Array.isArray(value) &&
    Object.values(value as Record<string, unknown>).every(
      (percent) =>
        typeof percent === 'number' &&
        Number.isFinite(percent) &&
        percent >= 0 &&
        percent <= 100
    )
  )
}

// Options for validateJsonString / createJsonStringField. The backend rejects
// an empty string, so the field is required (the visual editor emits `{}`).
export function discountMapValidation(kind: DiscountKind) {
  return {
    allowEmpty: false,
    predicate: isDiscountMap,
    predicateMessage:
      kind === 'model'
        ? 'Expected a JSON map of model name to discount percentage (0–100)'
        : 'Expected a JSON map of vendor name to discount percentage (0–100)',
  }
}
