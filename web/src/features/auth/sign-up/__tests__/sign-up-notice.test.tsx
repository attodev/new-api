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
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { SignUp } from '../index'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href='/'>{children}</a>,
}))
const mockStatus = vi.hoisted(() => ({ value: {} as Record<string, unknown> }))
vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: mockStatus.value }),
}))
vi.mock('../../auth-layout', () => ({
  AuthLayout: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))
vi.mock('../../components/terms-footer', () => ({ TermsFooter: () => null }))
vi.mock('../components/sign-up-form', () => ({ SignUpForm: () => null }))

describe('SignUp', () => {
  it('shows the contracted-domains-only notice when the whitelist is on', () => {
    mockStatus.value = { email_domain_restriction_enabled: true }
    render(<SignUp />)
    expect(screen.getByText('Create an account')).toBeInTheDocument()
    expect(
      screen.getByText('Only contracted domains can sign up')
    ).toBeInTheDocument()
  })

  it('hides the notice when the whitelist is off or unknown', () => {
    mockStatus.value = {}
    render(<SignUp />)
    expect(screen.getByText('Create an account')).toBeInTheDocument()
    expect(screen.queryByText('Only contracted domains can sign up')).toBeNull()
  })
})
