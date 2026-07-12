/*
Copyright (C) 2025 QuantumNous

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

import { describe, expect, test } from 'bun:test';
import { getClassicTossTarget } from './tossTargetRouting';

describe('classic Toss target routing', () => {
  test('keeps users without an organization on personal endpoints', () => {
    expect(getClassicTossTarget({ organization_id: 0 })).toEqual({
      scope: 'user',
      canManage: true,
      allowPersonalBilling: true,
      topUpApiBase: '/api/user',
    });
  });

  test('routes organization owners to organization wallet endpoints', () => {
    expect(
      getClassicTossTarget({
        organization_id: 7,
        organization_role: 'owner',
      }),
    ).toEqual({
      scope: 'organization',
      canManage: true,
      allowPersonalBilling: false,
      topUpApiBase: '/api/organization',
    });
  });

  test('does not expose owner-only Toss actions to other organization roles', () => {
    for (const organization_role of ['member', 'admin', '']) {
      const target = getClassicTossTarget({
        organization_id: 7,
        organization_role,
      });
      expect(target.scope).toBe('organization');
      expect(target.canManage).toBe(false);
      expect(target.allowPersonalBilling).toBe(false);
    }
  });
});
