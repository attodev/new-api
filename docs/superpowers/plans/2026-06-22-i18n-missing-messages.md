# i18n Missing Messages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all hardcoded Chinese strings in the backend (467 entries across 62 files) with i18n keys or English strings, eliminating Chinese from server output.

**Architecture:**
- Controller direct responses (`c.JSON`, `ApiErrorMsg`) → replace with `common.ApiErrorI18n(c, i18n.MsgXxx)`
- Model/service/relay/setting layer errors (`errors.New`, `fmt.Errorf`) → change to English strings (no gin context available, internal errors)
- Log messages (`SysLog`, `logger.Log`, `log.Println`) → change to English strings
- All new i18n keys added to `i18n/keys.go` + `locales/en.yaml` + `locales/zh-CN.yaml` + `locales/zh-TW.yaml`

**Tech Stack:** Go, go-i18n/v2, gin, YAML

---

## File Map

| File | Action | Count |
|------|--------|-------|
| `i18n/keys.go` | Add new key constants | +~80 |
| `i18n/locales/en.yaml` | Add English translations | +~80 |
| `i18n/locales/zh-CN.yaml` | Add Chinese translations | +~80 |
| `i18n/locales/zh-TW.yaml` | Add Traditional Chinese | +~80 |
| `controller/*.go` (31 files) | Replace Chinese with ApiErrorI18n | 284 entries |
| `model/*.go` (11 files) | Change Chinese errors to English | 61 entries |
| `service/*.go` (10 files) | Change Chinese errors to English | 41 entries |
| `relay/channel/**/*.go` (3 files) | Change Chinese errors to English | 34 entries |
| `setting/console_setting/validation.go` | Change Chinese errors to English | 37 entries |
| `middleware/auth.go`, `middleware/model-rate-limit.go` | Change to English | 3 entries |
| `common/email.go`, `common/init.go`, `common/pprof.go`, `common/totp.go` | Change to English | 7 entries |

---

## Task 1: Add new i18n keys

**Files:**
- Modify: `i18n/keys.go`
- Modify: `i18n/locales/en.yaml`
- Modify: `i18n/locales/zh-CN.yaml`
- Modify: `i18n/locales/zh-TW.yaml`

- [ ] **Step 1: Append to `i18n/keys.go`**

Add to the end of the file:

```go
// TOTP / verification messages
const (
	MsgTotpCodeMustBe6Digits  = "totp.code_must_be_6_digits"
	MsgTotpCodeDigitsOnly     = "totp.code_digits_only"
)

// Channel extended messages
const (
	MsgChannelGetInfoFailed        = "channel.get_info_failed"
	MsgChannelSettingFormatError   = "channel.setting_format_error"
	MsgChannelModelNameTooLong     = "channel.model_name_too_long"
	MsgChannelRegionEmpty          = "channel.region_empty"
	MsgChannelRegionInvalidJson    = "channel.region_invalid_json"
	MsgChannelRegionMissingDefault = "channel.region_missing_default"
	MsgChannelVertexAIInvalidJson  = "channel.vertexai_invalid_json"
	MsgChannelVertexAIKeysEmpty    = "channel.vertexai_keys_empty"
	MsgChannelTestRunning          = "channel.test_running"
	MsgChannelResponseTimeout      = "channel.response_timeout"
	MsgChannelRefreshFailed        = "channel.refresh_failed"
	MsgChannelCopyFailed           = "channel.copy_failed"
	MsgChannelGetCountFailed       = "channel.get_count_failed"
	MsgChannelGetTypeFailed        = "channel.get_type_failed"
	MsgChannelGetTagChannelFailed  = "channel.get_tag_channel_failed"
)

// Codex messages
const (
	MsgCodexParseAuthFailed    = "codex.parse_auth_failed"
	MsgCodexExchangeFailed     = "codex.exchange_failed"
	MsgCodexParseCredFailed    = "codex.parse_cred_failed"
	MsgCodexGetUsageFailed     = "codex.get_usage_failed"
)

// Custom OAuth extended messages
const (
	MsgCustomOAuthInvalidId        = "custom_oauth.invalid_id"
	MsgCustomOAuthInvalidParams    = "custom_oauth.invalid_params"
	MsgCustomOAuthDiscoveryEmpty   = "custom_oauth.discovery_url_empty"
	MsgCustomOAuthDiscoveryInvalid = "custom_oauth.discovery_url_invalid"
	MsgCustomOAuthDiscoveryFailed  = "custom_oauth.discovery_fetch_failed"
	MsgCustomOAuthDiscoveryParseFail = "custom_oauth.discovery_parse_failed"
	MsgCustomOAuthSlugConflict     = "custom_oauth.slug_conflict"
	MsgCustomOAuthInvalidProviderId = "custom_oauth.invalid_provider_id"
	MsgCustomOAuthCheckBindingFailed = "custom_oauth.check_binding_failed"
	MsgCustomOAuthDeleteHasBindings = "custom_oauth.delete_has_bindings"
	MsgCustomOAuthNotLoggedIn      = "custom_oauth.not_logged_in"
)

// Passkey extended messages
const (
	MsgPasskeyUserInfoFailed     = "passkey.user_info_failed"
	MsgPasskeyHandleMismatch     = "passkey.handle_mismatch"
	MsgPasskeySaveStateFailed    = "passkey.save_state_failed"
	MsgPasskeyInvalidSession     = "passkey.invalid_session"
	MsgPasskeyNeedVerification   = "passkey.need_verification"
	MsgPasskeyNeedSpecificVerify = "passkey.need_specific_verification"
	MsgPasskeyInvalidState       = "passkey.invalid_state"
)

// Secure verification messages
const (
	MsgVerifyInvalidParams     = "verify.invalid_params"
	MsgVerifyGetUserFailed     = "verify.get_user_failed"
	MsgVerifyUserDisabled      = "verify.user_disabled"
	MsgVerify2FAOrPasskey      = "verify.need_2fa_or_passkey"
	MsgVerify2FANotEnabled     = "verify.2fa_not_enabled"
	MsgVerifyCodeEmpty         = "verify.code_empty"
	MsgVerifyPasskeyNotEnabled = "verify.passkey_not_enabled"
	MsgVerifyPasskeyStateError = "verify.passkey_state_error"
	MsgVerifyPasskeyNotDone    = "verify.passkey_not_done"
	MsgVerifyUnsupportedMethod = "verify.unsupported_method"
	MsgVerifyFailed            = "verify.failed"
	MsgVerifySaveStateFailed   = "verify.save_state_failed"
)

// Topup extended messages
const (
	MsgTopupAmountTooSmall      = "topup.amount_too_small"
	MsgTopupAmountTooLarge      = "topup.amount_too_large"
	MsgTopupGetGroupFailed      = "topup.get_group_failed"
	MsgTopupAmountTooLow2       = "topup.amount_too_low"
	MsgTopupUnsupportedProvider = "topup.unsupported_provider"
	MsgTopupRedirectNotTrusted  = "topup.redirect_not_trusted"
	MsgTopupCancelRedirectBad   = "topup.cancel_redirect_not_trusted"
	MsgTopupWaffoPancakeNotConfig = "topup.waffo_pancake_not_configured"
	MsgTopupWaffoNotConfig      = "topup.waffo_not_configured"
)

// Subscription payment messages
const (
	MsgSubPaymentInvalidParams  = "subscription_payment.invalid_params"
)

// Playground messages
const (
	MsgPlaygroundNoAccessToken  = "playground.no_access_token"
)

// Option / compliance messages
const (
	MsgOptionComplianceFieldRestricted = "option.compliance_field_restricted"
)

// Ratio sync messages
const (
	MsgRatioSyncInvalidParams   = "ratio_sync.invalid_params"
)

// Task video messages
const (
	MsgTaskVideoGetChannelFailed = "task_video.get_channel_failed"
	MsgTaskVideoGetModelFailed   = "task_video.get_model_failed"
	MsgTaskVideoUpstreamFailed   = "task_video.upstream_failed"
)

// 2FA extended messages
const (
	MsgTwoFAGenerateKeyFailed  = "twofa.generate_key_failed"
	MsgTwoFAGenerateCodeFailed = "twofa.generate_code_failed"
	MsgTwoFASaveCodeFailed     = "twofa.save_code_failed"
	MsgTwoFAGetCountFailed     = "twofa.get_count_failed"
)

// Model metadata messages
const (
	MsgModelSyncGetFailed       = "model_sync.get_failed"
	MsgModelSyncGetUpstreamFailed = "model_sync.get_upstream_failed"
)

// Console config messages
const (
	MsgConsoleGetConfigFailed   = "console.get_config_failed"
)

// Prefill group messages
const (
	MsgPrefillGroupNameTypeEmpty = "prefill_group.name_type_empty"
	MsgPrefillGroupNameExists    = "prefill_group.name_exists"
	MsgPrefillGroupIdMissing     = "prefill_group.id_missing"
)
```

