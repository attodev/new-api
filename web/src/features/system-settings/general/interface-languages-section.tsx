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
// fork: O8 — the InterfaceLanguages option: which languages the language menu
// offers. Every locale stays loaded; a user's current language stays available.
import { zodResolver } from '@hookform/resolvers/zod'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { INTERFACE_LANGUAGE_OPTIONS } from '@/i18n/languages'
import { parseInterfaceLanguageOption } from '@/lib/interface-languages'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

const interfaceLanguagesSchema = z.object({
  InterfaceLanguages: z
    .array(z.string())
    .min(1, { message: 'Select at least one language.' }),
})

type InterfaceLanguagesFormValues = z.infer<typeof interfaceLanguagesSchema>

type InterfaceLanguagesSectionProps = {
  defaultValues: { InterfaceLanguages: string }
}

export function InterfaceLanguagesSection({
  defaultValues,
}: InterfaceLanguagesSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo<InterfaceLanguagesFormValues>(
    () => ({
      InterfaceLanguages: parseInterfaceLanguageOption(
        defaultValues.InterfaceLanguages
      ),
    }),
    [defaultValues.InterfaceLanguages]
  )

  const form = useForm<InterfaceLanguagesFormValues>({
    resolver: zodResolver(interfaceLanguagesSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (data: InterfaceLanguagesFormValues) => {
    const value = data.InterfaceLanguages.join(',')
    if (value === defaultValues.InterfaceLanguages) return
    await updateOption.mutateAsync({ key: 'InterfaceLanguages', value })
  }

  return (
    <SettingsSection title={t('Interface languages')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='InterfaceLanguages'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Interface languages')}</FormLabel>
                <FormDescription>
                  {t(
                    'Languages offered in the language menu. The current language of a user stays available.'
                  )}
                </FormDescription>
                <FormControl>
                  <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
                    {INTERFACE_LANGUAGE_OPTIONS.map((option) => (
                      <label
                        key={option.code}
                        className='flex items-center gap-2 text-sm'
                      >
                        <Checkbox
                          checked={field.value.includes(option.code)}
                          onCheckedChange={(checked) =>
                            field.onChange(
                              checked
                                ? [...field.value, option.code]
                                : field.value.filter(
                                    (code) => code !== option.code
                                  )
                            )
                          }
                        />
                        {option.label}
                      </label>
                    ))}
                  </div>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
