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
import { Textarea } from '@/components/ui/textarea'
import { safeJsonParse } from '../utils/json-parser'

type ModelDiscountVisualEditorProps = {
  modelDiscount: string
  onChange: (field: string, value: string) => void
}

type ModelDiscountRow = {
  _id: string
  name: string
  percent: number
}

const sectionCardClassName =
  'relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border before:border-border/90'
const sectionHeaderClassName = 'border-b bg-muted/20'

let modelDiscountIdCounter = 0
function createModelDiscountId() {
  modelDiscountIdCounter += 1
  return `mdr_${modelDiscountIdCounter}`
}

function clampPercent(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return 0
  if (parsed < 0) return 0
  if (parsed > 100) return 100
  return parsed
}

function buildModelDiscountRows(modelDiscount: string): ModelDiscountRow[] {
  const map = safeJsonParse<Record<string, number>>(modelDiscount, {
    fallback: {},
    context: 'model discounts',
  })
  return Object.entries(map).map(([name, percent]) => ({
    _id: createModelDiscountId(),
    name,
    percent: clampPercent(percent),
  }))
}

function serializeModelDiscountRows(rows: ModelDiscountRow[]): string {
  const map: Record<string, number> = {}
  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    map[name] = clampPercent(row.percent)
  }
  return JSON.stringify(map, null, 2)
}

function modelDiscountSignature(rows: ModelDiscountRow[]): string {
  return JSON.stringify(
    safeJsonParse(serializeModelDiscountRows(rows), {
      fallback: {},
      silent: true,
    })
  )
}

function sourceModelDiscountSignature(modelDiscount: string): string {
  return JSON.stringify(
    safeJsonParse(modelDiscount, { fallback: {}, silent: true })
  )
}

export const ModelDiscountVisualEditor = memo(
  function ModelDiscountVisualEditor({
    modelDiscount,
    onChange,
  }: ModelDiscountVisualEditorProps) {
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
              <CardTitle>{t('Model Discount')}</CardTitle>
              <CardDescription>
                {t(
                  'Per-model discount percentage (0–100). When a model has its own discount it takes precedence over any vendor discount.'
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
            <ModelDiscountTable
              modelDiscount={modelDiscount}
              onChange={onChange}
            />
          ) : (
            <ModelDiscountJsonEditor
              modelDiscount={modelDiscount}
              onChange={onChange}
            />
          )}
        </CardContent>
      </Card>
    )
  }
)

type ModelDiscountTableProps = {
  modelDiscount: string
  onChange: (field: string, value: string) => void
}

function ModelDiscountTable({
  modelDiscount,
  onChange,
}: ModelDiscountTableProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<ModelDiscountRow[]>(() =>
    buildModelDiscountRows(modelDiscount)
  )

  useEffect(() => {
    const incomingSignature = sourceModelDiscountSignature(modelDiscount)
    setRows((currentRows) => {
      if (modelDiscountSignature(currentRows) === incomingSignature) {
        return currentRows
      }
      return buildModelDiscountRows(modelDiscount)
    })
  }, [modelDiscount])

  const emitRows = useCallback(
    (nextRows: ModelDiscountRow[]) => {
      setRows(nextRows)
      onChange('ModelDiscount', serializeModelDiscountRows(nextRows))
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
    let index = 1
    let name = `model_${index}`
    while (existingNames.has(name)) {
      index += 1
      name = `model_${index}`
    }
    emitRows([
      ...rows,
      {
        _id: createModelDiscountId(),
        name,
        percent: 0,
      },
    ])
  }, [emitRows, rows])

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
        {t('Add model')}
      </Button>
      <div className='overflow-hidden rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='min-w-56'>{t('Model name')}</TableHead>
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
                  {t('No model discounts yet. Add a model to get started.')}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => {
                const percentNumber = Number(row.percent)
                const invalidPercent =
                  !Number.isFinite(percentNumber) ||
                  percentNumber < 0 ||
                  percentNumber > 100
                return (
                  <TableRow key={row._id}>
                    <TableCell>
                      <Input
                        value={row.name}
                        placeholder={t('gpt-4o')}
                        onChange={(event) =>
                          updateRow(row._id, 'name', event.target.value)
                        }
                        aria-invalid={duplicateNames.includes(row.name.trim())}
                      />
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
          {t('Duplicate model names: {{names}}', {
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

type ModelDiscountJsonEditorProps = {
  modelDiscount: string
  onChange: (field: string, value: string) => void
}

function ModelDiscountJsonEditor({
  modelDiscount,
  onChange,
}: ModelDiscountJsonEditorProps) {
  const { t } = useTranslation()

  return (
    <div className='space-y-2'>
      <Textarea
        rows={8}
        value={modelDiscount}
        onChange={(event) => onChange('ModelDiscount', event.target.value)}
      />
      <p className='text-muted-foreground text-sm'>
        {t(
          'JSON map of model name → discount percentage (0–100), e.g. { "gpt-4o": 10 }.'
        )}
      </p>
    </div>
  )
}