- [ ] **Step 2: Append to `i18n/locales/en.yaml`**

```yaml
# TOTP messages
totp.code_must_be_6_digits: "Verification code must be 6 digits"
totp.code_digits_only: "Verification code can only contain digits"

# Channel extended messages
channel.get_info_failed: "Failed to get channel info: {{.Error}}"
channel.setting_format_error: "Channel setting format error: {{.Error}}"
channel.model_name_too_long: "Model name is too long: {{.Name}}"
channel.region_empty: "Deployment region cannot be empty"
channel.region_invalid_json: "Deployment region must be valid JSON, e.g. {\"default\": \"us-central1\"}"
channel.region_missing_default: "Deployment region must contain a 'default' field"
channel.vertexai_invalid_json: "Batch add Vertex AI must use JsonArray format, e.g. [{key1}, {key2}...]"
channel.vertexai_keys_empty: "Keys for batch add Vertex AI cannot be empty"
channel.test_running: "Test is already running"
channel.response_timeout: "Response time {{.Seconds}}s exceeded threshold {{.Threshold}}s"
channel.refresh_failed: "Failed to refresh credentials, please try again later"
channel.copy_failed: "Failed to copy channel, please try again later"
channel.get_count_failed: "Failed to get channel count, please try again later"
channel.get_type_failed: "Failed to get channel type statistics, please try again later"
channel.get_tag_channel_failed: "Failed to get tag channels, please try again later"

# Codex messages
codex.parse_auth_failed: "Failed to parse authorization info, please check input format"
codex.exchange_failed: "Authorization code exchange failed, please retry"
codex.parse_cred_failed: "Failed to parse credentials, please check channel config"
codex.get_usage_failed: "Failed to get usage info, please try again later"

# Custom OAuth extended messages
custom_oauth.invalid_id: "Invalid ID"
custom_oauth.invalid_params: "Invalid parameters: {{.Error}}"
custom_oauth.discovery_url_empty: "Please fill in Discovery URL or Issuer URL first"
custom_oauth.discovery_url_invalid: "Discovery URL is invalid, only http/https is supported"
custom_oauth.discovery_fetch_failed: "Failed to fetch Discovery config: {{.Error}}"
custom_oauth.discovery_parse_failed: "Failed to parse Discovery config: {{.Error}}"
custom_oauth.slug_conflict: "This Slug conflicts with a built-in OAuth provider"
custom_oauth.invalid_provider_id: "Invalid provider ID"
custom_oauth.check_binding_failed: "Error checking user bindings, please try again later"
custom_oauth.delete_has_bindings: "This OAuth provider has user bindings and cannot be deleted. Please remove all bindings first."
custom_oauth.not_logged_in: "Not logged in"

# Passkey extended messages
passkey.user_info_failed: "Failed to get user info: {{.Error}}"
passkey.handle_mismatch: "User handle does not match credential"
passkey.save_state_failed: "Failed to save verification state: {{.Error}}"
passkey.invalid_session: "Invalid session info"
passkey.need_verification: "Please complete security verification first"
passkey.need_specific_verification: "Please complete the required security verification first"
passkey.invalid_state: "Invalid Passkey verification state"

# Secure verification messages
verify.invalid_params: "Invalid parameters: {{.Error}}"
verify.get_user_failed: "Failed to get user info: {{.Error}}"
verify.user_disabled: "This user has been disabled"
verify.need_2fa_or_passkey: "User has not enabled 2FA or Passkey"
verify.2fa_not_enabled: "User has not enabled 2FA"
verify.code_empty: "Verification code cannot be empty"
verify.passkey_not_enabled: "User has not enabled Passkey"
verify.passkey_state_error: "Passkey verification state error: {{.Error}}"
verify.passkey_not_done: "Please complete Passkey verification first"
verify.unsupported_method: "Unsupported verification method: {{.Method}}"
verify.failed: "Verification failed, please check your code"
verify.save_state_failed: "Failed to save verification state: {{.Error}}"

# Topup extended messages
topup.amount_too_small: "Top-up amount cannot be less than {{.Min}}"
topup.amount_too_large: "Top-up amount cannot exceed 10000"
topup.get_group_failed: "Failed to get user group"
topup.amount_too_low: "Top-up amount is too low"
topup.unsupported_provider: "Unsupported payment provider"
topup.redirect_not_trusted: "Payment success redirect URL is not in the trusted domain list"
topup.cancel_redirect_not_trusted: "Payment cancel redirect URL is not in the trusted domain list"
topup.waffo_pancake_not_configured: "Waffo Pancake is not configured or key is invalid"
topup.waffo_not_configured: "Waffo is not configured or key is invalid"

# Subscription payment messages
subscription_payment.invalid_params: "Invalid parameters"

# Playground messages
playground.no_access_token: "Access token is not supported for playground"

# Option messages
option.compliance_field_restricted: "Compliance confirmation field cannot be modified via general settings API"

# Ratio sync messages
ratio_sync.invalid_params: "Invalid request parameters"

# Task video messages
task_video.get_channel_failed: "Failed to get channel"
task_video.get_model_failed: "Failed to get model"
task_video.upstream_failed: "Upstream request failed"

# 2FA extended messages
twofa.generate_key_failed: "Failed to generate TOTP key: {{.Error}}"
twofa.generate_code_failed: "Failed to generate backup code: {{.Error}}"
twofa.save_code_failed: "Failed to save backup code: {{.Error}}"
twofa.get_count_failed: "Failed to get backup code count: {{.Error}}"

# Model sync messages
model_sync.get_failed: "Failed to get model list, please try again later"
model_sync.get_upstream_failed: "Failed to get upstream models: {{.Error}}"

# Console messages
console.get_config_failed: "Failed to get config, please try again later"

# Prefill group messages
prefill_group.name_type_empty: "Group name and type cannot be empty"
prefill_group.name_exists: "Group name already exists"
prefill_group.id_missing: "Group ID is missing"
```

