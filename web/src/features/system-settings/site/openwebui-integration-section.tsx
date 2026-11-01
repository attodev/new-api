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
// fork: O13 — built-in OIDC provider settings for Open WebUI login (Site &
// Branding section), the API key token (POST /oauth2/token-from-key)
// settings, and the ID token signing key status and rotation.
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
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
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

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
  getOidcSigningKeyStatus,
  isAbsoluteHttpUrl,
  OIDC_SIGNING_KEY_STATUS_QUERY_KEY,
  orderAroundEnableKey,
  rotateOidcSigningKey,
  saveOptionsInOrder,
  withStatusRefresh,
  type OpenWebUIOAuth2Defaults,
} from './openwebui-fork'
import { ClearSecretButton } from './openwebui-secret-controls'

/**
 * The reverse of Authentication's OAuth integrations: an external chat app
 * (Open WebUI) signs users in as themselves with new-api as the OpenID
 * Connect provider. oauth2.client_secret is write-only: GET /api/option/
 * hides it, the form never prefills it, and an empty field means "keep".
 */
const createOAuth2Schema = (t: TFunction) => {
  const urlRule = z
    .string()
    .refine((v) => v.trim() === '' || isAbsoluteHttpUrl(v), {
      message: t('Must be an absolute http(s) URL'),
    })
  // Same ranges and sentences as the server's option validation.
  const ttlError = {
    message: t('The API key token lifetime must be between 60 and 900 seconds'),
  }
  const rateError = {
    message: t(
      'The API key token rate limit must be between 1 and 600 per minute'
    ),
  }
  return z.object({
    oauth2: z
      .object({
        enabled: z.boolean(),
        client_id: z.string(),
        client_secret: z
          .string()
          .refine((v) => v.trim() === '' || v.trim().length >= 16, {
            message: t('The client secret must be at least 16 characters'),
          }),
        redirect_uri: urlRule,
        app_url: urlRule,
        post_logout_redirect_uri: urlRule,
        open_in_new_window: z.boolean(),
        replace_playground: z.boolean(),
        token_from_key_enabled: z.boolean(),
        token_from_key_client_id: z.string(),
        token_from_key_ttl_seconds: z
          .number(ttlError)
          .int(ttlError)
          .min(60, ttlError)
          .max(900, ttlError),
        token_from_key_rate_limit: z
          .number(rateError)
          .int(rateError)
          .min(1, rateError)
          .max(600, rateError),
      })
      .refine(
        (v) => {
          const id = v.token_from_key_client_id.trim()
          return id !== '' && id !== v.client_id.trim()
        },
        {
          path: ['token_from_key_client_id'],
          message: t(
            'The API key token client ID must not be empty and must differ from the Open WebUI client ID'
          ),
        }
      ),
  })
}

type OAuth2FormValues = z.infer<ReturnType<typeof createOAuth2Schema>>
type OAuth2Fields = OAuth2FormValues['oauth2']

const toFormValues = (
  d: OpenWebUIOAuth2Defaults,
  clientSecret = ''
): OAuth2FormValues => ({
  oauth2: {
    enabled: d['oauth2.enabled'],
    client_id: d['oauth2.client_id'] ?? '',
    client_secret: clientSecret,
    redirect_uri: d['oauth2.redirect_uri'] ?? '',
    app_url: d['oauth2.app_url'] ?? '',
    post_logout_redirect_uri: d['oauth2.post_logout_redirect_uri'] ?? '',
    open_in_new_window: d['oauth2.open_in_new_window'],
    replace_playground: d['oauth2.replace_playground'],
    token_from_key_enabled: d['oauth2.token_from_key_enabled'] ?? false,
    token_from_key_client_id: d['oauth2.token_from_key_client_id'] ?? '',
    token_from_key_ttl_seconds: d['oauth2.token_from_key_ttl_seconds'] ?? 600,
    token_from_key_rate_limit: d['oauth2.token_from_key_rate_limit'] ?? 30,
  },
})

