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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { loadTossPayments } from '@tosspayments/tosspayments-sdk';
import {
  Button,
  Card,
  Modal,
  Skeleton,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showError, showInfo, showSuccess } from '../../helpers';
import {
  getTossPaymentWindowTargetOptions,
  getValidTossBillingTradeNo,
  isTossUserCancellation,
} from './tossSubscriptionCheckout';
import { useTossPaymentLifecycle } from './tossPaymentLifecycle';
import {
  hasValidWalletAutoRechargeFingerprint,
  isUserVisibleWalletAutoRechargePolicy,
  walletAutoRechargeSessionMatchesPreset,
} from './tossWalletAutoRecharge';

const { Text } = Typography;

export default function WalletAutoRechargeCard({
  t,
  enabled,
  scope = 'user',
  creationDisabled = false,
}) {
  const [policies, setPolicies] = useState([]);
  const [presets, setPresets] = useState([]);
  const [loading, setLoading] = useState(true);
  const [processing, setProcessing] = useState(false);
  const processingRef = useRef(false);
  const tossPaymentLifecycle = useTossPaymentLifecycle();
  const isOrganization = scope === 'organization';
  const apiBase = isOrganization
    ? '/api/organization/wallet/auto-recharge'
    : '/api/user/wallet/auto-recharge';
  const presetTargetScope = isOrganization ? 'organization' : 'user';

  const refresh = useCallback(async () => {
    try {
      const [policyResponse, presetResponse] = await Promise.all([
        API.get(apiBase),
        API.get(`${apiBase}/presets`),
      ]);
      setPolicies(
        policyResponse.data?.success && Array.isArray(policyResponse.data?.data)
          ? policyResponse.data.data
          : [],
      );
      setPresets(
        presetResponse.data?.success && Array.isArray(presetResponse.data?.data)
          ? presetResponse.data.data
          : [],
      );
    } catch {
      setPolicies([]);
      setPresets([]);
    } finally {
      setLoading(false);
    }
  }, [apiBase]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const livePolicies = useMemo(
    () => policies.filter(isUserVisibleWalletAutoRechargePolicy),
    [policies],
  );
  const availablePresets = useMemo(
    () =>
      presets.filter(
        (preset) =>
          preset.enabled === true &&
          (preset.type === 'scheduled' || preset.type === 'threshold') &&
          (preset.target_scope === presetTargetScope ||
            preset.target_scope === 'all') &&
          hasValidWalletAutoRechargeFingerprint(preset),
      ),
    [presetTargetScope, presets],
  );

  const cancelPending = async (tradeNo) => {
    if (!tradeNo) return;
    try {
      await API.delete(`${apiBase}/pending/${encodeURIComponent(tradeNo)}`);
    } catch {
      // Best effort. The backend cleanup worker also expires untouched auths.
    }
  };

  const startPreset = (preset) => {
    if (!enabled || creationDisabled || processingRef.current) return;
    Modal.confirm({
      title: `Toss · ${t('自动充值')}`,
      content: (
        <div className='flex flex-col gap-1'>
          <div className='font-medium'>{preset.name || t('自动充值')}</div>
          {preset.description ? <div>{preset.description}</div> : null}
          <div>
            {t('类型')}: {preset.type}
          </div>
          <div>target_scope: {preset.target_scope}</div>
          <div>
            {t('金额')}: {preset.amount}
          </div>
          {preset.type === 'threshold' ? (
            <div>threshold_quota: {preset.threshold_quota ?? 0}</div>
          ) : (
            <>
              <div>
                interval: {preset.interval_value ?? 0}{' '}
                {preset.interval_unit || '-'}
                {preset.interval_unit === 'custom'
                  ? ` (${preset.custom_seconds ?? 0}s)`
                  : ''}
              </div>
              <div>
                charge_immediately:{' '}
                {preset.charge_immediately ? t('是') : t('否')}
              </div>
            </>
          )}
        </div>
      ),
      centered: true,
      onOk: async () => {
        if (processingRef.current) return;
        const lifecycleLease = tossPaymentLifecycle.beginRequest();
        if (lifecycleLease === null) return;
        processingRef.current = true;
        setProcessing(true);
        let pendingTradeNo = '';
        try {
          const response = await API.post(`${apiBase}/${preset.type}`, {
            preset_id: preset.id,
            preset_fingerprint: preset.terms_fingerprint,
          });
          const session = response.data?.data;
          pendingTradeNo = getValidTossBillingTradeNo(session);
          if (
            !response.data?.success ||
            !walletAutoRechargeSessionMatchesPreset(session, preset)
          ) {
            await cancelPending(pendingTradeNo);
            showError(response.data?.message || t('请求失败'));
            await refresh();
            return;
          }

          const tossPayments = await loadTossPayments(session.client_key);
          const payment = tossPayments.payment({
            customerKey: session.customer_key,
          });
          if (!(await tossPaymentLifecycle.adopt(payment, lifecycleLease))) {
            await cancelPending(pendingTradeNo);
            return;
          }
          await payment.requestBillingAuth({
            method: 'CARD',
            successUrl: session.success_url,
            failUrl: session.fail_url,
            ...getTossPaymentWindowTargetOptions(
              session.success_url,
              session.fail_url,
            ),
          });
          // Normally unreachable because Toss redirects. A resolved popup flow
          // must still reload the pending row before another preset can start.
          await refresh();
        } catch (error) {
          if (isTossUserCancellation(error)) {
            showInfo(t('取消'));
          } else {
            showError(t('请求失败'));
          }
          await cancelPending(pendingTradeNo);
          await refresh();
        } finally {
          tossPaymentLifecycle.finishRequest(lifecycleLease);
          processingRef.current = false;
          setProcessing(false);
        }
      },
    });
  };

  const cancelPolicy = (policy) => {
    if (processingRef.current) return;
    Modal.confirm({
      title: t('确认'),
      content: `${t('取消')} Toss ${t('自动充值')}?`,
      centered: true,
      onOk: async () => {
        if (processingRef.current) return;
        processingRef.current = true;
        setProcessing(true);
        try {
          const response = await API.delete(`${apiBase}/${policy.id}`);
          if (!response.data?.success) {
            showError(response.data?.message || t('请求失败'));
            return;
          }
          showSuccess(t('更新成功'));
          await refresh();
        } catch {
          showError(t('请求失败'));
        } finally {
          processingRef.current = false;
          setProcessing(false);
        }
      },
    });
  };

  if (loading) {
    return <Skeleton.Paragraph active rows={2} />;
  }
  if (!enabled && livePolicies.length === 0) return null;

  return (
    <Card
      className='!rounded-xl w-full'
      title={
        <Text type='tertiary' strong>
          Toss · {t('自动充值')}
        </Text>
      }
    >
      <Space vertical style={{ width: '100%' }}>
        {livePolicies.map((policy) => (
          <div
            key={policy.id}
            className='flex items-center justify-between gap-3 rounded-lg border p-3'
          >
            <div className='min-w-0'>
              <div className='font-medium'>
                {policy.type} · {t('金额')}: {policy.amount}
              </div>
              <Tag
                size='small'
                color={policy.status === 'active' ? 'green' : 'blue'}
              >
                {policy.status === 'active' ? t('生效') : t('处理中')}
              </Tag>
            </div>
            <Button
              size='small'
              theme='light'
              type='danger'
              disabled={processing}
              onClick={() => cancelPolicy(policy)}
            >
              {t('取消')}
            </Button>
          </div>
        ))}

        {enabled && !creationDisabled && (
          <div className='grid grid-cols-1 gap-2 sm:grid-cols-2'>
            {availablePresets.map((preset) => {
              return (
                <Button
                  key={preset.id}
                  theme='outline'
                  disabled={processing || livePolicies.length > 0}
                  onClick={() => startPreset(preset)}
                >
                  {preset.name || t('自动充值')} · {preset.amount}
                </Button>
              );
            })}
          </div>
        )}
      </Space>
    </Card>
  );
}