- [ ] **Step 3: Append identical structure to `zh-CN.yaml`**

```yaml
# TOTP messages
totp.code_must_be_6_digits: "验证码必须是6位数字"
totp.code_digits_only: "验证码只能包含数字"

# Channel extended messages
channel.get_info_failed: "获取渠道信息失败: {{.Error}}"
channel.setting_format_error: "渠道额外设置格式错误：{{.Error}}"
channel.model_name_too_long: "模型名称过长: {{.Name}}"
channel.region_empty: "部署地区不能为空"
channel.region_invalid_json: "部署地区必须是标准的Json格式，例如{\"default\": \"us-central1\"}"
channel.region_missing_default: "部署地区必须包含default字段"
channel.vertexai_invalid_json: "批量添加 Vertex AI 必须使用标准的JsonArray格式，例如[{key1}, {key2}...]"
channel.vertexai_keys_empty: "批量添加 Vertex AI 的 keys 不能为空"
channel.test_running: "测试已在运行中"
channel.response_timeout: "响应时间 {{.Seconds}}s 超过阈值 {{.Threshold}}s"
channel.refresh_failed: "刷新凭证失败，请稍后重试"
channel.copy_failed: "复制渠道失败，请稍后重试"
channel.get_count_failed: "获取渠道数量失败，请稍后重试"
channel.get_type_failed: "获取渠道类型统计失败，请稍后重试"
channel.get_tag_channel_failed: "获取标签渠道失败，请稍后重试"

# Codex messages
codex.parse_auth_failed: "解析授权信息失败，请检查输入格式"
codex.exchange_failed: "授权码交换失败，请重试"
codex.parse_cred_failed: "解析凭证失败，请检查渠道配置"
codex.get_usage_failed: "获取用量信息失败，请稍后重试"

# Custom OAuth extended messages
custom_oauth.invalid_id: "无效的 ID"
custom_oauth.invalid_params: "无效的请求参数: {{.Error}}"
custom_oauth.discovery_url_empty: "请先填写 Discovery URL 或 Issuer URL"
custom_oauth.discovery_url_invalid: "Discovery URL 无效，仅支持 http/https"
custom_oauth.discovery_fetch_failed: "获取 Discovery 配置失败: {{.Error}}"
custom_oauth.discovery_parse_failed: "解析 Discovery 配置失败: {{.Error}}"
custom_oauth.slug_conflict: "该 Slug 与内置 OAuth 提供商冲突"
custom_oauth.invalid_provider_id: "无效的提供商 ID"
custom_oauth.check_binding_failed: "检查用户绑定时发生错误，请稍后重试"
custom_oauth.delete_has_bindings: "该 OAuth 提供商还有用户绑定，无法删除。请先解除所有用户绑定。"
custom_oauth.not_logged_in: "未登录"

# Passkey extended messages
passkey.user_info_failed: "用户信息获取失败: {{.Error}}"
passkey.handle_mismatch: "用户句柄与凭证不匹配"
passkey.save_state_failed: "保存验证状态失败: {{.Error}}"
passkey.invalid_session: "无效的会话信息"
passkey.need_verification: "请先完成安全验证"
passkey.need_specific_verification: "请先完成对应的安全验证"
passkey.invalid_state: "无效的 Passkey 验证状态"

# Secure verification messages
verify.invalid_params: "参数错误: {{.Error}}"
verify.get_user_failed: "获取用户信息失败: {{.Error}}"
verify.user_disabled: "该用户已被禁用"
verify.need_2fa_or_passkey: "用户未启用2FA或Passkey"
verify.2fa_not_enabled: "用户未启用2FA"
verify.code_empty: "验证码不能为空"
verify.passkey_not_enabled: "用户未启用Passkey"
verify.passkey_state_error: "Passkey 验证状态异常: {{.Error}}"
verify.passkey_not_done: "请先完成 Passkey 验证"
verify.unsupported_method: "不支持的验证方式: {{.Method}}"
verify.failed: "验证失败，请检查验证码"
verify.save_state_failed: "保存验证状态失败: {{.Error}}"

# Topup extended messages
topup.amount_too_small: "充值数量不能小于 {{.Min}}"
topup.amount_too_large: "充值数量不能大于 10000"
topup.get_group_failed: "获取用户分组失败"
topup.amount_too_low: "充值金额过低"
topup.unsupported_provider: "不支持的支付渠道"
topup.redirect_not_trusted: "支付成功重定向URL不在可信任域名列表中"
topup.cancel_redirect_not_trusted: "支付取消重定向URL不在可信任域名列表中"
topup.waffo_pancake_not_configured: "Waffo Pancake 未配置或密钥无效"
topup.waffo_not_configured: "Waffo 未配置或密钥无效"

# Subscription payment messages
subscription_payment.invalid_params: "参数错误"

# Playground messages
playground.no_access_token: "暂不支持使用 access token"

# Option messages
option.compliance_field_restricted: "合规确认字段不允许通过通用设置接口修改"

# Ratio sync messages
ratio_sync.invalid_params: "请求参数格式错误"

# Task video messages
task_video.get_channel_failed: "获取渠道失败"
task_video.get_model_failed: "获取模型失败"
task_video.upstream_failed: "上游请求失败"

# 2FA extended messages
twofa.generate_key_failed: "生成TOTP密钥失败: {{.Error}}"
twofa.generate_code_failed: "生成备用码失败: {{.Error}}"
twofa.save_code_failed: "保存备用码失败: {{.Error}}"
twofa.get_count_failed: "获取备用码数量失败: {{.Error}}"

# Model sync messages
model_sync.get_failed: "获取模型列表失败，请稍后重试"
model_sync.get_upstream_failed: "获取上游模型失败: {{.Error}}"

# Console messages
console.get_config_failed: "获取配置失败，请稍后重试"

# Prefill group messages
prefill_group.name_type_empty: "组名称和类型不能为空"
prefill_group.name_exists: "组名称已存在"
prefill_group.id_missing: "缺少组 ID"
```