const toDefaults = (v: OAuth2Fields): OpenWebUIOAuth2Defaults => ({
  'oauth2.enabled': v.enabled,
  'oauth2.client_id': v.client_id.trim(),
  'oauth2.redirect_uri': v.redirect_uri.trim(),
  'oauth2.app_url': v.app_url.trim(),
  'oauth2.post_logout_redirect_uri': v.post_logout_redirect_uri.trim(),
  'oauth2.open_in_new_window': v.open_in_new_window,
  'oauth2.replace_playground': v.replace_playground,
  'oauth2.token_from_key_enabled': v.token_from_key_enabled,
  'oauth2.token_from_key_client_id': v.token_from_key_client_id.trim(),
  'oauth2.token_from_key_ttl_seconds': v.token_from_key_ttl_seconds,
  'oauth2.token_from_key_rate_limit': v.token_from_key_rate_limit,
})

type TextField = Extract<
  keyof OAuth2Fields,
  | 'client_id'
  | 'redirect_uri'
  | 'app_url'
  | 'post_logout_redirect_uri'
  | 'token_from_key_client_id'
>

type NumberField = Extract<
  keyof OAuth2Fields,
  'token_from_key_ttl_seconds' | 'token_from_key_rate_limit'
>

type OpenWebUIOAuth2SectionProps = {
  defaultValues: OpenWebUIOAuth2Defaults
}

