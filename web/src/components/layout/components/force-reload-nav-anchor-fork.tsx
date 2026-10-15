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
import type { ComponentProps } from 'react'

import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

import type { TopNavLink } from '../types'

// fork: O11 - Home must be a full page load (plain anchor, not a router Link)
// so the Go server renders the landing page instead of the SPA index route.
type ForceReloadNavAnchorProps = Omit<ComponentProps<'a'>, 'href'> & {
  href: string
  disabled?: boolean
}

export function ForceReloadNavAnchor({
  disabled,
  className,
  ...props
}: ForceReloadNavAnchorProps) {
  return (
    <a
      {...props}
      aria-disabled={disabled}
      tabIndex={disabled ? -1 : props.tabIndex}
      className={cn(className, disabled && 'pointer-events-none opacity-50')}
    />
  )
}

// Public header variant: the desktop pill styling is the default; the mobile
// overlay passes its own className/style.
type ForceReloadPublicNavLinkProps = {
  link: TopNavLink
  label: string
  isActive: boolean
  onClick: ComponentProps<'a'>['onClick']
  className?: string
  style?: ComponentProps<'a'>['style']
}

export function ForceReloadPublicNavLink({
  link,
  label,
  isActive,
  onClick,
  className,
  style,
}: ForceReloadPublicNavLinkProps) {
  return (
    <ForceReloadNavAnchor
      href={link.href}
      title={className ? undefined : label}
      disabled={link.disabled}
      onClick={onClick}
      style={style}
      className={
        className ??
        cn(
          'min-w-0 truncate rounded-lg px-3 py-1.5 text-sm font-medium transition-colors duration-200',
          isActive
            ? 'text-foreground'
            : 'text-muted-foreground hover:text-foreground'
        )
      }
    >
      {label}
    </ForceReloadNavAnchor>
  )
}

// Generic NavLinkItem variant (plain same-tab anchor).
export function ForceReloadNavLinkItem({
  link,
  className,
}: {
  link: TopNavLink
  className?: string
}) {
  return (
    <ForceReloadNavAnchor
      href={link.href}
      className={className}
      disabled={link.disabled}
    >
      {link.title}
    </ForceReloadNavAnchor>
  )
}

// TopNav: forceReload links are pulled out of the upstream link list
// (`withoutForceReload`) and rendered here, ahead of the remaining links. Home
// is the only such link and is always first.
export function ForceReloadTopNavItems({
  links,
  variant,
}: {
  links: TopNavLink[]
  variant: 'menu' | 'bar'
}) {
  return links
    .filter((link) => link.forceReload)
    .map(({ title, href, isActive, disabled }) => {
      const key = `${title}-${href}`
      if (variant === 'menu') {
        return (
          <DropdownMenuItem
            key={key}
            render={
              <ForceReloadNavAnchor
                href={href}
                disabled={disabled}
                className={!isActive ? 'text-muted-foreground' : ''}
              >
                {title}
              </ForceReloadNavAnchor>
            }
          />
        )
      }
      return (
        <ForceReloadNavAnchor
          key={key}
          href={href}
          disabled={disabled}
          className={`hover:text-primary text-sm font-medium transition-colors ${isActive ? '' : 'text-muted-foreground'}`}
        >
          {title}
        </ForceReloadNavAnchor>
      )
    })
}