- [ ] **Step 4: Append same keys to `zh-TW.yaml`** (same content as zh-CN for now — can be refined later)

Same content as zh-CN block above.

- [ ] **Step 5: Build and verify no compile error**
```bash
cd /home/molla/new-api && go build ./i18n/...
```
Expected: no errors

- [ ] **Step 6: Commit**
```bash
git add i18n/keys.go i18n/locales/en.yaml i18n/locales/zh-CN.yaml i18n/locales/zh-TW.yaml
git commit -m "feat(i18n): add missing message keys for all unheld Chinese strings"
```

---

## Task 2: Fix `common/` files (English strings)

**Files:**
- Modify: `common/email.go`
- Modify: `common/init.go`
- Modify: `common/pprof.go`
- Modify: `common/totp.go`

- [ ] **Step 1: `common/email.go` L45**
```go
// Before:
return fmt.Errorf("SMTP 服务器未配置")
// After:
return fmt.Errorf("SMTP server is not configured")
```

- [ ] **Step 2: `common/init.go` L53**
```go
// Before:
log.Println("警告：SESSION_SECRET被设置为默认值'random_string'，请修改为随机字符串。")
// After:
log.Println("Warning: SESSION_SECRET is set to default value 'random_string', please change it to a random string.")
```

- [ ] **Step 3: `common/pprof.go` L25, L31, L36**
```go
// L25 Before: SysLog("创建pprof文件夹失败 " + err.Error())
SysLog("failed to create pprof directory: " + err.Error())
// L31 Before: SysLog("创建pprof文件失败 " + err.Error())
SysLog("failed to create pprof file: " + err.Error())
// L36 Before: SysLog("启动pprof失败 " + err.Error())
SysLog("failed to start pprof: " + err.Error())
```

- [ ] **Step 4: `common/totp.go` — use ApiErrorI18n pattern (these return errors, not direct responses)**
```go
// L133 Before: return "", fmt.Errorf("验证码必须是6位数字")
return "", fmt.Errorf("verification code must be 6 digits")
// L138 Before: return "", fmt.Errorf("验证码只能包含数字")
return "", fmt.Errorf("verification code can only contain digits")
```

- [ ] **Step 5: Build**
```bash
cd /home/molla/new-api && go build ./common/...
```

- [ ] **Step 6: Commit**
```bash
git add common/email.go common/init.go common/pprof.go common/totp.go
git commit -m "fix(i18n): translate common/ Chinese strings to English"
```

---

## Task 3: Fix `controller/` — channel files

**Files:**
- Modify: `controller/channel.go`
- Modify: `controller/channel-billing.go`
- Modify: `controller/channel-test.go`
- Modify: `controller/channel_upstream_update.go`

- [ ] **Step 1: `controller/channel.go`**

