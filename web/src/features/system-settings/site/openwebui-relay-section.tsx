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
// fork: O13 — Open WebUI relay settings (Site & Branding section).
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useEffect, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

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

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { UpdateOptionRequest } from '../types'
import {
  getOpenWebUIRelayStatus,
  OPENWEBUI_RELAY_STATUS_QUERY_KEY,
  orderAroundEnableKey,
  saveOptionsInOrder,
  type OpenWebUIRelayDefaults,
} from './openwebui-fork'
import { ClearSecretButton, ConfiguredBadge } from './openwebui-secret-controls'

/**
 * Relay half of the Open WebUI integration. Open WebUI sends every user's
 * model call with one shared service key plus a per-request HS256 JWT
 * (X-OpenWebUI-User-Jwt); new-api verifies it and bills the identified
 * user's own `openwebui` token. The login half (OIDC) is the sibling
 * "OpenWebUI Integration" section.
 *
 * service_api_key / jwt_secret / jwt_previous_secret are write-only: GET
 * /api/option/ hides them, the form never prefills them, and whether each is
 * set comes from /api/openwebui/relay-status. An empty field means "keep".
 */
const SECRET_FIELDS = [
  'service_api_key',
  'jwt_secret',
  'jwt_previous_secret',
] as const
type SecretField = (typeof SECRET_FIELDS)[number]

const createRelaySchema = (t: TFunction) => {
  const secretRule = z
    .string()
    .refine((v) => v.trim() === '' || v.trim().length >= 32, {
      message: t('The Open WebUI JWT secret must be at least 32 characters'),
    })
  const leewayError = { message: t('Enter a whole number between 0 and 600') }
  return z.object({
    openwebui: z.object({
      relay_enabled: z.boolean(),
      service_api_key: z.string(),
      jwt_secret: secretRule,
      jwt_previous_secret: secretRule,
      jwt_issuer: z
        .string()
        .refine((v) => v.trim() !== '', { message: t('Invalid parameters') }),
      jwt_leeway_seconds: z
        .number(leewayError)
        .int(leewayError)
        .min(0, leewayError)
        .max(600, leewayError),
      allow_unsigned_requests: z.boolean(),
    }),
  })
}

type RelayFormValues = z.infer<ReturnType<typeof createRelaySchema>>
type RelayFields = RelayFormValues['openwebui']
type SecretValues = Pick<RelayFields, SecretField>

const EMPTY_SECRETS: SecretValues = {
  service_api_key: '',
  jwt_secret: '',
  jwt_previous_secret: '',
}

const toFormValues = (
  d: OpenWebUIRelayDefaults,
  secrets: SecretValues = EMPTY_SECRETS
): RelayFormValues => ({
  openwebui: {
    relay_enabled: d['openwebui.relay_enabled'],
    jwt_issuer: d['openwebui.jwt_issuer'] || 'open-webui',
    jwt_leeway_seconds: d['openwebui.jwt_leeway_seconds'] ?? 60,
    allow_unsigned_requests: d['openwebui.allow_unsigned_requests'],
    ...secrets,
  },
})

const toDefaults = (v: RelayFields): OpenWebUIRelayDefaults => ({
  'openwebui.relay_enabled': v.relay_enabled,
  'openwebui.jwt_issuer': v.jwt_issuer.trim(),
  'openwebui.jwt_leeway_seconds': v.jwt_leeway_seconds,
  'openwebui.allow_unsigned_requests': v.allow_unsigned_requests,
})

type OpenWebUIRelaySectionProps = {
  defaultValues: OpenWebUIRelayDefaults
}

