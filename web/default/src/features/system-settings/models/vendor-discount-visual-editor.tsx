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
import { useCallback, useEffect, useMemo, useState, memo } from 'react'
import { Code2, Eye, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { getVendors } from '@/features/models/api'
import { vendorsQueryKeys } from '@/features/models/lib'
import { safeJsonParse } from '../utils/json-parser'

type VendorDiscountVisualEditorProps = {
  vendorDiscount: string
  onChange: (field: string, value: string) => void
}

type VendorDiscountRow = {
  _id: string
  name: string
  percent: number
}

const sectionCardClassName =
  'relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border before:border-border/90'
const sectionHeaderClassName = 'border-b bg-muted/20'

let vendorDiscountIdCounter = 0
function createVendorDiscountId() {
  vendorDiscountIdCounter += 1
  return `vdr_${vendorDiscountIdCounter}`
}

function clampPercent(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return 0
  if (parsed < 0) return 0
  if (parsed > 100) return 100
  return parsed
}

function buildVendorDiscountRows(vendorDiscount: string): VendorDiscountRow[] {
  const map = safeJsonParse<Record<string, number>>(vendorDiscount, {
    fallback: {},
    context: 'vendor discounts',
  })
  return Object.entries(map).map(([name, percent]) => ({
    _id: createVendorDiscountId(),
    name,
    percent: clampPercent(percent),
  }))
}

function serializeVendorDiscountRows(rows: VendorDiscountRow[]): string {
  const map: Record<string, number> = {}
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    map[name] = clampPercent(row.percent)
  }
  return JSON.stringify(map, null, 2)
}

function vendorDiscountSignature(rows: VendorDiscountRow[]): string {
  return JSON.stringify(
    safeJsonParse(serializeVendorDiscountRows(rows), {
      fallback: {},
      silent: true,
    })
  )
}

function sourceVendorDiscountSignature(vendorDiscount: string): string {
  return JSON.stringify(
    safeJsonParse(vendorDiscount, { fallback: {}, silent: true })
  )
}

export const VendorDiscountVisualEditor = memo(
  function VendorDiscountVisualEditor({
    vendorDiscount,
    onChange,
  }: VendorDiscountVisualEditorProps) {
    const { t } = useTranslation()
    const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')

    const toggleEditMode = useCallback(() => {
      setEditMode((prev) => (prev === 'visual' ? 'json' : 'visual'))
    }, [])

    return (
      <Card className={sectionCardClassName}>
        <CardHeader className={sectionHeaderClassName}>
          <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
            <div>
              <CardTitle>{t('Vendor Discount')}</CardTitle>
              <CardDescription>
                {t(
                  "Per-vendor discount percentage (0–100). Applied only to a vendor's models that have no model-specific discount."
                )}
              </CardDescription>
            </div>
            <Button
              variant='outline'
              size='sm'
              onClick={toggleEditMode}
              className='sm:self-start'
            >
              {editMode === 'visual' ? (
                <>
                  <Code2 className='mr-2 h-4 w-4' />
                  {t('Switch to JSON')}
                </>
              ) : (
                <>
                  <Eye className='mr-2 h-4 w-4' />
                  {t('Switch to Visual')}
                </>
              )}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {editMode === 'visual' ? (
            <VendorDiscountTable
              vendorDiscount={vendorDiscount}
              onChange={onChange}
            />
          ) : (
            <VendorDiscountJsonEditor
              vendorDiscount={vendorDiscount}
              onChange={onChange}
            />
          )}
        </CardContent>
      </Card>
    )
  }
)

type VendorDiscountTableProps = {
  vendorDiscount: string
  onChange: (field: string, value: string) => void
}