```go
// L117: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取标签失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelGetTagsFailed)

// L123: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取标签数量失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelGetTagsFailed)

// L136: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取标签渠道失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelGetTagChannelFailed)

// L144: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道数量失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelGetCountFailed)

// L155: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道列表失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelGetListFailed)

// L171: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道类型统计失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelGetTypeFailed)

// L410: common.ApiError(c, fmt.Errorf("渠道ID格式错误: %v", err))
common.ApiErrorI18n(c, i18n.MsgChannelIdFormatError)

// L417: common.ApiError(c, fmt.Errorf("获取渠道信息失败: %v", err))
common.ApiErrorI18n(c, i18n.MsgChannelGetInfoFailed, map[string]any{"Error": err.Error()})

// L422: common.ApiError(c, fmt.Errorf("渠道不存在"))
common.ApiErrorI18n(c, i18n.MsgChannelNotExists)

// L529: c.JSON(http.StatusOK, gin.H{"success": false, "message": "刷新凭证失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelRefreshFailed)

// L1204: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道信息失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelGetInfoFailed, map[string]any{"Error": err.Error()})

// L1223: c.JSON(http.StatusOK, gin.H{"success": false, "message": "复制渠道失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgChannelCopyFailed)

// L460: return fmt.Errorf("渠道额外设置[channel setting] 格式错误：%s", err.Error())
return fmt.Errorf("channel setting format error: %s", err.Error())

// L472: return fmt.Errorf("模型名称过长: %s", m)
return fmt.Errorf("model name too long: %s", m)

// L480: return fmt.Errorf("部署地区不能为空")
return fmt.Errorf("deployment region cannot be empty")

// L485: return fmt.Errorf("部署地区必须是标准的Json格式...")
return fmt.Errorf(`deployment region must be valid JSON, e.g. {"default": "us-central1", "region2": "us-east1"}`)

// L489: return fmt.Errorf("部署地区必须包含default字段")
return fmt.Errorf("deployment region must contain 'default' field")

// L562: return nil, fmt.Errorf("批量添加 Vertex AI 必须使用标准的JsonArray格式...")
return nil, fmt.Errorf("batch add Vertex AI must use JsonArray format, e.g. [{key1}, {key2}...]: %w", err)

// L573: return nil, fmt.Errorf("Vertex AI key JSON 编码失败: %w", err)
return nil, fmt.Errorf("Vertex AI key JSON encoding failed: %w", err)

// L582: return nil, fmt.Errorf("批量添加 Vertex AI 的 keys 不能为空")
return nil, fmt.Errorf("keys for batch add Vertex AI cannot be empty")
```

- [ ] **Step 2: `controller/channel-billing.go`**
```go
// L370, L390: return 0, errors.New("尚未实现")
return 0, errors.New("not implemented")
```

- [ ] **Step 3: `controller/channel-test.go`**
```go
// L905: return errors.New("测试已在运行中")
return errors.New("test is already running")

// L945: err := fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", ...)
// Keep as English — this is an internal error that gets passed up
err := fmt.Errorf("response time %.2fs exceeded threshold %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
```

- [ ] **Step 4: `controller/channel_upstream_update.go`**
```go
// L282, L320: return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
return nil, fmt.Errorf("failed to get channel key: %w", apiErr)
```

- [ ] **Step 5: Build**
```bash
cd /home/molla/new-api && go build ./controller/...
```

- [ ] **Step 6: Commit**
```bash
git add controller/channel.go controller/channel-billing.go controller/channel-test.go controller/channel_upstream_update.go
git commit -m "fix(i18n): translate channel controller Chinese strings"
```

---

## Task 4: Fix `controller/` — auth/user/oauth files

**Files:**
- Modify: `controller/checkin.go`
- Modify: `controller/codex_oauth.go`
- Modify: `controller/codex_usage.go`
- Modify: `controller/custom_oauth.go`
- Modify: `controller/discord.go`
- Modify: `controller/github.go`
- Modify: `controller/oidc.go`
- Modify: `controller/user.go`
- Modify: `controller/twofa.go`
- Modify: `controller/passkey.go`
- Modify: `controller/secure_verification.go`

- [ ] **Step 1: `controller/checkin.go`** — LOG only (RecordLog message), change to English
```go
// L64: model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", ...))
model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("Check-in successful, quota awarded: %s", logger.LogQuota(checkin.QuotaAwarded)))
```

- [ ] **Step 2: `controller/codex_oauth.go`**
```go
// L136: c.JSON(http.StatusOK, gin.H{"success": false, "message": "解析授权信息失败，请检查输入格式"})
common.ApiErrorI18n(c, i18n.MsgCodexParseAuthFailed)

// L184: c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权码交换失败，请重试"})
common.ApiErrorI18n(c, i18n.MsgCodexExchangeFailed)
```

- [ ] **Step 3: `controller/codex_usage.go`**
```go
// L48: c.JSON(http.StatusOK, gin.H{"success": false, "message": "解析凭证失败，请检查渠道配置"})
common.ApiErrorI18n(c, i18n.MsgCodexParseCredFailed)

// L74, L104: c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取用量信息失败，请稍后重试"})
common.ApiErrorI18n(c, i18n.MsgCodexGetUsageFailed)
```

- [ ] **Step 4: `controller/custom_oauth.go`** — 24 entries, replace all ApiErrorMsg with ApiErrorI18n
```go
// "无效的 ID" (L97, L296, L407) → i18n.MsgCustomOAuthInvalidId
// "未找到该 OAuth 提供商" (L103, L309, L414) → i18n.MsgCustomOAuthNotFound
// "无效的请求参数: " (L145, L217, L302) → i18n.MsgCustomOAuthInvalidParams + map[string]any{"Error": err.Error()}
// "请先填写 Discovery URL 或 Issuer URL" (L153) → i18n.MsgCustomOAuthDiscoveryEmpty
// "Discovery URL 无效，仅支持 http/https" (L165) → i18n.MsgCustomOAuthDiscoveryInvalid
// "创建 Discovery 请求失败: " (L174) → i18n.MsgCustomOAuthDiscoveryFailed + map[string]any{"Error": err.Error()}
// "获取 Discovery 配置失败: " (L182, L193) → i18n.MsgCustomOAuthDiscoveryFailed + map[string]any{"Error": err.Error()/message}
// "解析 Discovery 配置失败: " (L199) → i18n.MsgCustomOAuthDiscoveryParseFail + map[string]any{"Error": err.Error()}
// "该 Slug 已被使用" (L223, L318) → i18n.MsgCustomOAuthSlugExists
// "该 Slug 与内置 OAuth 提供商冲突" (L229, L323) → i18n.MsgCustomOAuthSlugConflict
// "检查用户绑定时发生错误" (L422) → i18n.MsgCustomOAuthCheckBindingFailed
// "该 OAuth 提供商还有用户绑定，无法删除" (L426) → i18n.MsgCustomOAuthDeleteHasBindings
// "未登录" (L472, L526) → i18n.MsgCustomOAuthNotLoggedIn
// "无效的提供商 ID" (L533) → i18n.MsgCustomOAuthInvalidProviderId
```

