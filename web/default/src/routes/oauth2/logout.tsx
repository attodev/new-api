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
import { useEffect } from 'react'
import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { AuthLayout } from '@/features/auth/auth-layout'
import { logoutOidc } from '@/features/oidc/api'

const searchSchema = z.object({
  id_token_hint: z.string().optional(),
  post_logout_redirect_uri: z.string().optional(),
  state: z.string().optional(),
})

/**
 * OIDC end_session_endpoint as a page: the same-origin API call below
 * carries the session cookie that the cross-site GET from Open WebUI did
 * not, so the new-api session really ends. The server decides where to
 * send the browser next (registered post-logout URI or /sign-in).
 */
function OidcLogoutPage() {
  const { t } = useTranslation()
  const search = Route.useSearch()

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      let target = '/sign-in'
      try {
        const res = await logoutOidc(search)
        if (res?.data?.redirect_url) target = res.data.redirect_url
      } catch {
        /* fall back to /sign-in */
      }
      if (cancelled) return
      useAuthStore.getState().auth.reset()
      window.location.replace(target)
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <AuthLayout>
      <div className='mx-auto flex w-full max-w-sm flex-col items-center gap-4 text-center'>
        <Loader2 className='text-muted-foreground size-6 animate-spin' />
        <p className='text-muted-foreground text-sm'>
          {t('Signing you out...')}
        </p>
      </div>
    </AuthLayout>
  )
}

export const Route = createFileRoute('/oauth2/logout')({
  validateSearch: searchSchema,
  component: OidcLogoutPage,
})