export function OpenWebUIRelaySection(props: OpenWebUIRelaySectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()

  const statusQuery = useQuery({
    queryKey: OPENWEBUI_RELAY_STATUS_QUERY_KEY,
    queryFn: getOpenWebUIRelayStatus,
    staleTime: 60 * 1000,
  })
  const status = statusQuery.data?.data

  const form = useForm<RelayFormValues>({
    resolver: zodResolver(createRelaySchema(t)),
    defaultValues: toFormValues(props.defaultValues),
  })

  // The registry builds a new defaults object on every render; reset only when
  // the stored values really change, and keep secrets the admin is typing.
  const baselineRef = useRef(props.defaultValues)
  const serializedRef = useRef(JSON.stringify(props.defaultValues))
  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === serializedRef.current) return
    serializedRef.current = serialized
    baselineRef.current = props.defaultValues
    const current = form.getValues('openwebui')
    form.reset(
      toFormValues(props.defaultValues, {
        service_api_key: current.service_api_key,
        jwt_secret: current.jwt_secret,
        jwt_previous_secret: current.jwt_previous_secret,
      })
    )
  }, [props.defaultValues, form])

  const refreshStatus = () =>
    queryClient.invalidateQueries({
      queryKey: OPENWEBUI_RELAY_STATUS_QUERY_KEY,
    })

  const onSubmit = async (values: RelayFormValues) => {
    const v = values.openwebui
    const next = toDefaults(v)
    const secrets = SECRET_FIELDS.map((field) => ({
      key: `openwebui.${field}`,
      value: v[field].trim(),
    })).filter((update) => update.value !== '')

    const turningOn =
      next['openwebui.relay_enabled'] &&
      !baselineRef.current['openwebui.relay_enabled']
    if (
      turningOn &&
      status &&
      ((!status.service_key_configured && v.service_api_key.trim() === '') ||
        (!status.jwt_secret_configured && v.jwt_secret.trim() === ''))
    ) {
      form.setError('openwebui.relay_enabled', {
        message: t(
          'Set the service key and a JWT secret of at least 32 characters before enabling the Open WebUI relay'
        ),
      })
      return
    }

    const changed: UpdateOptionRequest[] = (
      Object.keys(next) as Array<keyof OpenWebUIRelayDefaults>
    )
      .filter((key) => next[key] !== baselineRef.current[key])
      .map((key) => ({ key, value: next[key] }))
    const updates = orderAroundEnableKey(
      [...secrets, ...changed],
      'openwebui.relay_enabled'
    )
    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    const ok = await saveOptionsInOrder(updateOption.mutateAsync, updates)
    if (secrets.length > 0 || ok) void refreshStatus()
    if (!ok) return

    baselineRef.current = next
    serializedRef.current = JSON.stringify(next)
    form.reset(toFormValues(next))
  }

  const clearSecret = async (field: SecretField) => {
    const ok = await saveOptionsInOrder(updateOption.mutateAsync, [
      { key: `openwebui.${field}`, value: '' },
    ])
    if (!ok) return
    form.setValue(`openwebui.${field}`, '')
    await refreshStatus()
  }

  const secretConfigured: Record<SecretField, boolean | undefined> = {
    service_api_key: status?.service_key_configured,
    jwt_secret: status?.jwt_secret_configured,
    jwt_previous_secret: status?.jwt_previous_secret_configured,
  }
  // The server refuses to clear the key or current secret while the relay is on.
  const clearBlocked: Record<SecretField, boolean> = {
    service_api_key: status?.relay_enabled === true,
    jwt_secret: status?.relay_enabled === true,
    jwt_previous_secret: false,
  }
  const secretText: Record<
    SecretField,
    { label: string; description: string; autoComplete: string }
  > = {
    service_api_key: {
      label: t('Service key'),
      description: t(
        'The key of an enabled token that Open WebUI uses as OPENAI_API_KEYS. Paste it with or without the sk- prefix. IP limits on that token restrict where Open WebUI may call from.'
      ),
      autoComplete: 'off',
    },
    jwt_secret: {
      label: t('JWT secret'),
      description: t(
        "Must equal Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET (32+ random characters)."
      ),
      autoComplete: 'new-password',
    },
    jwt_previous_secret: {
      label: t('Previous JWT secret'),
      description: t(
        'Accepted alongside the current secret while rotating. Move the old secret here, set the new one above, restart Open WebUI, then press Clear once every Open WebUI instance uses the new secret.'
      ),
      autoComplete: 'new-password',
    },
  }

  return (
    <SettingsSection title={t('OpenWebUI Relay')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
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
                  <FormMessage />
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

          {SECRET_FIELDS.map((secret) => (
            <FormField
              key={secret}
              control={form.control}
              name={`openwebui.${secret}`}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {secretText[secret].label}
                    <ConfiguredBadge configured={secretConfigured[secret]} />
                  </FormLabel>
                  <div className='flex gap-2'>
                    <FormControl>
                      <Input
                        type='password'
                        autoComplete={secretText[secret].autoComplete}
                        placeholder={t('Leave empty to keep the current value')}
                        {...field}
                        onChange={(e) => field.onChange(e.target.value)}
                      />
                    </FormControl>
                    <ClearSecretButton
                      disabled={
                        secretConfigured[secret] !== true ||
                        clearBlocked[secret]
                      }
                      isPending={updateOption.isPending}
                      onConfirm={() => clearSecret(secret)}
                    />
                  </div>
                  <FormDescription>
                    {secretText[secret].description}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}

          <FormField
            control={form.control}
            name='openwebui.jwt_issuer'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('JWT issuer')}</FormLabel>
                <FormControl>
                  <Input
                    autoComplete='off'
                    {...field}
                    onChange={(e) => field.onChange(e.target.value)}
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
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={Number.isNaN(field.value) ? '' : field.value}
                    onChange={(e) =>
                      field.onChange(
                        e.target.value === ''
                          ? Number.NaN
                          : Number(e.target.value)
                      )
                    }
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
