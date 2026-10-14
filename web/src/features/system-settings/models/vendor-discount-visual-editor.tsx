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
import { useQuery } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { memo, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

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
import { getVendors } from '@/features/models/api'
import { vendorsQueryKeys } from '@/features/models/lib/query-keys'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  type DiscountEditorProps,
  discountCardClassName,
  discountHeaderClassName,
  isPercentInRange,
  nextPlaceholderName,
  useDiscountRows,
} from './discount-utils'
import { DiscountRowMessages } from './model-discount-visual-editor'

export const VendorDiscountVisualEditor = memo(
  function VendorDiscountVisualEditor(props: DiscountEditorProps) {
    const { t } = useTranslation()
    const {
      rows,
      updateRow,
      addRow,
      removeRow,
      duplicateNames,
      outOfRange,
      hasEmptyName,
    } = useDiscountRows(props)

    const { data: vendorsData, isError: vendorsError } = useQuery({
      queryKey: vendorsQueryKeys.list(),
      queryFn: async () =>
        requireServerSuccess(await getVendors({ page_size: 1000 })),
    })

    const vendorNames = useMemo(
      () => (vendorsData?.data?.items ?? []).map((vendor) => vendor.name),
      [vendorsData]
    )
    // Fall back to a free-text vendor name when the vendor list is
    // unavailable (request failed or returned no vendors).
    const useFreeTextVendor = vendorsError || vendorNames.length === 0

    const handleAdd = () => {
      const existingNames = new Set(rows.map((row) => row.name))
      // Pre-select the first vendor that has not been added yet, if any.
      const firstUnused = useFreeTextVendor
        ? undefined
        : vendorNames.find((name) => !existingNames.has(name))
      addRow(firstUnused ?? nextPlaceholderName(rows, 'vendor'))
    }

    return (
      <Card className={discountCardClassName}>
        <CardHeader className={discountHeaderClassName}>
          <CardTitle>{t('Vendor discount')}</CardTitle>
          <CardDescription>
            {t(
              "Per-vendor discount percentage (0–100). Applied only to a vendor's models that have no model-specific discount."
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className='space-y-3'>
          <Button type='button' size='sm' onClick={handleAdd}>
            <Plus className='mr-2 h-4 w-4' />
            {t('Add vendor')}
          </Button>
          <div className='overflow-hidden rounded-md border'>
            <Table aria-label={t('Vendor discount')}>
              <TableHeader>
                <TableRow>
                  <TableHead className='min-w-56'>{t('Vendor')}</TableHead>
                  <TableHead className='w-40'>{t('Discount (%)')}</TableHead>
                  <TableHead className='w-16 text-right'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={3}
                      className='text-muted-foreground h-20 text-center text-sm'
                    >
                      {t(
                        'No vendor discounts yet. Add a vendor to get started.'
                      )}
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row) => {
                    const isDuplicate = duplicateNames.includes(row.name.trim())
                    // A stored vendor that is no longer in the list still
                    // renders as an option so it is not silently dropped.
                    const options =
                      row.name && !vendorNames.includes(row.name)
                        ? [row.name, ...vendorNames]
                        : vendorNames
                    return (
                      <TableRow key={row._id}>
                        <TableCell>
                          {useFreeTextVendor ? (
                            <Input
                              value={row.name}
                              placeholder='OpenAI'
                              aria-label={t('Vendor')}
                              aria-invalid={isDuplicate}
                              onChange={(event) =>
                                updateRow(row._id, 'name', event.target.value)
                              }
                            />
                          ) : (
                            <Select
                              value={row.name}
                              onValueChange={(value) =>
                                updateRow(row._id, 'name', String(value ?? ''))
                              }
                            >
                              <SelectTrigger
                                className='w-full'
                                aria-label={t('Vendor')}
                                aria-invalid={isDuplicate}
                              >
                                <SelectValue
                                  placeholder={t('Select a vendor')}
                                />
                              </SelectTrigger>
                              <SelectContent alignItemWithTrigger={false}>
                                <SelectGroup>
                                  {options.map((name) => (
                                    <SelectItem key={name} value={name}>
                                      {name}
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
                            value={row.percent}
                            aria-label={t('Discount (%)')}
                            aria-invalid={!isPercentInRange(row.percent)}
                            onChange={(event) =>
                              updateRow(row._id, 'percent', event.target.value)
                            }
                          />
                        </TableCell>
                        <TableCell className='text-right'>
                          <Button
                            type='button'
                            variant='ghost'
                            size='sm'
                            aria-label={t('Delete')}
                            onClick={() => removeRow(row._id)}
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
          <DiscountRowMessages
            duplicateMessage={
              duplicateNames.length > 0
                ? t('Duplicate vendor names: {{names}}', {
                    names: duplicateNames.join(', '),
                  })
                : undefined
            }
            outOfRange={outOfRange}
            hasEmptyName={hasEmptyName}
          />
        </CardContent>
      </Card>
    )
  }
)
