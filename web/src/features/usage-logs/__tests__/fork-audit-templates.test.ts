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
import i18next from 'i18next'
import { describe, expect, test } from 'vitest'

import en from '@/i18n/locales/en.json'
import ko from '@/i18n/locales/ko.json' // fork: O9

import { FORK_AUDIT_TEMPLATES } from '../lib/fork-audit-templates'
import { renderAuditContent } from '../lib/format'
import type { LogOtherData } from '../types'

// fork: O3 — params mirror the Go call sites (recordOrganizationAudit with
// organizationAuditParams, and controller/topup_expire_admin.go), including the
// changed-field keys merged into the organization/member update entries.
const CASES: Array<{
  action: string
  params: Record<string, string | number | boolean>
  expected: string
}> = [
  {
    action: 'organization.create',
    params: {
      organization_id: 4,
      name: 'Atto Lab',
      owner_user_id: 9,
      quota: 5000,
    },
    expected: 'Created organization “Atto Lab” (ID 4) with owner 9',
  },
  {
    action: 'organization.update',
    params: { organization_id: 4, name: 'Atto Research', quota_limit: 100 },
    expected: 'Updated organization 4',
  },
  {
    action: 'organization.delete',
    params: { organization_id: 4, owner_user_id: 9, previous_quota: 300 },
    expected: 'Deleted organization 4 (owner 9)',
  },
  {
    action: 'organization.user.assign',
    params: {
      organization_id: 4,
      user_id: 12,
      organization_role: 'member',
      previous_organization_id: 0,
    },
    expected: 'Assigned user 12 to organization 4 as member',
  },
  {
    action: 'organization.user.update',
    params: { organization_id: 4, user_id: 12, organization_member_status: 2 },
    expected: 'Updated member 12 of organization 4',
  },
  {
    action: 'organization.user.remove',
    params: {
      organization_id: 4,
      user_id: 12,
      organization_role: 'member',
      previous_quota: 70,
    },
    expected: 'Removed user 12 from organization 4',
  },
  {
    action: 'organization.user.import',
    params: {
      organization_id: 4,
      remove_absent: false,
      assigned: 3,
      created: 2,
      updated: 1,
      removed: 0,
      errors: 1,
    },
    expected:
      'Imported members into organization 4 from Excel (assigned 3, created 2, updated 1, removed 0, errors 1)',
  },
  {
    action: 'organization.plan.create',
    params: {
      organization_id: 4,
      plan_id: 8,
      title: 'Team Monthly',
      total_amount: 1000,
      duration_unit: 'month',
      duration_value: 1,
      custom_seconds: 0,
    },
    expected:
      'Created subscription plan “Team Monthly” (ID 8) for organization 4',
  },
  {
    action: 'organization.plan.update',
    params: {
      organization_id: 4,
      plan_id: 8,
      total_amount: 2000,
      custom_seconds: 0,
    },
    expected: 'Updated subscription plan 8 of organization 4',
  },
  {
    action: 'organization.plan.disable',
    params: { organization_id: 4, plan_id: 8 },
    expected: 'Disabled subscription plan 8 of organization 4',
  },
  {
    action: 'organization.subscription.assign',
    params: {
      organization_id: 4,
      user_id: 12,
      plan_id: 8,
      subscription_id: 31,
      amount_total: 1000,
    },
    expected: 'Assigned subscription plan 8 of organization 4 to user 12',
  },
  {
    action: 'organization.subscription.cancel',
    params: { organization_id: 4, user_id: 12 },
    expected: 'Cancelled the active subscription of user 12 in organization 4',
  },
  {
    action: 'organization.topup.expire',
    params: { organization_id: 4, expired: 5, skipped_recent: 1 },
    expected: 'Expired 5 pending top-up orders of organization 4',
  },
  {
    action: 'topup.expire',
    params: {
      user_id: 12,
      expired: 5,
      skipped_recent: 1,
      expired_subscriptions: 2,
      skipped_recent_subscriptions: 0,
    },
    expected: 'Expired 5 pending top-ups and 2 subscription orders of user 12',
  },
]

const KO_EXPECTED: Record<string, string> = {
  'organization.user.update': '조직 4의 멤버 12 정보를 수정했습니다',
  'organization.topup.expire':
    '조직 4의 대기 중인 충전 주문 5건을 만료 처리했습니다',
}

function other(action: string, params: CaseParams): LogOtherData {
  return { op: { action, params } } as LogOtherData
}
type CaseParams = Record<string, string | number | boolean>

describe('fork organization / top-up expiry audit templates', () => {
  test('covers exactly the 14 fork audit actions', () => {
    expect(Object.keys(FORK_AUDIT_TEMPLATES).sort()).toEqual(
      CASES.map((c) => c.action).sort()
    )
  })

  test.each(CASES)('renders $action as an English sentence', (testCase) => {
    const rendered = renderAuditContent(
      other(testCase.action, testCase.params),
      (key, opts) => i18next.t(key, opts)
    )
    expect(rendered).not.toBeNull()
    expect(rendered).not.toContain('{{')
    expect(rendered).toBe(testCase.expected)
  })

  test('every template is a key of both en.json and ko.json', () => {
    const enKeys = en.translation as Record<string, string>
    const koKeys = ko.translation as Record<string, string>
    for (const template of Object.values(FORK_AUDIT_TEMPLATES)) {
      expect(enKeys[template], template).toBe(template)
      expect(koKeys[template], template).toMatch(/[가-힣]/)
    }
  })

  test('renders fork audit actions in Korean', async () => {
    i18next.addResourceBundle('ko', 'translation', ko.translation)
    await i18next.changeLanguage('ko')
    try {
      for (const testCase of CASES) {
        const rendered = renderAuditContent(
          other(testCase.action, testCase.params),
          (key, opts) => i18next.t(key, opts)
        )
        expect(rendered).not.toBeNull()
        expect(rendered).not.toContain('{{')
        expect(rendered).toMatch(/[가-힣]/)
        const expected = KO_EXPECTED[testCase.action]
        if (expected) expect(rendered).toBe(expected)
      }
    } finally {
      await i18next.changeLanguage('en')
    }
  })
})
