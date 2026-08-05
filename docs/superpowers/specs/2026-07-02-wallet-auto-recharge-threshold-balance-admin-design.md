# 자동충전 기준 잔액 관리자 입력 설계

## 목표

관리자가 자동충전 기준값을 raw quota 숫자가 아니라 지갑 화면에서 보이는 잔액 단위로 관리하게 한다.

## 배경

현재 자동충전 기준은 DB/API에서 `threshold_quota` raw quota 값으로 저장된다. 사용자 지갑과 자동충전 카드에서는 이 값을 `formatQuota()`로 변환해 표시하므로, 관리자가 직접 `500000` 같은 raw quota를 입력해야 하는 화면은 지갑 잔액 표시와 일관성이 떨어진다.

## 설계

- DB/API 필드와 비교 로직은 그대로 `threshold_quota`를 사용한다.
- 관리자 화면에서 기준값을 보여줄 때는 `quotaUnitsToDollars(threshold_quota)`로 지갑 잔액 단위 숫자로 변환한다.
- 관리자 화면에서 저장할 때는 `parseQuotaFromDollars(input)`로 다시 raw quota로 변환해 `threshold_quota`에 담는다.
- 관리자 옵션 일괄 편집의 기준값 목록도 지갑 잔액 숫자 목록으로 입력받고, 저장 계획 생성 전에 raw quota 목록으로 변환한다.
- 사용자 지갑/자동충전 카드 표시는 기존처럼 `formatQuota(threshold_quota)`를 유지한다.
- 기존 raw quota 데이터는 화면에서 잔액 단위로 변환되어 보이므로 별도 마이그레이션은 필요 없다.

## 변경 범위

- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
- `web/default/src/features/wallet/lib/auto-recharge-options.ts`
- `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`
- 필요한 경우 i18n locale JSON

## 검증

- 관리자 form normalize 테스트에서 `threshold_quota: "1"` 입력이 raw quota `500000`으로 저장되는지 확인한다.
- 기존 preset의 `threshold_quota: 500000`이 form state에서 `"1"`로 표시되는지 확인한다.
- 옵션 일괄 편집에서 기준 잔액 `1, 5`가 raw quota `500000, 2500000` 저장 계획으로 변환되는지 확인한다.
- 기존 자동충전 카드 사용자 UI 테스트가 계속 통과하는지 확인한다.
