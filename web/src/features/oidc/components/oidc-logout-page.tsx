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
// fork: O13 — OIDC end-session endpoint page (/oauth2/logout).
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { logout } from '@/features/auth/api'
import { AuthLayout } from '@/features/auth/auth-layout'
import {
  clearAuthenticatedClientState,
  resolveAuthentication,
} from '@/lib/auth-session'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { logoutOidc, readLogoutParams, type OidcLogoutResponse } from '../api'
import { replaceLocation } from '../lib/location'

/**
 * Finish when the server ended the session; returns false when it did not
 * (needs_confirmation or an unexpected reply). The OIDC logout revoked the
 * session the Bearer token named. The normal sign-out request runs as well:
 * it is the only request that carries the refresh cookie (Path=/api/user/auth),
 * so it clears the cookie even when the Bearer token was stale. Its result
 * does not matter, the user is leaving either way. The redirect_url is
 * server-supplied: the registered post-logout URI or /sign-in.
 */
async function finishIfLoggedOut(
  res: OidcLogoutResponse | null,
  queryClient: QueryClient
): Promise<boolean> {
  const redirectUrl = res?.data?.redirect_url
  if (!redirectUrl) return false
  try {
    await logout()
  } catch {
    /* best effort: the OIDC logout already ended the session */
  }
  clearAuthenticatedClientState(queryClient)
  replaceLocation(redirectUrl)
  return true
}

/**
 * OIDC end_session_endpoint as a page. Open WebUI sends the browser here; the
 * page restores the dashboard session first (the end-session API identifies
 * it by the Bearer token only), then asks the server to end it. Without a
 * verifiable id_token_hint the server does not log out on its own and asks
 * for confirmation (RP-Initiated Logout §2), which this page then collects.
 */
export function OidcLogoutPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [phase, setPhase] = useState<'working' | 'confirm'>('working')
  const [submitting, setSubmitting] = useState(false)
  const started = useRef(false)

  useEffect(() => {
    // One end-session request per visit, also under StrictMode's double effect.
    if (started.current) return
    started.current = true

    void (async () => {
      await resolveAuthentication()
      let res: OidcLogoutResponse | null = null
      try {
        res = await logoutOidc(readLogoutParams(window.location.search))
      } catch {
        /* nothing ended: let the user decide below */
      }
      if (await finishIfLoggedOut(res, queryClient)) return
      setPhase('confirm')
    })()
  }, [queryClient])

  const confirmSignOut = async () => {
    setSubmitting(true)
    let res: OidcLogoutResponse | null = null
    let failure: unknown = null
    try {
      res = await logoutOidc({
        ...readLogoutParams(window.location.search),
        confirmed: true,
      })
    } catch (error: unknown) {
      failure = error
    }
    if (await finishIfLoggedOut(res, queryClient)) return
    setSubmitting(false)
    toast.error(getServerErrorMessage(failure ?? res, t('Request failed')))
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
              <Button
                onClick={() => void confirmSignOut()}
                disabled={submitting}
              >
                {submitting && <Loader2 className='size-4 animate-spin' />}
                {t('Sign out')}
              </Button>
              <Button
                variant='outline'
                onClick={() => void navigate({ to: '/' })}
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