function VendorDiscountTable({
  vendorDiscount,
  onChange,
}: VendorDiscountTableProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<VendorDiscountRow[]>(() =>
    buildVendorDiscountRows(vendorDiscount)
  )

  const { data: vendorsData, isError: vendorsError } = useQuery({
    queryKey: vendorsQueryKeys.list(),
    queryFn: () => getVendors({ page_size: 1000 }),
  })

  const vendorNames = useMemo(
    () => (vendorsData?.data?.items ?? []).map((vendor) => vendor.name),
    [vendorsData]
  )
  // Fall back to a free-text vendor-name input when the vendor list is
  // unavailable (request failed or returned no vendors).
  const useFreeTextVendor = vendorsError || vendorNames.length === 0

  useEffect(() => {
    const incomingSignature = sourceVendorDiscountSignature(vendorDiscount)
    setRows((currentRows) => {
      if (vendorDiscountSignature(currentRows) === incomingSignature) {
        return currentRows
      }
      return buildVendorDiscountRows(vendorDiscount)
    })
  }, [vendorDiscount])

  const emitRows = useCallback(
    (nextRows: VendorDiscountRow[]) => {
      setRows(nextRows)
      onChange('VendorDiscount', serializeVendorDiscountRows(nextRows))
    },
    [onChange]
  )

  const updateRow = useCallback(
    (id: string, field: 'name' | 'percent', value: string | number) => {
      emitRows(
        rows.map((row) => (row._id === id ? { ...row, [field]: value } : row))
      )
    },
    [emitRows, rows]
  )

  const addRow = useCallback(() => {
    const existingNames = new Set(rows.map((row) => row.name))
    let name = ''
    // Pre-select the first vendor that has not yet been added, if any.
    if (!useFreeTextVendor) {
      name = vendorNames.find((vendorName) => !existingNames.has(vendorName)) ?? ''
    }
    if (!name) {
      let index = 1
      name = `vendor_${index}`
      while (existingNames.has(name)) {
        index += 1
        name = `vendor_${index}`
      }
    }
    emitRows([
      ...rows,
      {
        _id: createVendorDiscountId(),
        name,
        percent: 0,
      },
    ])
  }, [emitRows, rows, useFreeTextVendor, vendorNames])

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
    return Array.from(counts.entries())
      .filter(([, count]) => count > 1)
      .map(([name]) => name)
  }, [rows])

  const outOfRange = useMemo(
    () =>
      rows.some((row) => {
        const parsed = Number(row.percent)
        return !Number.isFinite(parsed) || parsed < 0 || parsed > 100
      }),
    [rows]
  )

  return (
    <div className='space-y-3'>
      <Button onClick={addRow} size='sm'>
        <Plus className='mr-2 h-4 w-4' />
        {t('Add vendor')}
      </Button>
      <div className='overflow-hidden rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='min-w-56'>{t('Vendor')}</TableHead>
              <TableHead className='w-40'>{t('Discount (%)')}</TableHead>
              <TableHead className='w-16 text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={3}
                  className='text-muted-foreground h-20 text-center text-sm'
                >
                  {t('No vendor discounts yet. Add a vendor to get started.')}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => {
                const percentNumber = Number(row.percent)
                const invalidPercent =
                  !Number.isFinite(percentNumber) ||
                  percentNumber < 0 ||
                  percentNumber > 100
                const isDuplicate = duplicateNames.includes(row.name.trim())
                // When using the vendor dropdown, a stored value that no longer
                // matches any known vendor still needs to render as a usable
                // option so it is not silently dropped.
                const selectableVendorNames =
                  !useFreeTextVendor && row.name && !vendorNames.includes(row.name)
                    ? [row.name, ...vendorNames]
                    : vendorNames
                return (
                  <TableRow key={row._id}>
                    <TableCell>
                      {useFreeTextVendor ? (
                        <Input
                          value={row.name}
                          placeholder={t('openai')}
                          onChange={(event) =>
                            updateRow(row._id, 'name', event.target.value)
                          }
                          aria-invalid={isDuplicate}
                        />
                      ) : (
                        <Select
                          value={row.name}
                          onValueChange={(value) =>
                            updateRow(row._id, 'name', value)
                          }
                        >
                          <SelectTrigger aria-invalid={isDuplicate}>
                            <SelectValue placeholder={t('Select a vendor')} />
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              {selectableVendorNames.map((vendorName) => (
                                <SelectItem key={vendorName} value={vendorName}>
                                  {vendorName}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      )}
                    </TableCell>
                    <TableCell>
                      <Input
                        type='number'
                        min={0}
                        max={100}
                        step='any'
                        value={String(row.percent)}
                        onChange={(event) =>
                          updateRow(
                            row._id,
                            'percent',
                            clampPercent(event.target.value)
                          )
                        }
                        aria-invalid={invalidPercent}
                      />
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => removeRow(row._id)}
                        aria-label={t('Delete')}
                      >
                        <Trash2 className='h-4 w-4' />
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
      </div>

      {duplicateNames.length > 0 && (
        <p className='text-destructive text-sm'>
          {t('Duplicate vendor names: {{names}}', {
            names: duplicateNames.join(', '),
          })}
        </p>
      )}
      {outOfRange && (
        <p className='text-destructive text-sm'>
          {t('Discount percentages must be between 0 and 100.')}
        </p>
      )}
    </div>
  )
}

type VendorDiscountJsonEditorProps = {
  vendorDiscount: string
  onChange: (field: string, value: string) => void
}

function VendorDiscountJsonEditor({
  vendorDiscount,
  onChange,
}: VendorDiscountJsonEditorProps) {
  const { t } = useTranslation()

  return (
    <div className='space-y-2'>
      <Textarea
        rows={8}
        value={vendorDiscount}
        onChange={(event) => onChange('VendorDiscount', event.target.value)}
      />
      <p className='text-muted-foreground text-sm'>
        {t(
          'JSON map of vendor name → discount percentage (0–100), e.g. { "openai": 10 }.'
        )}
      </p>
    </div>
  )
}
