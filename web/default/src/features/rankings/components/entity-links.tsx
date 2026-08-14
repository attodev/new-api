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
import { Link } from '@tanstack/react-router'
import { cn } from '@/lib/utils'

type EntityLinkBaseProps = {
  className?: string
  children?: React.ReactNode
}

type ModelLinkProps = EntityLinkBaseProps & {
  /** model_name as it appears in the pricing API. */
  modelName: string
}

/**
 * Renders a model name without linking to a model details page. Model
 * details are intentionally unavailable in the public marketplace.
 */
export function ModelLink(props: ModelLinkProps) {
  return (
    <span className={cn(props.className)}>
      {props.children ?? props.modelName}
    </span>
  )
}

type VendorLinkProps = EntityLinkBaseProps & {
  /** Display name of the vendor (e.g. "Google", "OpenAI"). */
  vendor: string
}

/**
 * Link wrapping a vendor name. Navigates to the pricing page filtered by
 * that vendor (`/pricing?vendor={vendor}`). Renders the vendor name itself
 * by default.
 */
export function VendorLink(props: VendorLinkProps) {
  return (
    <Link
      to='/pricing'
      search={{ vendor: props.vendor }}
      className={cn(
        'hover:text-foreground underline decoration-current/40 decoration-1 underline-offset-2 transition-colors hover:decoration-current',
        props.className
      )}
    >
      {props.children ?? props.vendor}
    </Link>
  )
}
