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
import { useEffect, useRef } from 'react'
import { TossPaymentLifecycle } from '../lib/toss-payment-lifecycle'

export function useTossPaymentLifecycle(): TossPaymentLifecycle {
  const lifecycleRef = useRef<TossPaymentLifecycle | null>(null)
  if (!lifecycleRef.current) {
    lifecycleRef.current = new TossPaymentLifecycle()
  }

  useEffect(() => {
    const lifecycle = lifecycleRef.current!
    lifecycle.activate()
    return () => lifecycle.deactivate()
  }, [])

  return lifecycleRef.current
}
