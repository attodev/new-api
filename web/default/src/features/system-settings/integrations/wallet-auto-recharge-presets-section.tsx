import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useForm } from 'react-hook-form'
import { SettingsSection } from '../components/settings-section'
import {
  createAdminWalletAutoRechargePreset,
  deleteAdminWalletAutoRechargePreset,
  isApiSuccess,
  listAdminWalletAutoRechargePresets,
  updateAdminWalletAutoRechargePreset,
} from '@/features/wallet/api'
import type {
  WalletAutoRechargeIntervalUnit,
  WalletAutoRechargePreset,
  WalletAutoRechargePresetRequest,
  WalletAutoRechargeTargetScope,
  WalletAutoRechargeType,
} from '@/features/wallet/types'

const PRESET_QUERY_KEY = ['admin-wallet-auto-recharge-presets'] as const
type SummaryTranslator = (
  key: string,
  options?: Record<string, unknown>
) => string

const TYPE_OPTIONS: Array<{ value: WalletAutoRechargeType; label: string }> = [
  { value: 'scheduled', label: 'Scheduled' },
  { value: 'threshold', label: 'Threshold' },
]

const TARGET_SCOPE_OPTIONS: Array<{
  value: WalletAutoRechargeTargetScope
  label: string
}> = [
  { value: 'all', label: 'All targets' },
  { value: 'user', label: 'Users only' },
  { value: 'organization', label: 'Organizations only' },
]

const INTERVAL_UNIT_OPTIONS: Array<{
  value: WalletAutoRechargeIntervalUnit
  label: string
}> = [
  { value: 'month', label: 'Month' },
  { value: 'day', label: 'Day' },
  { value: 'custom', label: 'Custom seconds' },
]

export interface WalletAutoRechargePresetFormState {
  type: WalletAutoRechargeType
  target_scope: WalletAutoRechargeTargetScope
  name: string
  description: string
  amount: string
  threshold_amount: string
  interval_unit: WalletAutoRechargeIntervalUnit
  interval_value: string
  custom_seconds: string
  charge_immediately: boolean
  sort_order: string
  enabled: boolean
}

function createEmptyPresetFormState(): WalletAutoRechargePresetFormState {
  return {
    type: 'scheduled',
    target_scope: 'all',
    name: '',
    description: '',
    amount: '',
    threshold_amount: '',
    interval_unit: 'month',
    interval_value: '1',
    custom_seconds: '',
    charge_immediately: true,
    sort_order: '0',
    enabled: true,
  }
}

function toPresetFormState(
  preset?: WalletAutoRechargePreset | null
): WalletAutoRechargePresetFormState {
  if (!preset) return createEmptyPresetFormState()

  return {
    type: preset.type,
    target_scope: preset.target_scope,
    name: preset.name,
    description: preset.description ?? '',
    amount: String(preset.amount ?? ''),
    threshold_amount:
      preset.threshold_amount !== null && preset.threshold_amount !== undefined
        ? String(preset.threshold_amount)
        : '',
    interval_unit: preset.interval_unit ?? 'month',
    interval_value:
      preset.interval_value !== null && preset.interval_value !== undefined
        ? String(preset.interval_value)
        : '1',
    custom_seconds:
      preset.custom_seconds !== null && preset.custom_seconds !== undefined
        ? String(preset.custom_seconds)
        : '',
    charge_immediately: preset.charge_immediately !== false,
    sort_order:
      preset.sort_order !== null && preset.sort_order !== undefined
        ? String(preset.sort_order)
        : '0',
    enabled: preset.enabled,
  }
}

export function normalizePresetForm(
  form: WalletAutoRechargePresetFormState
): WalletAutoRechargePresetRequest {
  const amount = Number(form.amount || 0)
  const sortOrder = Number(form.sort_order || 0)

  if (form.type === 'scheduled') {
    const intervalUnit = form.interval_unit
    const intervalValue = Number(form.interval_value || 0)
    const customSeconds =
      intervalUnit === 'custom' ? Number(form.custom_seconds || 0) : 0

    return {
      type: form.type,
      target_scope: form.target_scope,
      name: form.name.trim(),
      description: form.description.trim(),
      amount,
      threshold_amount: 0,
      interval_unit: intervalUnit,
      interval_value: intervalValue,
      custom_seconds: customSeconds,
      charge_immediately: form.charge_immediately,
      sort_order: sortOrder,
      enabled: form.enabled,
    }
  }

  return {
    type: form.type,
    target_scope: form.target_scope,
    name: form.name.trim(),
    description: form.description.trim(),
    amount,
    threshold_amount: Number(form.threshold_amount || 0),
    interval_unit: 'month',
    interval_value: 1,
    custom_seconds: 0,
    charge_immediately: false,
    sort_order: sortOrder,
    enabled: form.enabled,
  }
}