- [ ] **Step 5: `controller/discord.go` and `controller/github.go` and `controller/oidc.go`** — these return errors (indirect), change to English
```go
// discord.go L38: errors.New("无效的参数") → errors.New("invalid parameters")
// discord.go L60: errors.New("无法连接至 Discord 服务器...") → errors.New("unable to connect to Discord server, please try again later")
// discord.go L71: (already SysError + return) → errors.New("failed to get Discord token, please check settings")
// discord.go L82: errors.New("无法连接至 Discord 服务器...") → errors.New("unable to connect to Discord server, please try again later")
// discord.go L87: errors.New("Discord 获取用户信息失败！...") → errors.New("failed to get Discord user info, please check settings")
// discord.go L97: errors.New("Discord 获取用户信息为空！...") → errors.New("Discord returned empty user info, please check settings")
// github.go L33: errors.New("无效的参数") → errors.New("invalid parameters")
// github.go L52, L68: errors.New("无法连接至 GitHub 服务器...") → errors.New("unable to connect to GitHub server, please try again later")
// github.go L77: errors.New("返回值非法，用户字段为空...") → errors.New("invalid response, user field is empty, please try again later")
// oidc.go: same pattern as discord.go — change to English
```

- [ ] **Step 6: `controller/twofa.go`** — all are SysLog (indirect), change to English
```go
// L73: common.SysLog("生成TOTP密钥失败: " + err.Error()) → common.SysLog("failed to generate TOTP key: " + err.Error())
// L84: → "failed to generate backup code: " + err.Error()
// L118, L381: → "failed to save backup code: " + err.Error()
// L297: → "failed to get backup code count: " + err.Error()
// L371: → "failed to generate backup code: " + err.Error()
```

- [ ] **Step 7: `controller/passkey.go`**
```go
// L137: common.ApiErrorMsg(c, "无法创建 Passkey 凭证") → common.ApiErrorI18n(c, i18n.MsgPasskeyCreateFailed)
// L275: return nil, fmt.Errorf("未找到 Passkey 凭证: %w", err) → English
// L281: return nil, fmt.Errorf("用户信息获取失败: %w", err) → English
// L285: return nil, errors.New("该用户已被禁用") → English
// L294: return nil, errors.New("用户句柄与凭证不匹配") → English
// L309, L315: common.ApiErrorMsg → common.ApiErrorI18n(c, i18n.MsgPasskeyLoginAbnormal)
// L320: → i18n.MsgUserDisabled
// L327: → i18n.MsgPasskeyUpdateFailed
// L344: → i18n.MsgPasskeyInvalidUserId
// L496: common.ApiError(c, fmt.Errorf("保存验证状态失败: %v", err)) → ApiErrorI18n(c, i18n.MsgPasskeySaveStateFailed, map[string]any{"Error": err.Error()})
// L510: return nil, errors.New("未登录") → English
// L514: return nil, errors.New("无效的会话信息") → English
// L521: return nil, errors.New("该用户已被禁用") → English
// L571: common.ApiErrorMsg(c, "请先完成安全验证") → ApiErrorI18n(c, i18n.MsgPasskeyNeedVerification)
// L576: → ApiErrorI18n(c, i18n.MsgPasskeyNeedSpecificVerify)
```

- [ ] **Step 8: `controller/secure_verification.go`**
```go
// All ApiError(c, fmt.Errorf("中文")) → ApiErrorI18n(c, i18n.MsgVerifyXxx)
// L52 → MsgVerifyInvalidParams
// L59 → MsgVerifyGetUserFailed
// L64 → MsgVerifyUserDisabled
// L76 → MsgVerify2FAOrPasskey
// L88 → MsgVerify2FANotEnabled
// L92 → MsgVerifyCodeEmpty
// L100 → MsgVerifyPasskeyNotEnabled
// L106 → MsgVerifyPasskeyStateError
// L110 → MsgVerifyPasskeyNotDone
// L116 → MsgVerifyUnsupportedMethod
// L121 → MsgVerifyFailed
// L128 → MsgVerifySaveStateFailed
// L133: model.RecordLog LOG → English
// L168: return false, fmt.Errorf("无效的 Passkey 验证状态") → English
```

- [ ] **Step 9: `controller/user.go`** — LOG only
```go
// L977, L988: fmt.Sprintf("管理员增加/减少用户额度 %s") → English
```

- [ ] **Step 10: Build**
```bash
cd /home/molla/new-api && go build ./controller/...
```

- [ ] **Step 11: Commit**
```bash
git add controller/checkin.go controller/codex_oauth.go controller/codex_usage.go \
  controller/custom_oauth.go controller/discord.go controller/github.go controller/oidc.go \
  controller/user.go controller/twofa.go controller/passkey.go controller/secure_verification.go
git commit -m "fix(i18n): translate auth/user/oauth controller Chinese strings"
```

---

## Task 5: Fix `controller/` — payment/subscription files

**Files:**
- Modify: `controller/topup.go`
- Modify: `controller/topup_stripe.go`
- Modify: `controller/topup_paypal.go`
- Modify: `controller/topup_creem.go`
- Modify: `controller/topup_waffo.go`
- Modify: `controller/topup_waffo_pancake.go`
- Modify: `controller/subscription.go`
- Modify: `controller/subscription_payment_stripe.go`
- Modify: `controller/subscription_payment_creem.go`
- Modify: `controller/subscription_payment_epay.go`
- Modify: `controller/subscription_payment_waffo_pancake.go`

