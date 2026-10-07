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
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { AuthLayout } from '@/features/auth/auth-layout'
import { authorizeOidc, type OidcAuthorizeParams } from '@/features/oidc/api'

type Phase =
  | { kind: 'working' }
  | { kind: 'email_required' }
  | { kind: 'invalid'; message: string }

/**
 * OIDC authorization endpoint, rendered as a page because the cross-site
 * navigation from Open WebUI carries no SameSite=Strict session cookie.
 * The page immediately calls the same-origin authorize API and acts on
 * the answer: 401 → sign in and come back; success → follow the redirect;
 * redirectable error → follow the redirect (it carries ?error=);
 * non-redirectable error → explain here and never trust redirect_uri.
 */
function OidcAuthorizePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [phase, setPhase] = useState<Phase>({ kind: 'working' })

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      // Read the raw query: the router JSON-parses search values, which
      // would turn numeric/boolean-looking client_id/state/nonce into
      // non-strings and change what the server sees.
      const qs = new URLSearchParams(window.location.search)
      const params: OidcAuthorizeParams = {
        response_type: qs.get('response_type') ?? undefined,
        client_id: qs.get('client_id') ?? undefined,
        redirect_uri: qs.get('redirect_uri') ?? undefined,
        scope: qs.get('scope') ?? undefined,
        state: qs.get('state') ?? undefined,
        nonce: qs.get('nonce') ?? undefined,
        code_challenge: qs.get('code_challenge') ?? undefined,
        code_challenge_method: qs.get('code_challenge_method') ?? undefined,
      }
      const { status, body } = await authorizeOidc(params)
      if (cancelled) return

      if (status === 401) {
        // Not logged in (or the New-Api-User header was missing because the
        // auth store is empty): send to sign-in and return to this exact URL.
        useAuthStore.getState().auth.reset()
        const here = window.location.pathname + window.location.search
        navigate({ to: '/sign-in', search: { redirect: here }, replace: true })
        return
      }

      const redirectUrl = body?.data?.redirect_url
      if (redirectUrl) {
        window.location.replace(redirectUrl)
        return
      }
      if (body?.error === 'email_required') {
        setPhase({ kind: 'email_required' })
        return
      }
      setPhase({
        kind: 'invalid',
        message:
          body?.message ||
          t(
            'The sign-in request is not valid, please contact your administrator'
          ),
      })
    })()
    return () => {
      cancelled = true
    }
    // The search params are stable for the life of this page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
              {t(
                'The external app needs your email address. Register one in your profile, then open the external app again.'
              )}
            </p>
            <Button onClick={() => navigate({ to: '/profile' })}>
              {t('Go to profile')}
            </Button>
          </>
        )}
        {phase.kind === 'invalid' && (
          <>
            <h2 className='text-lg font-semibold'>
              {t('Sign-in request rejected')}
            </h2>
            <p className='text-muted-foreground text-sm'>{phase.message}</p>
            <Button variant='outline' onClick={() => navigate({ to: '/' })}>
              {t('Back to home')}
            </Button>
          </>
        )}
      </div>
    </AuthLayout>
  )
}

export const Route = createFileRoute('/oauth2/authorize')({
  // Permissive on purpose: the page reads window.location.search itself.
  validateSearch: (search: Record<string, unknown>) => search,
  component: OidcAuthorizePage,
})
