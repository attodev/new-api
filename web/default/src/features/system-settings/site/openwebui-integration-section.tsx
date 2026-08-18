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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { SettingsForm, SettingsSwitchContent, SettingsSwitchItem } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * This settings panel is the reverse direction of the "OAuth Integrations"
 * tab under Authentication: it doesn't let new-api users sign in via an
 * external provider — it lets an external chat app (e.g. OpenWebUI) sign
 * users in as themselves, using new-api as the identity provider. Kept
 * under Site & Branding as its own section for that reason, not folded
 * into Auth's OAuth Integrations tab.
 */
const oauth2Schema = z.object({
  oauth2: z.object({
    enabled: z.boolean(),
    client_id: z.string(),
    client_secret: z.string(),
    redirect_uri: z.string(),
    open_in_new_window: z.boolean(),
    replace_playground: z.boolean(),
  }),
})

type Oauth2FormValues = z.infer<typeof oauth2Schema>

type FlatOauth2Defaults = {
  'oauth2.enabled': boolean
  'oauth2.client_id': string
  'oauth2.client_secret': string
  'oauth2.redirect_uri': string
  'oauth2.open_in_new_window': boolean
  'oauth2.replace_playground': boolean
}

const buildFormDefaults = (defaults: FlatOauth2Defaults): Oauth2FormValues => ({
  oauth2: {
    enabled: defaults['oauth2.enabled'],
    client_id: defaults['oauth2.client_id'] ?? '',
    client_secret: defaults['oauth2.client_secret'] ?? '',
    redirect_uri: defaults['oauth2.redirect_uri'] ?? '',
    open_in_new_window: defaults['oauth2.open_in_new_window'],
    replace_playground: defaults['oauth2.replace_playground'],
  },
})

const normalizeFormValues = (values: Oauth2FormValues): FlatOauth2Defaults => ({
  'oauth2.enabled': values.oauth2.enabled,
  'oauth2.client_id': values.oauth2.client_id,
  'oauth2.client_secret': values.oauth2.client_secret,
  'oauth2.redirect_uri': values.oauth2.redirect_uri,
  'oauth2.open_in_new_window': values.oauth2.open_in_new_window,
  'oauth2.replace_playground': values.oauth2.replace_playground,
})

interface OpenWebUIOAuth2SectionProps {
  defaultValues: FlatOauth2Defaults
}

export function OpenWebUIOAuth2Section(props: OpenWebUIOAuth2SectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<Oauth2FormValues>({
    resolver: zodResolver(oauth2Schema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatOauth2Defaults>(props.defaultValues)
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

  const onSubmit = async (values: Oauth2FormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatOauth2Defaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
  }

  return (
    <SettingsSection title={t('OpenWebUI Integration')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='oauth2.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable External App Access')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Let an external chat app (e.g. OpenWebUI) sign in as new-api users. new-api acts as the identity provider for the external app — this is the reverse of signing new-api in via an external OAuth provider.'
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
            name='oauth2.client_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Client ID')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t(
                      'A value you choose and share with the external app'
                    )}
                    autoComplete='off'
                    value={field.value ?? ''}
                    onChange={(event) => field.onChange(event.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='oauth2.client_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Client Secret')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    placeholder={t(
                      'A random secret value you generate and share with the external app'
                    )}
                    autoComplete='new-password'
                    value={field.value ?? ''}
                    onChange={(event) => field.onChange(event.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='oauth2.redirect_uri'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Redirect URI')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t(
                      'https://your-chat-app.example.com/auth/newapi/callback'
                    )}
                    autoComplete='off'
                    value={field.value ?? ''}
                    onChange={(event) => field.onChange(event.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The exact callback URL the external app registered for this integration'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='oauth2.open_in_new_window'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Open in a New Window')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Open the external chat app in a new tab, keeping this window open, instead of navigating away from it'
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
            name='oauth2.replace_playground'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Replace Playground')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Make the Playground page itself act like the "Open in Chat App" button — visiting Playground goes straight to the external app instead of showing the built-in playground'
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
