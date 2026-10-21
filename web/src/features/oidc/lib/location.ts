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
// fork: O13 — document-level navigation used by the OIDC pages and the chat app entry.
// Kept in one module so tests can replace it (jsdom cannot navigate).

/** Leave the SPA for `url`, replacing the current history entry. */
export function replaceLocation(url: string): void {
  window.location.replace(url)
}

/** Leave the SPA for `url`, keeping the current page in history. */
export function assignLocation(url: string): void {
  window.location.assign(url)
}

/** Open `url` in a new tab without giving it a handle back to this page. */
export function openNewWindow(url: string): void {
  window.open(url, '_blank', 'noopener,noreferrer')
}
