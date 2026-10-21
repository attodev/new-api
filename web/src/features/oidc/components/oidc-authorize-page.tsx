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
// fork: O13 — OIDC authorization endpoint page (/oauth2/authorize).
import { useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { AuthLayout } from '@/features/auth/auth-layout'
import {
  clearAuthentication,
  refreshAuthentication,
  resolveAuthentication,
} from '@/lib/auth-session'
import { getServerMessage } from '@/lib/server-error-message'

import { authorizeOidc, readAuthorizeParams } from '../api'
import { replaceLocation } from '../lib/location'

type Phase =
  | { kind: 'working' }
  | { kind: 'email_required'; message?: string }
  | { kind: 'invalid'; message?: string }

/**
 * OIDC authorization endpoint, rendered as a page: Open WebUI sends the
 * browser here, and only the SPA holds the dashboard session (an in-memory
 * Bearer token restored from the refresh cookie). The page restores that
 * session, then calls the same-origin authorize API and acts on the answer:
 * no session -> sign in and come back to this exact URL; success or a
 * redirectable OAuth error -> follow the server's redirect; any other error
 * -> explain it here and never trust redirect_uri.
 */
export function OidcAuthorizePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [phase, setPhase] = useState<Phase>({ kind: 'working' })
  const started = useRef(false)

  useEffect(() => {
    // One authorization code per visit, also under StrictMode's double effect.
    if (started.current) return
    started.current = true

    const here = window.location.pathname + window.location.search
    const toSignIn = () =>
      void navigate({
        to: '/sign-in',
        search: { redirect: here },
        replace: true,
      })

    void (async () => {
      const outcome = await resolveAuthentication()
      if (outcome.kind === 'transient_error') {
        setPhase({ kind: 'invalid', message: t('Request failed') })
        return
      }
      if (outcome.kind !== 'authenticated') {
        toSignIn()
        return
      }

      // Read the raw query: the router JSON-parses search values, which
      // could turn number-like state/nonce values into numbers.
      const params = readAuthorizeParams(window.location.search)
      let { status, body } = await authorizeOidc(params)
      if (status === 401) {
        // The access token may have expired since it was checked. Refresh it
        // from the refresh cookie and try once more before asking for a
        // password.
        const refreshed = await refreshAuthentication()
        if (refreshed.kind === 'transient_error') {
          setPhase({ kind: 'invalid', message: t('Request failed') })
          return
        }
        if (refreshed.kind === 'authenticated') {
          ;({ status, body } = await authorizeOidc(params))
        }
      }
      if (status === 401) {
        // The server refused this session (revoked, disabled user) or it could
        // not be refreshed. Drop it so sign-in asks for credentials instead of
        // bouncing back here.
        clearAuthentication(false)
        toSignIn()
        return
      }

      const redirectUrl = body?.data?.redirect_url
      if (redirectUrl) {
        replaceLocation(redirectUrl)
        return
      }
      // No body: the request itself failed (network, proxy), not the OAuth check.
      const message = body ? getServerMessage(body) : t('Request failed')
      setPhase(
        body?.error === 'email_required'
          ? { kind: 'email_required', message }
          : { kind: 'invalid', message }
      )
    })()
  }, [navigate, t])

  return (
    <AuthLayout>
      <div className='mx-auto flex w-full max-w-sm flex-col items-center gap-4 text-center'>
        {phase.kind === 'working' && (
          <>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
            <p className='text-muted-foreground text-sm'>
              {t('Signing you in to the external app...')}
            </p>
          </>
        )}
        {phase.kind === 'email_required' && (
          <>
            <h2 className='text-lg font-semibold'>
              {t('Email address required')}
            </h2>
            <p className='text-muted-foreground text-sm'>
              {phase.message ||
                t(
                  'Register an email address in your profile before signing in to the external app'
                )}
            </p>
            <Button onClick={() => void navigate({ to: '/profile' })}>
              {t('Go to profile')}
            </Button>
          </>
        )}
        {phase.kind === 'invalid' && (
          <>
            <h2 className='text-lg font-semibold'>
              {t('Sign-in request rejected')}
            </h2>
            <p className='text-muted-foreground text-sm'>
              {phase.message ||
                t(
                  'The sign-in request is not valid, please contact your administrator'
                )}
            </p>
            <Button
              variant='outline'
              onClick={() => void navigate({ to: '/' })}
            >
              {t('Back to Home')}
            </Button>
          </>
        )}
      </div>
    </AuthLayout>
  )
}
