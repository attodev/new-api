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
import { createElement, type ComponentProps, type MouseEvent } from 'react'

import type { TopNavLink } from '../types'
import { ForceReloadPublicNavLink } from './force-reload-nav-anchor-fork'

// fork: O11 - keeps TopNav's upstream router-Link/external markup untouched.
export function withoutForceReload<T extends TopNavLink>(links: T[]): T[] {
  return links.filter((link) => !link.forceReload)
}

// Keeps the PublicHeader call sites to a few tagged lines: the header builds
// this renderer once from its `t` and click handler, and each `links.map`
// returns `plainLink(link, isActive)` (desktop pill) or `plainLink(link,
// isActive, linkClassName, transitionStyle)` (mobile overlay, which also
// closes on click) for forceReload links.
type PublicNavClickHandler = (
  event: MouseEvent<HTMLAnchorElement>,
  link: TopNavLink,
  closeMobile?: boolean
) => void

export function createForceReloadPublicNavLink(
  t: (key: string) => string,
  onNavClick: PublicNavClickHandler
) {
  return (
    link: TopNavLink,
    isActive: boolean,
    className?: string,
    style?: ComponentProps<'a'>['style']
  ) =>
    createElement(ForceReloadPublicNavLink, {
      key: `${link.title}:${link.href}`,
      link,
      label: t(link.title),
      isActive,
      onClick: (event) => onNavClick(event, link, className !== undefined),
      className,
      style,
    })
}
