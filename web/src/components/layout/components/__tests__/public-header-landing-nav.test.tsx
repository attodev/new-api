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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'

import { NavLinkItem } from '../nav-link-item'
import { PublicHeader } from '../public-header'
import { SystemBrand } from '../system-brand'
import { TopNav } from '../top-nav'

// fork: O11 - the public header logo and the Home link must leave the SPA for
// the server-rendered landing page (full page load) instead of a router Link;
// the signed-in SystemBrand logo stays a router Link to /dashboard.

function Page() {
  return <PublicHeader showNotifications={false} />
}

async function renderHeader() {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: { success: true, data: url === '/api/notice' ? '' : {} },
  }))
  const root = createRootRoute({ component: Page })
  const index = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => null,
  })
  const dashboard = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    component: () => null,
  })
  const router = createRouter({
    routeTree: root.addChildren([index, dashboard]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return router
}

function logoLink() {
  const link = document.querySelector('header nav a')
  if (!link) throw new Error('logo link not found')
  return link as HTMLAnchorElement
}

afterEach(() => {
  cleanup()
  useAuthStore.getState().auth.reset()
})

describe('public header landing navigation', () => {
  it('sends the signed-out logo to the landing page with a plain anchor', async () => {
    await renderHeader()
    const logo = await waitFor(logoLink)
    expect(logo.getAttribute('href')).toBe('/')
    // A TanStack Link on the active route would carry data-status.
    expect(logo.hasAttribute('data-status')).toBe(false)
  })

  it('renders the Home link as a plain anchor to the landing page', async () => {
    await renderHeader()
    const homeLinks = await screen.findAllByRole('link', { name: 'Home' })
    expect(homeLinks.length).toBeGreaterThan(0)
    for (const home of homeLinks) {
      expect(home.getAttribute('href')).toBe('/')
      expect(home.hasAttribute('data-status')).toBe(false)
    }
  })

  it('sends the signed-in logo to the dashboard through the router', async () => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'alice',
      role: 1,
    } as AuthUser)
    const router = await renderHeader()
    const logo = await waitFor(logoLink)
    expect(logo.getAttribute('href')).toBe('/dashboard')
    await userEvent.click(logo)
    await waitFor(() =>
      expect(router.state.location.pathname).toBe('/dashboard')
    )
  })
})

describe('forceReload nav links outside the public header', () => {
  const home = { title: 'Home', href: '/', forceReload: true }

  it('renders TopNav forceReload links as same-tab plain anchors', () => {
    render(<TopNav links={[home]} />)
    const link = screen.getByRole('link', { name: 'Home' })
    expect(link.getAttribute('href')).toBe('/')
    expect(link.hasAttribute('target')).toBe(false)
    expect(link.hasAttribute('data-status')).toBe(false)
  })

  it('renders NavLinkItem forceReload links as plain anchors', () => {
    render(<NavLinkItem link={home} />)
    const link = screen.getByRole('link', { name: 'Home' })
    expect(link.getAttribute('href')).toBe('/')
    expect(link.hasAttribute('target')).toBe(false)
  })
})

describe('SystemBrand inline variant', () => {
  // Mounted only in the signed-in layout: the logo stays inside the SPA.
  it('links to the dashboard through the router', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: {} },
    })
    const root = createRootRoute({
      component: () => <SystemBrand variant='inline' />,
    })
    const index = createRoute({
      getParentRoute: () => root,
      path: '/',
      component: () => null,
    })
    const dashboard = createRoute({
      getParentRoute: () => root,
      path: '/dashboard',
      component: () => null,
    })
    const router = createRouter({
      routeTree: root.addChildren([index, dashboard]),
      history: createMemoryHistory({ initialEntries: ['/'] }),
    })
    await router.load()
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    const link = await screen.findByRole('link', { name: 'Go to home' })
    expect(link.getAttribute('href')).toBe('/dashboard')
    expect(link.hasAttribute('target')).toBe(false)
    await userEvent.click(link)
    await waitFor(() =>
      expect(router.state.location.pathname).toBe('/dashboard')
    )
  })
})
