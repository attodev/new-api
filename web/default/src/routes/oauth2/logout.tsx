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
import { useEffect, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { AuthLayout } from '@/features/auth/auth-layout'
import {
  logoutOidc,
  type OidcLogoutParams,
  type OidcLogoutResponse,
} from '@/features/oidc/api'

function readLogoutParams(): OidcLogoutParams {
  // Raw query, not Route.useSearch(): the router JSON-parses values.
  const qs = new URLSearchParams(window.location.search)
  return {
    id_token_hint: qs.get('id_token_hint') ?? undefined,
    post_logout_redirect_uri: qs.get('post_logout_redirect_uri') ?? undefined,
    state: qs.get('state') ?? undefined,
  }
}

/**
 * Follows the server's answer when it ended the session. Returns false when
 * it did not (needs_confirmation or an unexpected reply). The redirect_url
 * is server-supplied: the registered post-logout URI or /sign-in.
 */
function finishIfLoggedOut(res: OidcLogoutResponse | null): boolean {
  const redirectUrl = res?.data?.redirect_url
  if (!redirectUrl) return false
  useAuthStore.getState().auth.reset()
  window.location.replace(redirectUrl)
  return true
}

/**
 * OIDC end_session_endpoint as a page: the same-origin API call below
 * carries the session cookie that the cross-site GET from Open WebUI did
 * not, so the new-api session really ends. Without a verifiable
 * id_token_hint the server does not log out on its own and asks for
 * confirmation (RP-Initiated Logout §2), which this page then collects.
 */
function OidcLogoutPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [phase, setPhase] = useState<'working' | 'confirm'>('working')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      let res: OidcLogoutResponse | null = null
      try {
        res = await logoutOidc(readLogoutParams())
      } catch {
        /* nothing ended: let the user decide below */
      }
      if (cancelled || finishIfLoggedOut(res)) return
      setPhase('confirm')
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const confirmSignOut = async () => {
    setSubmitting(true)
    let res: OidcLogoutResponse | null = null
    try {
      res = await logoutOidc({ ...readLogoutParams(), confirmed: true })
    } catch {
      /* reported below */
    }
    if (finishIfLoggedOut(res)) return
    setSubmitting(false)
    toast.error(t('Request failed'))
  }

  return (
    <AuthLayout>
      <div className='mx-auto flex w-full max-w-sm flex-col items-center gap-4 text-center'>
        {phase === 'working' && (
          <>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
            <p className='text-muted-foreground text-sm'>
              {t('Signing you out...')}
            </p>
          </>
        )}
        {phase === 'confirm' && (
          <>
            <h2 className='text-lg font-semibold'>
              {t('Sign out of new-api?')}
            </h2>
            <p className='text-muted-foreground text-sm'>
              {t(
                'The external app asked to end your session here. Confirm to sign out of this account too.'
              )}
            </p>
            <div className='flex flex-wrap justify-center gap-2'>
              <Button onClick={confirmSignOut} disabled={submitting}>
                {submitting && <Loader2 className='size-4 animate-spin' />}
                {t('Sign out')}
              </Button>
              <Button
                variant='outline'
                onClick={() => navigate({ to: '/' })}
                disabled={submitting}
              >
                {t('Stay signed in')}
              </Button>
            </div>
          </>
        )}
      </div>
    </AuthLayout>
  )
}

export const Route = createFileRoute('/oauth2/logout')({
  // Permissive on purpose: the page reads window.location.search itself.
  validateSearch: (search: Record<string, unknown>) => search,
  component: OidcLogoutPage,
})