- [ ] **Step 1: `controller/topup.go`** — replace c.JSON with ApiErrorI18n
```go
// "充值数量不能小于 %d" → ApiErrorI18n(c, i18n.MsgTopupAmountTooSmall, map[string]any{"Min": getStripeMinTopup()})
// "充值数量不能大于 10000" → ApiErrorI18n(c, i18n.MsgTopupAmountTooLarge)
// "获取用户分组失败" → ApiErrorI18n(c, i18n.MsgTopupGetGroupFailed)
// "充值金额过低" → ApiErrorI18n(c, i18n.MsgTopupAmountTooLow)
// "不支持的支付渠道" → ApiErrorI18n(c, i18n.MsgTopupUnsupportedProvider)
// "支付成功重定向URL不在可信任域名列表中" → ApiErrorI18n(c, i18n.MsgTopupRedirectNotTrusted)
// "支付取消重定向URL不在可信任域名列表中" → ApiErrorI18n(c, i18n.MsgTopupCancelRedirectBad)
```

- [ ] **Step 2: Payment log messages in `controller/topup_stripe.go`, `topup_paypal.go`, `topup_creem.go`, `topup_waffo.go`, `topup_waffo_pancake.go`**

These are all `logger.LogInfo/LogError/LogWarn` calls (LOG tag, not REST). Change all Chinese format strings to English:
```go
// Pattern: logger.LogError(ctx, fmt.Sprintf("Stripe 创建 Checkout Session 失败 ...", ...))
// → logger.LogError(ctx, fmt.Sprintf("Stripe create checkout session failed user_id=%d trade_no=%s amount=%d error=%q", ...))
// Apply same English translation to all Stripe/PayPal/Creem/Waffo log messages
```

- [ ] **Step 3: `controller/subscription.go`** — all `common.ApiErrorMsg(c, "参数错误")` variants
```go
// All "参数错误" → ApiErrorI18n(c, i18n.MsgInvalidParams)
// All subscription-specific errors already covered by existing i18n keys
```

- [ ] **Step 4: `controller/subscription_payment_*.go`**
```go
// All "参数错误" → ApiErrorI18n(c, i18n.MsgSubPaymentInvalidParams)
// "该套餐未配置 WaffoPancakeProductId" → ApiErrorI18n(c, i18n.MsgTopupWaffoPancakeNotConfig) [or add new key]
// "Waffo Pancake 未配置或密钥无效" → ApiErrorI18n(c, i18n.MsgTopupWaffoPancakeNotConfig)
```

- [ ] **Step 5: Build**
```bash
cd /home/molla/new-api && go build ./controller/...
```

- [ ] **Step 6: Commit**
```bash
git add controller/topup.go controller/topup_stripe.go controller/topup_paypal.go \
  controller/topup_creem.go controller/topup_waffo.go controller/topup_waffo_pancake.go \
  controller/subscription.go controller/subscription_payment_stripe.go \
  controller/subscription_payment_creem.go controller/subscription_payment_epay.go \
  controller/subscription_payment_waffo_pancake.go
git commit -m "fix(i18n): translate payment/subscription controller Chinese strings"
```

---

## Task 6: Fix `controller/` — misc files

**Files:**
- Modify: `controller/console_migrate.go`
- Modify: `controller/midjourney.go`
- Modify: `controller/model_meta.go`
- Modify: `controller/model_sync.go`
- Modify: `controller/option.go`
- Modify: `controller/payment_compliance.go`
- Modify: `controller/playground.go`
- Modify: `controller/prefill_group.go`
- Modify: `controller/ratio_sync.go`
- Modify: `controller/relay.go`
- Modify: `controller/task_video.go`

- [ ] **Step 1: Each file — apply ApiErrorI18n for direct responses, English for logs**
```go
// console_migrate.go L21: "获取配置失败，请稍后重试" → ApiErrorI18n(c, i18n.MsgConsoleGetConfigFailed)
// midjourney.go: all logger.LogInfo/LogError Chinese → English log strings
// model_meta.go: already uses ApiErrorMsg → change to ApiErrorI18n
//   "模型名称不能为空" → i18n.MsgModelNameEmpty
//   "模型名称已存在" → i18n.MsgModelNameExists
//   "缺少模型 ID" → i18n.MsgModelIdMissing
// model_sync.go: "获取模型列表失败" → i18n.MsgModelGetListFailed; "获取上游模型失败" → ApiErrorI18n + Error arg
// option.go L148 → ApiErrorI18n(c, i18n.MsgOptionComplianceFieldRestricted)
// payment_compliance.go: "参数错误" → MsgInvalidParams; "请确认合规声明" → already covered by existing key
// playground.go → ApiErrorI18n(c, i18n.MsgPlaygroundNoAccessToken)
// prefill_group.go: "组名称和类型不能为空" → i18n.MsgPrefillGroupNameTypeEmpty, etc.
// ratio_sync.go → ApiErrorI18n(c, i18n.MsgRatioSyncInvalidParams)
// relay.go: LOG only → English
// task_video.go: direct responses → ApiErrorI18n with new task_video keys
```

- [ ] **Step 2: Build**
```bash
cd /home/molla/new-api && go build ./controller/...
```

- [ ] **Step 3: Commit**
```bash
git add controller/console_migrate.go controller/midjourney.go controller/model_meta.go \
  controller/model_sync.go controller/option.go controller/payment_compliance.go \
  controller/playground.go controller/prefill_group.go controller/ratio_sync.go \
  controller/relay.go controller/task_video.go
git commit -m "fix(i18n): translate misc controller Chinese strings"
```

---

## Task 7: Fix `model/` files

**Files:** `model/channel_cache.go`, `model/log.go`, `model/main.go`, `model/passkey.go`, `model/redemption.go`, `model/subscription.go`, `model/token.go`, `model/topup.go`, `model/twofa.go`, `model/usedata.go`, `model/user.go`

Strategy: All model-layer Chinese strings are in `errors.New`/`fmt.Errorf` — change to English. Log strings (SysLog) also to English.