function getIntervalSummary(
  preset: WalletAutoRechargePreset,
  t: SummaryTranslator = (key) => key
) {
  if (preset.interval_unit === 'custom') {
    return t('{{seconds}}s', { seconds: preset.custom_seconds ?? 0 })
  }
  return t('{{value}} {{unit}}', {
    value: preset.interval_value || 1,
    unit: preset.interval_unit || 'month',
  })
}

export function getPresetSummary(
  preset: WalletAutoRechargePreset,
  t: SummaryTranslator = (key) => key
) {
  if (preset.type === 'scheduled') {
    return t('{{amount}} / {{interval}}', {
      amount: preset.amount,
      interval: getIntervalSummary(preset, t),
    })
  }
  return t('Below {{threshold}} -> {{amount}}', {
    threshold: preset.threshold_amount ?? 0,
    amount: preset.amount,
  })
}

function getTargetScopeLabel(scope: WalletAutoRechargeTargetScope) {
  return (
    TARGET_SCOPE_OPTIONS.find((option) => option.value === scope)?.label ?? scope
  )
}

function getTypeLabel(type: WalletAutoRechargeType) {
  return TYPE_OPTIONS.find((option) => option.value === type)?.label ?? type
}

function PresetTable({
  title,
  presets,
  onEdit,
  onDelete,
  isDeleting,
}: {
  title: string
  presets: WalletAutoRechargePreset[]
  onEdit: (preset: WalletAutoRechargePreset) => void
  onDelete: (preset: WalletAutoRechargePreset) => void
  isDeleting: boolean
}) {
  const { t } = useTranslation()

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {presets.length === 0 ? (
          <Empty className='rounded-lg border'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Plus />
              </EmptyMedia>
              <EmptyTitle>{t('No presets yet')}</EmptyTitle>
              <EmptyDescription>
                {t('Create a preset for this mode to expose it in wallet UI.')}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent />
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Name')}</TableHead>
                <TableHead>{t('Target scope')}</TableHead>
                <TableHead>{t('Summary')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {presets.map((preset) => (
                <TableRow key={preset.id}>
                  <TableCell>
                    <div className='flex flex-col gap-1'>
                      <div className='font-medium'>{preset.name}</div>
                      {preset.description ? (
                        <div className='text-muted-foreground text-sm'>
                          {preset.description}
                        </div>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant='outline'>
                      {t(getTargetScopeLabel(preset.target_scope))}
                    </Badge>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-sm'>
                    {getPresetSummary(preset, t)}
                  </TableCell>
                  <TableCell>
                    <Badge variant={preset.enabled ? 'secondary' : 'outline'}>
                      {preset.enabled ? t('Enabled') : t('Disabled')}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div className='flex justify-end gap-2'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => onEdit(preset)}
                      >
                        <Pencil data-icon='inline-start' />
                        {t('Edit')}
                      </Button>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        disabled={isDeleting}
                        onClick={() => onDelete(preset)}
                      >
                        <Trash2 data-icon='inline-start' />
                        {t('Delete')}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

export function WalletAutoRechargePresetsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingPreset, setEditingPreset] =
    useState<WalletAutoRechargePreset | null>(null)
  const [deletingPreset, setDeletingPreset] =
    useState<WalletAutoRechargePreset | null>(null)
  const form = useForm<WalletAutoRechargePresetFormState>({
    defaultValues: createEmptyPresetFormState(),
  })

  const presetQuery = useQuery({
    queryKey: PRESET_QUERY_KEY,
    queryFn: listAdminWalletAutoRechargePresets,
  })

  const presets = useMemo(
    () => [...(presetQuery.data?.data ?? [])].sort((left, right) => {
      if (left.type !== right.type) {
        return left.type.localeCompare(right.type)
      }
      const sortOrder = (left.sort_order ?? 0) - (right.sort_order ?? 0)
      if (sortOrder !== 0) return sortOrder
      return left.id - right.id
    }),
    [presetQuery.data?.data]
  )

  const scheduledPresets = useMemo(
    () => presets.filter((preset) => preset.type === 'scheduled'),
    [presets]
  )
  const thresholdPresets = useMemo(
    () => presets.filter((preset) => preset.type === 'threshold'),
    [presets]
  )

  const createMutation = useMutation({
    mutationFn: createAdminWalletAutoRechargePreset,
    onSuccess: async (response) => {
      if (!isApiSuccess(response)) {
        throw new Error(response.message || 'Failed to create preset')
      }
      await queryClient.invalidateQueries({ queryKey: PRESET_QUERY_KEY })
      toast.success(t('Preset created'))
      setDialogOpen(false)
      setEditingPreset(null)
      form.reset(createEmptyPresetFormState())
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Failed to create preset')
      )
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      payload,
    }: {
      id: number
      payload: WalletAutoRechargePresetRequest
    }) => updateAdminWalletAutoRechargePreset(id, payload),
    onSuccess: async (response) => {
      if (!isApiSuccess(response)) {
        throw new Error(response.message || 'Failed to update preset')
      }
      await queryClient.invalidateQueries({ queryKey: PRESET_QUERY_KEY })
      toast.success(t('Preset updated'))
      setDialogOpen(false)
      setEditingPreset(null)
      form.reset(createEmptyPresetFormState())
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Failed to update preset')
      )
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteAdminWalletAutoRechargePreset,
    onSuccess: async (response) => {
      if (!isApiSuccess(response)) {
        throw new Error(response.message || 'Failed to delete preset')
      }
      await queryClient.invalidateQueries({ queryKey: PRESET_QUERY_KEY })
      toast.success(t('Preset deleted'))
      setDeletingPreset(null)
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : t('Failed to delete preset')
      )
    },
  })

  const watchedType = form.watch('type')
  const watchedIntervalUnit = form.watch('interval_unit')
  const isSaving = createMutation.isPending || updateMutation.isPending

  const openCreateDialog = () => {
    setEditingPreset(null)
    form.reset(createEmptyPresetFormState())
    setDialogOpen(true)
  }

  const openEditDialog = (preset: WalletAutoRechargePreset) => {
    setEditingPreset(preset)
    form.reset(toPresetFormState(preset))
    setDialogOpen(true)
  }

  const handleDialogChange = (open: boolean) => {
    setDialogOpen(open)
    if (!open) {
      setEditingPreset(null)
      form.reset(createEmptyPresetFormState())
    }
  }

  const handleSubmit = async (values: WalletAutoRechargePresetFormState) => {
    const payload = normalizePresetForm(values)
    if (!payload.name) {
      form.setError('name', { message: t('Preset name is required') })
      return
    }

    if (editingPreset) {
      await updateMutation.mutateAsync({ id: editingPreset.id, payload })
      return
    }
    await createMutation.mutateAsync(payload)
  }

  return (
    <SettingsSection title={t('Auto Recharge Presets')}>
      <div className='flex items-center justify-between gap-3'>
        <div className='flex flex-col gap-1'>
          <div className='text-sm font-medium'>{t('Admin preset library')}</div>
          <div className='text-muted-foreground text-sm'>
            {t(
              'Manage the scheduled and threshold presets shown in wallet auto recharge flows.'
            )}
          </div>
        </div>
        <Button type='button' onClick={openCreateDialog}>
          <Plus data-icon='inline-start' />
          {t('Add preset')}
        </Button>
      </div>

      <Separator />

      {presetQuery.isLoading ? (
        <div className='flex flex-col gap-4'>
          <Skeleton className='h-40 w-full rounded-lg' />
          <Skeleton className='h-40 w-full rounded-lg' />
        </div>
      ) : (
        <div className='flex flex-col gap-4'>
          <PresetTable
            title={t('Scheduled presets')}
            presets={scheduledPresets}
            onEdit={openEditDialog}
            onDelete={setDeletingPreset}
            isDeleting={deleteMutation.isPending}
          />
          <PresetTable
            title={t('Threshold presets')}
            presets={thresholdPresets}
            onEdit={openEditDialog}
            onDelete={setDeletingPreset}
            isDeleting={deleteMutation.isPending}
          />
        </div>
      )}

      <Dialog open={dialogOpen} onOpenChange={handleDialogChange}>
        <DialogContent className='sm:max-w-[640px]'>
          <DialogHeader>
            <DialogTitle>
              {editingPreset ? t('Edit preset') : t('Create preset')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'These presets are available to wallet auto recharge flows for users and organizations.'
              )}
            </DialogDescription>
          </DialogHeader>

          <Form {...form}>
            <form
              onSubmit={form.handleSubmit(handleSubmit)}
              className='flex flex-col gap-4'
            >
              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='type'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Preset type')}</FormLabel>
                      <Select
                        items={TYPE_OPTIONS}
                        value={field.value}
                        onValueChange={(value) =>
                          field.onChange(value as WalletAutoRechargeType)
                        }
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder={t('Select preset type')} />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {TYPE_OPTIONS.map((option) => (
                              <SelectItem key={option.value} value={option.value}>
                                {t(option.label)}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='target_scope'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Target scope')}</FormLabel>
                      <Select
                        items={TARGET_SCOPE_OPTIONS}
                        value={field.value}
                        onValueChange={(value) =>
                          field.onChange(value as WalletAutoRechargeTargetScope)
                        }
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder={t('Select target scope')} />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {TARGET_SCOPE_OPTIONS.map((option) => (
                              <SelectItem key={option.value} value={option.value}>
                                {t(option.label)}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Preset name')}</FormLabel>
                      <FormControl>
                        <Input placeholder={t('e.g., Monthly recharge')} {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='sort_order'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Sort order')}</FormLabel>
                      <FormControl>
                        <Input type='number' min={0} {...field} />
                      </FormControl>
                      <FormDescription>
                        {t('Lower values appear first in wallet UI.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <FormField
                control={form.control}
                name='description'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Description')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('Optional help text shown with the preset')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='amount'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Recharge amount')}</FormLabel>
                      <FormControl>
                        <Input type='number' min={0} {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {watchedType === 'threshold' ? (
                  <FormField
                    control={form.control}
                    name='threshold_amount'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Threshold amount')}</FormLabel>
                        <FormControl>
                          <Input type='number' min={0} {...field} />
                        </FormControl>
                        <FormDescription>
                          {t('Recharge when balance falls to this amount or below.')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                ) : (
                  <div className='grid gap-4 sm:grid-cols-2 sm:col-span-1'>
                    <FormField
                      control={form.control}
                      name='interval_unit'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Interval unit')}</FormLabel>
                          <Select
                            items={INTERVAL_UNIT_OPTIONS}
                            value={field.value}
                            onValueChange={(value) =>
                              field.onChange(value as WalletAutoRechargeIntervalUnit)
                            }
                          >
                            <FormControl>
                              <SelectTrigger>
                                <SelectValue
                                  placeholder={t('Select interval unit')}
                                />
                              </SelectTrigger>
                            </FormControl>
                            <SelectContent alignItemWithTrigger={false}>
                              <SelectGroup>
                                {INTERVAL_UNIT_OPTIONS.map((option) => (
                                  <SelectItem
                                    key={option.value}
                                    value={option.value}
                                  >
                                    {t(option.label)}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='interval_value'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Interval value')}</FormLabel>
                          <FormControl>
                            <Input type='number' min={1} {...field} />
                          </FormControl>
                          <FormDescription>
                            {watchedIntervalUnit === 'custom'
                              ? t('Keep this as 1 unless you need an advanced multiplier.')
                              : t('Number of units between charges.')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                )}
              </div>

              {watchedType === 'scheduled' && watchedIntervalUnit === 'custom' ? (
                <FormField
                  control={form.control}
                  name='custom_seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Custom seconds')}</FormLabel>
                      <FormControl>
                        <Input type='number' min={1} {...field} />
                      </FormControl>
                      <FormDescription>
                        {t('Set the custom interval in seconds.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ) : null}

              {watchedType === 'scheduled' ? (
                <FormField
                  control={form.control}
                  name='charge_immediately'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4 rounded-lg border p-4'>
                      <div className='flex flex-col gap-1'>
                        <FormLabel>{t('Charge immediately')}</FormLabel>
                        <FormDescription>
                          {t('Start the first scheduled charge as soon as the preset is selected.')}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              ) : null}

              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='enabled'
                  render={({ field }) => (
                    <FormItem className='flex items-center justify-between gap-4 rounded-lg border p-4'>
                      <div className='flex flex-col gap-1'>
                        <FormLabel>{t('Enabled')}</FormLabel>
                        <FormDescription>
                          {t('Disable a preset without deleting its configuration.')}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                <div className='rounded-lg border p-4'>
                  <div className='text-sm font-medium'>{t('Preview')}</div>
                  <div className='text-muted-foreground mt-2 text-sm'>
                    {t(getTypeLabel(watchedType))}:{' '}
                    {getPresetSummary(
                      {
                        id: editingPreset?.id ?? 0,
                        ...normalizePresetForm(form.getValues()),
                      },
                      t
                    )}
                  </div>
                </div>
              </div>

              <DialogFooter>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => handleDialogChange(false)}
                >
                  {t('Cancel')}
                </Button>
                <Button type='submit' disabled={isSaving}>
                  {isSaving ? <Spinner data-icon='inline-start' /> : null}
                  {editingPreset ? t('Update preset') : t('Create preset')}
                </Button>
              </DialogFooter>
            </form>
          </Form>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={!!deletingPreset}
        onOpenChange={(open) => {
          if (!open) {
            setDeletingPreset(null)
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete preset')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('This removes the preset from wallet auto recharge flows. Existing subscriptions are not changed.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              disabled={deleteMutation.isPending || !deletingPreset}
              onClick={() => {
                if (!deletingPreset) return
                void deleteMutation.mutateAsync(deletingPreset.id)
              }}
            >
              {deleteMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
