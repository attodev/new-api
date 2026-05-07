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

import React, { useEffect, useState, useRef } from 'react';
import { Banner, Button, Form, Row, Col, Spin } from '@douyinfe/semi-ui';
import {
  API,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import { BookOpen, TriangleAlert } from 'lucide-react';

export default function SettingsPaymentGatewayPayPal(props) {
  const { t } = useTranslation();
  const sectionTitle = props.hideSectionTitle ? undefined : t('PayPal 设置');
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    PayPalClientId: '',
    PayPalClientSecret: '',
    PayPalWebhookID: '',
    PayPalUnitPrice: 1.0,
    PayPalMinTopUp: 1,
    PayPalSandbox: false,
  });
  const [originInputs, setOriginInputs] = useState({});
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        PayPalClientId: props.options.PayPalClientId || '',
        PayPalClientSecret: props.options.PayPalClientSecret || '',
        PayPalWebhookID: props.options.PayPalWebhookID || '',
        PayPalUnitPrice:
          props.options.PayPalUnitPrice !== undefined
            ? parseFloat(props.options.PayPalUnitPrice)
            : 1.0,
        PayPalMinTopUp:
          props.options.PayPalMinTopUp !== undefined
            ? parseFloat(props.options.PayPalMinTopUp)
            : 1,
        PayPalSandbox:
          props.options.PayPalSandbox !== undefined
            ? props.options.PayPalSandbox
            : false,
      };
      setInputs(currentInputs);
      setOriginInputs({ ...currentInputs });
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitPayPalSetting = async () => {
    setLoading(true);
    try {
      const options = [];

      if (inputs.PayPalClientId && inputs.PayPalClientId !== '') {
        options.push({ key: 'PayPalClientId', value: inputs.PayPalClientId });
      }
      if (inputs.PayPalClientSecret && inputs.PayPalClientSecret !== '') {
        options.push({
          key: 'PayPalClientSecret',
          value: inputs.PayPalClientSecret,
        });
      }
      if (inputs.PayPalWebhookID !== undefined) {
        options.push({
          key: 'PayPalWebhookID',
          value: inputs.PayPalWebhookID,
        });
      }
      if (
        inputs.PayPalUnitPrice !== undefined &&
        inputs.PayPalUnitPrice !== null
      ) {
        options.push({
          key: 'PayPalUnitPrice',
          value: inputs.PayPalUnitPrice.toString(),
        });
      }
      if (
        inputs.PayPalMinTopUp !== undefined &&
        inputs.PayPalMinTopUp !== null
      ) {
        options.push({
          key: 'PayPalMinTopUp',
          value: inputs.PayPalMinTopUp.toString(),
        });
      }
      if (
        originInputs['PayPalSandbox'] !== inputs.PayPalSandbox &&
        inputs.PayPalSandbox !== undefined
      ) {
        options.push({
          key: 'PayPalSandbox',
          value: inputs.PayPalSandbox ? 'true' : 'false',
        });
      }

      const requestQueue = options.map((opt) =>
        API.put('/api/option/', {
          key: opt.key,
          value: opt.value,
        }),
      );

      const results = await Promise.all(requestQueue);

      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => {
          showError(res.data.message);
        });
      } else {
        showSuccess(t('更新成功'));
        setOriginInputs({ ...inputs });
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    }
    setLoading(false);
  };

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={sectionTitle}>
          <Banner
            type='info'
            icon={<BookOpen size={16} />}
            description={
              <>
                {t('在')}{' '}
                <a
                  href='https://developer.paypal.com/dashboard/'
                  target='_blank'
                  rel='noreferrer'
                >
                  PayPal Developer Dashboard
                </a>{' '}
                {t('中创建应用并获取 Client ID 和 Secret。建议先在沙盒环境中完成联调。')}
              </>
            }
            style={{ marginBottom: 12 }}
          />
          <Banner
            type='warning'
            icon={<TriangleAlert size={16} />}
            description={
              <>
                {t('Webhook ID는 PayPal Developer Dashboard → 앱 선택 → Webhooks에서 발급받으세요. 이벤트는 ')}<b>CHECKOUT.ORDER.APPROVED</b>{t(' 와 ')}<b>PAYMENT.CAPTURE.COMPLETED</b>{t(' 를 구독해야 합니다.')}
                <br />
                {t('Webhook URL')}: {props.options?.ServerAddress || t('서버 주소')}/api/paypal/webhook
              </>
            }
            style={{ marginBottom: 16 }}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='PayPalClientId'
                label={t('Client ID')}
                placeholder={t('PayPal 应用 Client ID，留空表示保持当前不变')}
                type='password'
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='PayPalClientSecret'
                label={t('Client Secret')}
                placeholder={t('PayPal 应用 Client Secret，留空表示保持当前不变')}
                extraText={t('保存后不会回显')}
                type='password'
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={24} lg={24} xl={24}>
              <Form.Input
                field='PayPalWebhookID'
                label={t('Webhook ID')}
                placeholder={t('PayPal Developer Dashboard에서 발급받은 Webhook ID')}
                extraText={t('Webhook 미설정 시 브라우저 리다이렉트에만 의존합니다 (불안정)')}
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='PayPalUnitPrice'
                precision={2}
                label={t('充值价格（x元/美金）')}
                placeholder={t('例如：7，就是7元/美金')}
                extraText={t('按 1 美元对应的站内价格填写')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='PayPalMinTopUp'
                label={t('最低充值美元数量')}
                placeholder={t('例如：1，就是最低充值1$')}
                extraText={t('用户单次最少可充值的美元数量')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='PayPalSandbox'
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                label={t('启用沙盒模式（测试用）')}
              />
            </Col>
          </Row>
          <Button onClick={submitPayPalSetting}>{t('更新 PayPal 设置')}</Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