- [ ] **Step 1: `model/token.go`** — 13 entries
```go
// "搜索模式中不允许包含连续的 % 通配符" → "consecutive %% wildcards are not allowed in search pattern"
// "搜索模式中最多允许包含 2 个 % 通配符" → "search pattern may contain at most 2 %% wildcards"
// "使用模糊搜索时，关键词长度至少为 2 个字符" → "keyword must be at least 2 characters when using wildcard search"
// "获取令牌数量失败" → "failed to get token count"
// (remaining are already covered or LOG-only)
```

- [ ] **Step 2: `model/user.go`** — 11 entries
```go
// "转移额度最小为%s！" → already covered by i18n.MsgUserTransferQuotaMinimum (use T() with context... but no context here)
// → fmt.Errorf("minimum transfer quota is %s", ...) — English fallback
// RecordLog strings: "新用户注册赠送 %s" → "New user registration bonus: %s"
// "使用邀请码赠送 %s" → "Invitation code bonus: %s"
// "邀请用户赠送 %s" → "Inviter bonus: %s"
// SysLog strings: change to English
```

- [ ] **Step 3: `model/topup.go`** — 10 entries, all errors/status → English
- [ ] **Step 4: `model/twofa.go`** — 6 SysLog entries → English
- [ ] **Step 5: `model/usedata.go`** — 2 SysLog entries → English
- [ ] **Step 6: Remaining model files** — same pattern

- [ ] **Step 7: Build**
```bash
cd /home/molla/new-api && go build ./model/...
```

- [ ] **Step 8: Commit**
```bash
git add model/
git commit -m "fix(i18n): translate model layer Chinese strings to English"
```

---

## Task 8: Fix `service/` files

**Files:** `service/billing.go`, `service/billing_session.go`, `service/channel.go`, `service/channel_affinity.go`, `service/passkey/service.go`, `service/passkey/session.go`, `service/pre_consume_quota.go`, `service/quota.go`, `service/task_billing.go`, `service/task_polling.go`

Strategy: All are SysLog/logger.Log (internal) + indirect errors → change to English.

- [ ] **Step 1: `service/channel.go`** — 2 SysLog entries
```go
// "通道「%s」（#%d）发生错误，准备禁用，原因：%s" → "channel \"%s\" (#%d) error, disabling, reason: %s"
// "通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作" → "channel \"%s\" (#%d) auto-disable not enabled, skipping"
```

- [ ] **Step 2: `service/task_polling.go`** — 7 entries
```go
// "任务进度轮询开始" → "task progress polling started"
// "任务进度轮询完成" → "task progress polling completed"
// "渠道 #%d 未完成的任务有: %d..." → "channel #%d pending tasks: %d, fetched: %s"
```

- [ ] **Step 3: Remaining service files** — billing log strings → English

- [ ] **Step 4: Build**
```bash
cd /home/molla/new-api && go build ./service/...
```

- [ ] **Step 5: Commit**
```bash
git add service/
git commit -m "fix(i18n): translate service layer Chinese strings to English"
```

---

## Task 9: Fix `relay/`, `setting/`, `middleware/` files

**Files:**
- Modify: `relay/channel/gemini/relay-gemini.go`
- Modify: `relay/channel/ollama/relay-ollama.go`
- Modify: `relay/channel/task/jimeng/adaptor.go`
- Modify: `setting/console_setting/validation.go`
- Modify: `middleware/auth.go`
- Modify: `middleware/model-rate-limit.go`

- [ ] **Step 1: `relay/channel/ollama/relay-ollama.go`** — 27 entries, all `fmt.Errorf` → English
```go
// "创建请求失败: %v" → "failed to create request: %v"
// "请求失败: %v" → "request failed: %v"
// "服务器返回错误 %d: %s" → "server returned error %d: %s"
// "读取响应失败: %v" → "failed to read response: %v"
// "解析响应失败: %v" → "failed to parse response: %v"
// etc.
```

- [ ] **Step 2: `relay/channel/gemini/relay-gemini.go`** — 6 entries, same pattern → English

- [ ] **Step 3: `setting/console_setting/validation.go`** — 37 entries, all `fmt.Errorf` → English
```go
// "%s格式错误：%s" → "%s format error: %s"
// "第%d个%s的URL格式不正确" → "item %d of %s has invalid URL format"
// "API信息数量不能超过50个" → "API info list cannot exceed 50 items"
// etc.
```

- [ ] **Step 4: `middleware/auth.go`** — 1 entry
```go
// "普通用户不支持指定渠道" → "regular users cannot specify channels"
```

- [ ] **Step 5: `middleware/model-rate-limit.go`** — 2 LOG entries → English
```go
// "检查成功请求数限制失败:" → "failed to check success request rate limit:"
// "检查总请求数限制失败:" → "failed to check total request rate limit:"
```

- [ ] **Step 6: Build**
```bash
cd /home/molla/new-api && go build ./relay/... ./setting/... ./middleware/...
```

- [ ] **Step 7: Commit**
```bash
git add relay/channel/gemini/relay-gemini.go relay/channel/ollama/relay-ollama.go \
  relay/channel/task/jimeng/adaptor.go setting/console_setting/validation.go \
  middleware/auth.go middleware/model-rate-limit.go
git commit -m "fix(i18n): translate relay/setting/middleware Chinese strings to English"
```

---

## Task 10: Final build verification

- [ ] **Step 1: Full build**
```bash
cd /home/molla/new-api && go build ./...
```
Expected: no errors

- [ ] **Step 2: Verify no Chinese remains in output paths**
```bash
grep -rn --include="*.go" -P "[\x{4e00}-\x{9fff}]" /home/molla/new-api \
  --exclude-dir=vendor --exclude="*_test.go" \
  | grep -vE '^\s*//' \
  | grep -E '(ApiError|ApiErrorMsg|ApiErrorI18n|c\.JSON|errors\.New|fmt\.Errorf|SysLog|SysError|logger\.Log|log\.Print)' \
  | wc -l
```
Expected: 0

- [ ] **Step 3: Final commit**
```bash
git add -A
git commit -m "fix(i18n): complete Chinese string elimination from backend output"
```
