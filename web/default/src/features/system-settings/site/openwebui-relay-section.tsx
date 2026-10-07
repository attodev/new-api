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
import { useEffect, useMemo, useRef } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
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
import { Switch } from '@/components/ui/switch'
import { getOpenWebUIRelayStatus } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * Relay half of the Open WebUI integration. Open WebUI sends every user's
 * model call with one shared service key plus a per-request HS256 JWT
 * (X-OpenWebUI-User-Jwt); new-api verifies it and bills the identified
 * user's own `openwebui` token. The login half (OIDC) lives in the sibling
 * "OpenWebUI Integration" section.
 *
 * service_api_key / jwt_secret / jwt_previous_secret are write-only: the
 * backend hides them from GET /api/option/, so the form shows whether each
 * is configured (from /api/openwebui/relay-status) and only sends a value
 * when the admin types one.
 */
const relaySchema = z.object({
  openwebui: z.object({
    relay_enabled: z.boolean(),
    service_api_key: z.string(),
    jwt_secret: z.string().refine((v) => v === '' || v.length >= 32, {
      message: 'JWT secret must be at least 32 characters',
    }),
    jwt_previous_secret: z.string().refine((v) => v === '' || v.length >= 32, {
      message: 'JWT secret must be at least 32 characters',
    }),
    jwt_issuer: z.string().min(1),
    jwt_leeway_seconds: z.number().int().min(0).max(600),
    allow_unsigned_requests: z.boolean(),
  }),
})

type RelayFormValues = z.infer<typeof relaySchema>

type FlatRelayDefaults = {
  'openwebui.relay_enabled': boolean
  'openwebui.service_api_key': string
  'openwebui.jwt_secret': string
  'openwebui.jwt_previous_secret': string
  'openwebui.jwt_issuer': string
  'openwebui.jwt_leeway_seconds': number
  'openwebui.allow_unsigned_requests': boolean
}

const buildFormDefaults = (d: FlatRelayDefaults): RelayFormValues => ({
  openwebui: {
    relay_enabled: d['openwebui.relay_enabled'],
    service_api_key: d['openwebui.service_api_key'] ?? '',
    jwt_secret: d['openwebui.jwt_secret'] ?? '',
    jwt_previous_secret: d['openwebui.jwt_previous_secret'] ?? '',
    jwt_issuer: d['openwebui.jwt_issuer'] || 'open-webui',
    jwt_leeway_seconds: d['openwebui.jwt_leeway_seconds'] ?? 60,
    allow_unsigned_requests: d['openwebui.allow_unsigned_requests'],
  },
})

const normalizeFormValues = (v: RelayFormValues): FlatRelayDefaults => ({
  'openwebui.relay_enabled': v.openwebui.relay_enabled,
  'openwebui.service_api_key': v.openwebui.service_api_key,
  'openwebui.jwt_secret': v.openwebui.jwt_secret,
  'openwebui.jwt_previous_secret': v.openwebui.jwt_previous_secret,
  'openwebui.jwt_issuer': v.openwebui.jwt_issuer,
  'openwebui.jwt_leeway_seconds': v.openwebui.jwt_leeway_seconds,
  'openwebui.allow_unsigned_requests': v.openwebui.allow_unsigned_requests,
})

// Secrets are write-only: an empty field means "keep the current value",
// never "clear it". Save order matters — the backend refuses to enable the
// relay before the key and secret exist, so relay_enabled goes last.
const SECRET_KEYS: Array<keyof FlatRelayDefaults> = [
  'openwebui.service_api_key',
  'openwebui.jwt_secret',
  'openwebui.jwt_previous_secret',
]

interface OpenWebUIRelaySectionProps {
  defaultValues: FlatRelayDefaults
}

function ConfiguredBadge({ configured }: { configured: boolean | undefined }) {
  const { t } = useTranslation()
  if (configured === undefined) return null
  return (
    <Badge variant={configured ? 'default' : 'outline'} className='ml-2'>
      {configured ? t('Configured') : t('Not configured')}
    </Badge>
  )
}

export function OpenWebUIRelaySection(props: OpenWebUIRelaySectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()

  const statusQuery = useQuery({
    queryKey: ['openwebui-relay-status'],
    queryFn: getOpenWebUIRelayStatus,
    staleTime: 60 * 1000,
  })
  const status = statusQuery.data?.data

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<RelayFormValues>({
    resolver: zodResolver(relaySchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatRelayDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const onSubmit = async (values: RelayFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatRelayDefaults>
    )
      .filter((key) => normalized[key] !== baselineRef.current[key])
      // An untouched (empty) secret field is not a change.
      .filter((key) => !(SECRET_KEYS.includes(key) && normalized[key] === ''))
      .sort((a, b) => {
        if (a === 'openwebui.relay_enabled') return 1
        if (b === 'openwebui.relay_enabled') return -1
        return 0
      })

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      const result = await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
      if (!result.success) {
        // useUpdateOption already toasted the backend message; stop here so
        // relay_enabled is not attempted after a failed secret save.
        return
      }
    }

    // Secrets were accepted by the server; blank them again in the form so
    // the field goes back to "keep current value".
    const nextBaseline: FlatRelayDefaults = {
      ...normalized,
      'openwebui.service_api_key': '',
      'openwebui.jwt_secret': '',
      'openwebui.jwt_previous_secret': '',
    }
    baselineRef.current = nextBaseline
    baselineSerializedRef.current = JSON.stringify(nextBaseline)
    form.reset(buildFormDefaults(nextBaseline))
    queryClient.invalidateQueries({ queryKey: ['openwebui-relay-status'] })
  }

  return (
    <SettingsSection title={t('OpenWebUI Relay')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='openwebui.relay_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Open WebUI relay')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Bill requests that arrive with the service key to the Open WebUI user identified by the signed X-OpenWebUI-User-Jwt header. Requires the service key and a JWT secret below.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.service_api_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Service key')}
                  <ConfiguredBadge
                    configured={status?.service_key_configured}
                  />
                </FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='off'
                    placeholder={t('Leave empty to keep the current value')}
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The key of an enabled token that Open WebUI uses as OPENAI_API_KEYS. Paste it with or without the sk- prefix. IP limits on that token restrict where Open WebUI may call from.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('JWT secret')}
                  <ConfiguredBadge configured={status?.jwt_secret_configured} />
                </FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={t('Leave empty to keep the current value')}
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    "Must equal Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET (32+ random characters)."
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_previous_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Previous JWT secret')}
                  <ConfiguredBadge
                    configured={status?.jwt_previous_secret_configured}
                  />
                </FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={t('Leave empty to keep the current value')}
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Accepted alongside the current secret while rotating. Move the old secret here, set the new one above, restart Open WebUI, then clear this field via the relay-status API or by saving a new rotation.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_issuer'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('JWT issuer')}</FormLabel>
                <FormControl>
                  <Input
                    autoComplete='off'
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t('Expected iss claim. Open WebUI always sends open-webui.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_leeway_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Clock skew allowance (seconds)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={600}
                    value={Number.isNaN(field.value) ? '' : (field.value ?? 60)}
                    onChange={(e) =>
                      field.onChange(
                        e.target.value === ''
                          ? Number.NaN
                          : Number(e.target.value)
                      )
                    }
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Applied to exp and iat. Open WebUI JWTs live 300 seconds by default; 60 is a safe start.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.allow_unsigned_requests'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Allow requests without a user JWT')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Some Open WebUI audio routes send the service key without a JWT. On: bill them to the service token itself (logged as unsigned). Off: reject them with 401. A present but invalid JWT is always rejected.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
