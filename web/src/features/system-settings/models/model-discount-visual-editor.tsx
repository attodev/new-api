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
import { Plus, Trash2 } from 'lucide-react'
import { memo } from 'react'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  type DiscountEditorProps,
  discountCardClassName,
  discountHeaderClassName,
  isPercentInRange,
  nextPlaceholderName,
  useDiscountRows,
} from './discount-utils'

export const ModelDiscountVisualEditor = memo(
  function ModelDiscountVisualEditor(props: DiscountEditorProps) {
    const { t } = useTranslation()
    const { rows, updateRow, addRow, removeRow, duplicateNames, outOfRange } =
      useDiscountRows(props)

    return (
      <Card className={discountCardClassName}>
        <CardHeader className={discountHeaderClassName}>
          <CardTitle>{t('Model discount')}</CardTitle>
          <CardDescription>
            {t(
              'Per-model discount percentage (0–100). When a model has its own discount it takes precedence over any vendor discount.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className='space-y-3'>
          <Button
            type='button'
            size='sm'
            onClick={() => addRow(nextPlaceholderName(rows, 'model'))}
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add model')}
          </Button>
          <div className='overflow-hidden rounded-md border'>
            <Table aria-label={t('Model discount')}>
              <TableHeader>
                <TableRow>
                  <TableHead className='min-w-56'>{t('Model name')}</TableHead>
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
                      {t('No model discounts yet. Add a model to get started.')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row) => (
                    <TableRow key={row._id}>
                      <TableCell>
                        <Input
                          value={row.name}
                          placeholder='gpt-4o'
                          aria-label={t('Model name')}
                          aria-invalid={duplicateNames.includes(
                            row.name.trim()
                          )}
                          onChange={(event) =>
                            updateRow(row._id, 'name', event.target.value)
                          }
                        />
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
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          <DiscountRowMessages
            duplicateMessage={
              duplicateNames.length > 0
                ? t('Duplicate model names: {{names}}', {
                    names: duplicateNames.join(', '),
                  })
                : undefined
            }
            outOfRange={outOfRange}
          />
        </CardContent>
      </Card>
    )
  }
)

export function DiscountRowMessages(props: {
  duplicateMessage?: string
  outOfRange: boolean
}) {
  const { t } = useTranslation()
  return (
    <>
      {props.duplicateMessage && (
        <p className='text-destructive text-sm'>{props.duplicateMessage}</p>
      )}
      {props.outOfRange && (
        <p className='text-destructive text-sm'>
          {t('Discount percentages must be between 0 and 100.')}
        </p>
      )}
    </>
  )
}
