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
import { describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SidebarModulesCard } from '../components/sidebar-modules-card'

describe('SidebarModulesCard labels', () => {
  it('uses the short section and module labels', () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: { success: false } })
    render(<SidebarModulesCard />)
    for (const label of [
      'General',
      'Personal',
      'Overview · Dashboard',
      'API Keys',
      'Wallet',
      'Profile',
    ]) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0)
    }
    // Chat is both the section title and the module title.
    expect(screen.getAllByText('Chat')).toHaveLength(2)
    for (const old of [
      'Chat Area',
      'Console Area',
      'Dashboard',
      'Token Management',
      'Personal Center Area',
      'Wallet Management',
      'Personal Settings',
    ]) {
      expect(screen.queryByText(old)).toBeNull()
    }
  })
})