export function OpenWebUIOAuth2Section(props: OpenWebUIOAuth2SectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  // Every key here is oauth2.*, which feeds the public status oauth2_* fields.
  const saveOption = withStatusRefresh(updateOption.mutateAsync, queryClient)

  const form = useForm<OAuth2FormValues>({
    resolver: zodResolver(createOAuth2Schema(t)),
    defaultValues: toFormValues(props.defaultValues),
  })

  const baselineRef = useRef(props.defaultValues)
  const serializedRef = useRef(JSON.stringify(props.defaultValues))
  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === serializedRef.current) return
    serializedRef.current = serialized
    baselineRef.current = props.defaultValues
    form.reset(
      toFormValues(props.defaultValues, form.getValues('oauth2.client_secret'))
    )
  }, [props.defaultValues, form])

  const onSubmit = async (values: OAuth2FormValues) => {
    const v = values.oauth2
    const next = toDefaults(v)
    const clientSecret = v.client_secret.trim()

    const turningOn =
      next['oauth2.enabled'] && !baselineRef.current['oauth2.enabled']
    if (
      turningOn &&
      (next['oauth2.client_id'] === '' ||
        !isAbsoluteHttpUrl(next['oauth2.redirect_uri']))
    ) {
      form.setError('oauth2.enabled', {
        message: t(
          'Set a client ID, a client secret of at least 16 characters and an absolute redirect URI before enabling external app access'
        ),
      })
      return
    }

    const updates: UpdateOptionRequest[] = (
      Object.keys(next) as Array<keyof OpenWebUIOAuth2Defaults>
    )
      .filter((key) => next[key] !== baselineRef.current[key])
      .map((key) => ({ key, value: next[key] }))
    if (clientSecret !== '') {
      updates.push({ key: 'oauth2.client_secret', value: clientSecret })
    }
    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    // Each switch is saved after the settings it guards when it turns on,
    // and before them when it turns off; the provider switch wraps it all.
    const ok = await saveOptionsInOrder(
      saveOption,
      orderAroundEnableKey(
        orderAroundEnableKey(updates, 'oauth2.token_from_key_enabled'),
        'oauth2.enabled'
      )
    )
    if (!ok) return

    baselineRef.current = next
    serializedRef.current = JSON.stringify(next)
    form.reset(toFormValues(next))
  }

  const clearClientSecret = async () => {
    const ok = await saveOptionsInOrder(saveOption, [
      { key: 'oauth2.client_secret', value: '' },
    ])
    if (ok) form.setValue('oauth2.client_secret', '')
  }

  const textFields: Record<
    TextField,
    { label: string; placeholder: string; description?: string }
  > = {
    client_id: {
      label: t('Client ID'),
      placeholder: t('A value you choose and share with the external app'),
    },
    redirect_uri: {
      label: t('Redirect URI'),
      placeholder: 'https://chat.example.com/oauth/oidc/login/callback',
      description: t(
        "Open WebUI's OPENID_REDIRECT_URI — must match exactly. Open WebUI's default is {WEBUI_URL}/oauth/oidc/login/callback."
      ),
    },
    app_url: {
      label: t('App URL'),
      placeholder: 'https://chat.example.com',
      description: t(
        'Open WebUI\'s WEBUI_URL. The Playground "Open in Chat App" link opens this address; the app then signs in through new-api automatically.'
      ),
    },
    post_logout_redirect_uri: {
      label: t('Post-logout redirect URI'),
      placeholder: 'https://chat.example.com',
      description: t(
        "Open WebUI's WEBUI_AUTH_SIGNOUT_REDIRECT_URL. The only address the end-session endpoint will redirect to after logging out; leave empty to send users to the new-api sign-in page."
      ),
    },
    token_from_key_client_id: {
      label: t('API Key Token Client ID'),
      placeholder: 'openwebui-mcp',
      description: t(
        "Reported by introspection for these tokens; add it to Open WebUI's OAUTH_TOKEN_EXCHANGE_TRUSTED_CLIENT_IDS. Must differ from the Client ID."
      ),
    },
  }

  const numberFields: Record<
    NumberField,
    { label: string; description: string; min: number; max: number }
  > = {
    token_from_key_ttl_seconds: {
      label: t('API Key Token Lifetime (seconds)'),
      description: t('Between 60 and 900 seconds.'),
      min: 60,
      max: 900,
    },
    token_from_key_rate_limit: {
      label: t('API Key Token Requests per Minute'),
      description: t(
        'Limit per API key. Each client IP is also limited to 60 per minute.'
      ),
      min: 1,
      max: 600,
    },
  }

  const renderNumberField = (name: NumberField) => (
    <FormField
      control={form.control}
      name={`oauth2.${name}`}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{numberFields[name].label}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={numberFields[name].min}
              max={numberFields[name].max}
              name={field.name}
              ref={field.ref}
              onBlur={field.onBlur}
              value={Number.isNaN(field.value) ? '' : field.value}
              onChange={(e) =>
                field.onChange(
                  e.target.value === '' ? Number.NaN : Number(e.target.value)
                )
              }
            />
          </FormControl>
          <FormDescription>{numberFields[name].description}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const renderTextField = (name: TextField) => (
    <FormField
      control={form.control}
      name={`oauth2.${name}`}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{textFields[name].label}</FormLabel>
          <FormControl>
            <Input
              placeholder={textFields[name].placeholder}
              autoComplete='off'
              {...field}
              onChange={(e) => field.onChange(e.target.value)}
            />
          </FormControl>
          {textFields[name].description && (
            <FormDescription>{textFields[name].description}</FormDescription>
          )}
          <FormMessage />
        </FormItem>
      )}
    />
  )

  const renderSwitch = (
    name:
      | 'enabled'
      | 'open_in_new_window'
      | 'replace_playground'
      | 'token_from_key_enabled',
    label: string,
    description: string
  ) => (
    <FormField
      control={form.control}
      name={`oauth2.${name}`}
      render={({ field }) => (
        <SettingsSwitchItem>
          <SettingsSwitchContent>
            <FormLabel>{label}</FormLabel>
            <FormDescription>{description}</FormDescription>
            <FormMessage />
          </SettingsSwitchContent>
          <FormControl>
            <Switch checked={field.value} onCheckedChange={field.onChange} />
          </FormControl>
        </SettingsSwitchItem>
      )}
    />
  )

  return (
    <SettingsSection title={t('OpenWebUI Integration')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          {renderSwitch(
            'enabled',
            t('Enable External App Access'),
            t(
              "Let Open WebUI sign in as new-api users through the built-in OpenID Connect provider. Point Open WebUI's OPENID_PROVIDER_URL at this server's /.well-known/openid-configuration."
            )
          )}

          {renderTextField('client_id')}

          <FormField
            control={form.control}
            name='oauth2.client_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Client Secret')}</FormLabel>
                <div className='flex gap-2'>
                  <FormControl>
                    <Input
                      type='password'
                      placeholder={t('Leave empty to keep the current value')}
                      autoComplete='new-password'
                      {...field}
                      onChange={(e) => field.onChange(e.target.value)}
                    />
                  </FormControl>
                  <ClearSecretButton
                    disabled={baselineRef.current['oauth2.enabled']}
                    isPending={updateOption.isPending}
                    onConfirm={clearClientSecret}
                  />
                </div>
                <FormDescription>
                  {t(
                    'A random secret of at least 16 characters that you generate and share with the external app. It is never shown again after saving.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          {renderTextField('redirect_uri')}
          {renderTextField('app_url')}
          {renderTextField('post_logout_redirect_uri')}

          {renderSwitch(
            'open_in_new_window',
            t('Open in a New Window'),
            t(
              'Open the external chat app in a new tab, keeping this window open, instead of navigating away from it'
            )
          )}
          {renderSwitch(
            'replace_playground',
            t('Replace Playground'),
            t(
              'Make the Playground page itself act like the "Open in Chat App" button — visiting Playground goes straight to the external app instead of showing the built-in playground'
            )
          )}

          {renderSwitch(
            'token_from_key_enabled',
            t('Issue Tokens from API Keys'),
            t(
              "Let a server holding a user's API key get a short-lived OIDC access token for that user from POST /oauth2/token-from-key (for the Open WebUI MCP server with OAuth token exchange). The token only works with userinfo and introspection."
            )
          )}
          {renderTextField('token_from_key_client_id')}
          {renderNumberField('token_from_key_ttl_seconds')}
          {renderNumberField('token_from_key_rate_limit')}
        </SettingsForm>
      </Form>
      <SigningKeyPanel />
    </SettingsSection>
  )
}

/** service/oidc PreviousKeyRetention: a retired key stays in the JWKS 24h. */
const PREVIOUS_KEY_RETENTION = 24 * 60 * 60

/** Root-only (the System Settings route and the API both require root). */
function SigningKeyPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const statusQuery = useQuery({
    queryKey: OIDC_SIGNING_KEY_STATUS_QUERY_KEY,
    queryFn: async () => requireServerSuccess(await getOidcSigningKeyStatus()),
    staleTime: 60 * 1000,
  })
  const rotate = useMutation({
    mutationFn: async () => requireServerSuccess(await rotateOidcSigningKey()),
    onSuccess: () => {
      toast.success(t('Signing key rotated'))
      void queryClient.invalidateQueries({
        queryKey: OIDC_SIGNING_KEY_STATUS_QUERY_KEY,
      })
    },
    onError: (error) =>
      handleServerError(error, t('Failed to rotate signing key')),
    onSettled: () => setConfirmOpen(false),
  })
  const data = statusQuery.data?.data
  const fmt = (ts: number) => new Date(ts * 1000).toLocaleString()

  return (
    <div className='flex flex-col gap-2 rounded-md border p-4'>
      <h4 className='text-sm font-medium'>{t('ID token signing key')}</h4>
      <p className='text-muted-foreground text-xs'>
        {t(
          'id_tokens are signed with RS256. Rotating installs a new key immediately; the previous key stays published for 24 hours so tokens signed with it keep verifying.'
        )}
      </p>
      {data ? (
        <div className='text-muted-foreground grid gap-1 font-mono text-xs'>
          <span>
            {t('Current')}: {data.current.kid} · {fmt(data.current.created_at)}
          </span>
          {data.previous && (
            <span>
              {t('Previous')}: {data.previous.kid} ·{' '}
              {fmt(data.previous.created_at)} ·{' '}
              {/* The rotation that retired it created the current key. */}
              {t('Published until {{time}}', {
                time: fmt(data.current.created_at + PREVIOUS_KEY_RETENTION),
              })}
            </span>
          )}
        </div>
      ) : (
        <p className='text-muted-foreground text-xs'>
          {statusQuery.isError ? t('Failed to load') : t('Loading...')}
        </p>
      )}
      <div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => setConfirmOpen(true)}
          disabled={rotate.isPending || !data}
        >
          {t('Rotate signing key')}
        </Button>
      </div>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Rotate the signing key?')}
        desc={t(
          'A new key will be used for all new id_tokens. Open WebUI picks it up from the JWKS endpoint automatically; existing sessions are not affected.'
        )}
        confirmText={t('Rotate')}
        isLoading={rotate.isPending}
        handleConfirm={() => rotate.mutate()}
      />
    </div>
  )
}
