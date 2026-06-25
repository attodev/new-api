package i18n

// Message keys for i18n translations
// Use these constants instead of hardcoded strings

// Common error messages
const (
	MsgInvalidParams     = "common.invalid_params"
	MsgDatabaseError     = "common.database_error"
	MsgRetryLater        = "common.retry_later"
	MsgGenerateFailed    = "common.generate_failed"
	MsgNotFound          = "common.not_found"
	MsgUnauthorized      = "common.unauthorized"
	MsgForbidden         = "common.forbidden"
	MsgInvalidId         = "common.invalid_id"
	MsgIdEmpty           = "common.id_empty"
	MsgFeatureDisabled   = "common.feature_disabled"
	MsgOperationSuccess  = "common.operation_success"
	MsgOperationFailed   = "common.operation_failed"
	MsgUpdateSuccess     = "common.update_success"
	MsgUpdateFailed      = "common.update_failed"
	MsgCreateSuccess     = "common.create_success"
	MsgCreateFailed      = "common.create_failed"
	MsgDeleteSuccess     = "common.delete_success"
	MsgDeleteFailed      = "common.delete_failed"
	MsgAlreadyExists     = "common.already_exists"
	MsgNameCannotBeEmpty = "common.name_cannot_be_empty"
	MsgBatchTooMany      = "common.batch_too_many"
)

// Auth middleware messages
const (
	MsgAuthNotLoggedIn           = "auth.not_logged_in"
	MsgAuthAccessTokenInvalid    = "auth.access_token_invalid"
	MsgAuthUserInfoInvalid       = "auth.user_info_invalid"
	MsgAuthUserIdNotProvided     = "auth.user_id_not_provided"
	MsgAuthUserIdFormatError     = "auth.user_id_format_error"
	MsgAuthUserIdMismatch        = "auth.user_id_mismatch"
	MsgAuthUserBanned            = "auth.user_banned"
	MsgAuthInsufficientPrivilege = "auth.insufficient_privilege"
)

// Token related messages
const (
	MsgTokenNameTooLong          = "token.name_too_long"
	MsgTokenQuotaNegative        = "token.quota_negative"
	MsgTokenQuotaExceedMax       = "token.quota_exceed_max"
	MsgTokenGenerateFailed       = "token.generate_failed"
	MsgTokenGetInfoFailed        = "token.get_info_failed"
	MsgTokenExpiredCannotEnable  = "token.expired_cannot_enable"
	MsgTokenExhaustedCannotEable = "token.exhausted_cannot_enable"
	MsgTokenInvalid              = "token.invalid"
	MsgTokenNotProvided          = "token.not_provided"
	MsgTokenExpired              = "token.expired"
	MsgTokenExhausted            = "token.exhausted"
	MsgTokenStatusUnavailable    = "token.status_unavailable"
	MsgTokenDbError              = "token.db_error"
)

// Redemption related messages
const (
	MsgRedemptionNameLength        = "redemption.name_length"
	MsgRedemptionCountPositive     = "redemption.count_positive"
	MsgRedemptionCountMax          = "redemption.count_max"
	MsgRedemptionCreateFailed      = "redemption.create_failed"
	MsgRedemptionInvalid           = "redemption.invalid"
	MsgRedemptionUsed              = "redemption.used"
	MsgRedemptionExpired           = "redemption.expired"
	MsgRedemptionFailed            = "redemption.failed"
	MsgRedemptionNotProvided       = "redemption.not_provided"
	MsgRedemptionExpireTimeInvalid = "redemption.expire_time_invalid"
)

// User related messages
const (
	MsgUserPasswordLoginDisabled     = "user.password_login_disabled"
	MsgUserRegisterDisabled          = "user.register_disabled"
	MsgUserPasswordRegisterDisabled  = "user.password_register_disabled"
	MsgUserUsernameOrPasswordEmpty   = "user.username_or_password_empty"
	MsgUserUsernameOrPasswordError   = "user.username_or_password_error"
	MsgUserEmailOrPasswordEmpty      = "user.email_or_password_empty"
	MsgUserExists                    = "user.exists"
	MsgUserNotExists                 = "user.not_exists"
	MsgUserDisabled                  = "user.disabled"
	MsgUserSessionSaveFailed         = "user.session_save_failed"
	MsgUserRequire2FA                = "user.require_2fa"
	MsgUserEmailVerificationRequired = "user.email_verification_required"
	MsgUserVerificationCodeError     = "user.verification_code_error"
	MsgUserInputInvalid              = "user.input_invalid"
	MsgUserNoPermissionSameLevel     = "user.no_permission_same_level"
	MsgUserNoPermissionHigherLevel   = "user.no_permission_higher_level"
	MsgUserCannotCreateHigherLevel   = "user.cannot_create_higher_level"
	MsgUserCannotDeleteRootUser      = "user.cannot_delete_root_user"
	MsgUserCannotDisableRootUser     = "user.cannot_disable_root_user"
	MsgUserCannotDemoteRootUser      = "user.cannot_demote_root_user"
	MsgUserAlreadyAdmin              = "user.already_admin"
	MsgUserAlreadyCommon             = "user.already_common"
	MsgUserAdminCannotPromote        = "user.admin_cannot_promote"
	MsgUserOriginalPasswordError     = "user.original_password_error"
	MsgUserInviteQuotaInsufficient   = "user.invite_quota_insufficient"
	MsgUserTransferQuotaMinimum      = "user.transfer_quota_minimum"
	MsgUserTransferSuccess           = "user.transfer_success"
	MsgUserTransferFailed            = "user.transfer_failed"
	MsgUserTopUpProcessing           = "user.topup_processing"
	MsgUserRegisterFailed            = "user.register_failed"
	MsgUserDefaultTokenFailed        = "user.default_token_failed"
	MsgUserAffCodeEmpty              = "user.aff_code_empty"
	MsgUserEmailEmpty                = "user.email_empty"
	MsgUserGitHubIdEmpty             = "user.github_id_empty"
	MsgUserDiscordIdEmpty            = "user.discord_id_empty"
	MsgUserOidcIdEmpty               = "user.oidc_id_empty"
	MsgUserWeChatIdEmpty             = "user.wechat_id_empty"
	MsgUserTelegramIdEmpty           = "user.telegram_id_empty"
	MsgUserTelegramNotBound          = "user.telegram_not_bound"
	MsgUserLinuxDOIdEmpty            = "user.linux_do_id_empty"
	MsgUserQuotaChangeZero           = "user.quota_change_zero"
)

// Quota related messages
const (
	MsgQuotaNegative        = "quota.negative"
	MsgQuotaExceedMax       = "quota.exceed_max"
	MsgQuotaInsufficient    = "quota.insufficient"
	MsgQuotaWarningInvalid  = "quota.warning_invalid"
	MsgQuotaThresholdGtZero = "quota.threshold_gt_zero"
)

// Subscription related messages
const (
	MsgSubscriptionNotEnabled       = "subscription.not_enabled"
	MsgSubscriptionTitleEmpty       = "subscription.title_empty"
	MsgSubscriptionPriceNegative    = "subscription.price_negative"
	MsgSubscriptionPriceMax         = "subscription.price_max"
	MsgSubscriptionPurchaseLimitNeg = "subscription.purchase_limit_negative"
	MsgSubscriptionQuotaNegative    = "subscription.quota_negative"
	MsgSubscriptionGroupNotExists   = "subscription.group_not_exists"
	MsgSubscriptionResetCycleGtZero = "subscription.reset_cycle_gt_zero"
	MsgSubscriptionPurchaseMax      = "subscription.purchase_max"
	MsgSubscriptionInvalidId        = "subscription.invalid_id"
	MsgSubscriptionInvalidUserId    = "subscription.invalid_user_id"
)

// Payment related messages
const (
	MsgPaymentNotConfigured      = "payment.not_configured"
	MsgPaymentMethodNotExists    = "payment.method_not_exists"
	MsgPaymentCallbackError      = "payment.callback_error"
	MsgPaymentCreateFailed       = "payment.create_failed"
	MsgPaymentStartFailed        = "payment.start_failed"
	MsgPaymentAmountTooLow       = "payment.amount_too_low"
	MsgPaymentStripeNotConfig    = "payment.stripe_not_configured"
	MsgPaymentWebhookNotConfig   = "payment.webhook_not_configured"
	MsgPaymentPriceIdNotConfig   = "payment.price_id_not_configured"
	MsgPaymentCreemNotConfig     = "payment.creem_not_configured"
	MsgPaymentComplianceRequired = "payment.compliance_required"
)

// Topup related messages
const (
	MsgTopupNotProvided    = "topup.not_provided"
	MsgTopupOrderNotExists = "topup.order_not_exists"
	MsgTopupOrderStatus    = "topup.order_status"
	MsgTopupFailed         = "topup.failed"
	MsgTopupInvalidQuota   = "topup.invalid_quota"
)

// Channel related messages
const (
	MsgChannelNotExists          = "channel.not_exists"
	MsgChannelIdFormatError      = "channel.id_format_error"
	MsgChannelNoAvailableKey     = "channel.no_available_key"
	MsgChannelGetListFailed      = "channel.get_list_failed"
	MsgChannelGetTagsFailed      = "channel.get_tags_failed"
	MsgChannelGetKeyFailed       = "channel.get_key_failed"
	MsgChannelGetOllamaFailed    = "channel.get_ollama_failed"
	MsgChannelQueryFailed        = "channel.query_failed"
	MsgChannelNoValidUpstream    = "channel.no_valid_upstream"
	MsgChannelUpstreamSaturated  = "channel.upstream_saturated"
	MsgChannelGetAvailableFailed = "channel.get_available_failed"
)

// Model related messages
const (
	MsgModelNameEmpty     = "model.name_empty"
	MsgModelNameExists    = "model.name_exists"
	MsgModelIdMissing     = "model.id_missing"
	MsgModelGetListFailed = "model.get_list_failed"
	MsgModelGetFailed     = "model.get_failed"
	MsgModelResetSuccess  = "model.reset_success"
)

// Vendor related messages
const (
	MsgVendorNameEmpty  = "vendor.name_empty"
	MsgVendorNameExists = "vendor.name_exists"
	MsgVendorIdMissing  = "vendor.id_missing"
)

// Group related messages
const (
	MsgGroupNameTypeEmpty = "group.name_type_empty"
	MsgGroupNameExists    = "group.name_exists"
	MsgGroupIdMissing     = "group.id_missing"
)

// Checkin related messages
const (
	MsgCheckinDisabled     = "checkin.disabled"
	MsgCheckinAlreadyToday = "checkin.already_today"
	MsgCheckinFailed       = "checkin.failed"
	MsgCheckinQuotaFailed  = "checkin.quota_failed"
	MsgCheckinSuccess      = "checkin.success"
)

// Passkey related messages
const (
	MsgPasskeyCreateFailed  = "passkey.create_failed"
	MsgPasskeyLoginAbnormal = "passkey.login_abnormal"
	MsgPasskeyUpdateFailed  = "passkey.update_failed"
	MsgPasskeyInvalidUserId = "passkey.invalid_user_id"
	MsgPasskeyVerifyFailed  = "passkey.verify_failed"
)

// 2FA related messages
const (
	MsgTwoFANotEnabled    = "twofa.not_enabled"
	MsgTwoFAUserIdEmpty   = "twofa.user_id_empty"
	MsgTwoFAAlreadyExists = "twofa.already_exists"
	MsgTwoFARecordIdEmpty = "twofa.record_id_empty"
	MsgTwoFACodeInvalid   = "twofa.code_invalid"
)

// Rate limit related messages
const (
	MsgRateLimitReached      = "rate_limit.reached"
	MsgRateLimitTotalReached = "rate_limit.total_reached"
)

// Setting related messages
const (
	MsgSettingInvalidType      = "setting.invalid_type"
	MsgSettingWebhookEmpty     = "setting.webhook_empty"
	MsgSettingWebhookInvalid   = "setting.webhook_invalid"
	MsgSettingEmailInvalid     = "setting.email_invalid"
	MsgSettingBarkUrlEmpty     = "setting.bark_url_empty"
	MsgSettingBarkUrlInvalid   = "setting.bark_url_invalid"
	MsgSettingGotifyUrlEmpty   = "setting.gotify_url_empty"
	MsgSettingGotifyTokenEmpty = "setting.gotify_token_empty"
	MsgSettingGotifyUrlInvalid = "setting.gotify_url_invalid"
	MsgSettingUrlMustHttp      = "setting.url_must_http"
	MsgSettingSaved            = "setting.saved"
)

// Deployment related messages (io.net)
const (
	MsgDeploymentNotEnabled     = "deployment.not_enabled"
	MsgDeploymentIdRequired     = "deployment.id_required"
	MsgDeploymentContainerIdReq = "deployment.container_id_required"
	MsgDeploymentNameEmpty      = "deployment.name_empty"
	MsgDeploymentNameTaken      = "deployment.name_taken"
	MsgDeploymentHardwareIdReq  = "deployment.hardware_id_required"
	MsgDeploymentHardwareInvId  = "deployment.hardware_invalid_id"
	MsgDeploymentApiKeyRequired = "deployment.api_key_required"
	MsgDeploymentInvalidPayload = "deployment.invalid_payload"
	MsgDeploymentNotFound       = "deployment.not_found"
)

// Performance related messages
const (
	MsgPerfDiskCacheCleared  = "performance.disk_cache_cleared"
	MsgPerfStatsReset        = "performance.stats_reset"
	MsgPerfGcExecuted        = "performance.gc_executed"
	MsgPerfInvalidMode       = "performance.invalid_mode"
	MsgPerfInvalidValue      = "performance.invalid_value"
	MsgPerfLogDirNotConfig   = "performance.log_dir_not_configured"
)

// Ability related messages
const (
	MsgAbilityDbCorrupted   = "ability.db_corrupted"
	MsgAbilityRepairRunning = "ability.repair_running"
)

// OAuth related messages
const (
	MsgOAuthInvalidCode     = "oauth.invalid_code"
	MsgOAuthGetUserErr      = "oauth.get_user_error"
	MsgOAuthAccountUsed     = "oauth.account_used"
	MsgOAuthUnknownProvider = "oauth.unknown_provider"
	MsgOAuthStateInvalid    = "oauth.state_invalid"
	MsgOAuthNotEnabled      = "oauth.not_enabled"
	MsgOAuthUserDeleted     = "oauth.user_deleted"
	MsgOAuthUserBanned      = "oauth.user_banned"
	MsgOAuthBindSuccess     = "oauth.bind_success"
	MsgOAuthAlreadyBound    = "oauth.already_bound"
	MsgOAuthConnectFailed   = "oauth.connect_failed"
	MsgOAuthTokenFailed     = "oauth.token_failed"
	MsgOAuthUserInfoEmpty   = "oauth.user_info_empty"
	MsgOAuthTrustLevelLow   = "oauth.trust_level_low"
)

// Model layer error messages (for translation in controller)
const (
	MsgRedeemFailed          = "redeem.failed"
	MsgCreateDefaultTokenErr = "user.create_default_token_error"
	MsgUuidDuplicate         = "common.uuid_duplicate"
	MsgInvalidInput          = "common.invalid_input"
)

// Distributor related messages
const (
	MsgDistributorInvalidRequest          = "distributor.invalid_request"
	MsgDistributorInvalidChannelId        = "distributor.invalid_channel_id"
	MsgDistributorChannelDisabled         = "distributor.channel_disabled"
	MsgDistributorAffinityChannelDisabled = "distributor.affinity_channel_disabled"
	MsgDistributorTokenNoModelAccess      = "distributor.token_no_model_access"
	MsgDistributorTokenModelForbidden     = "distributor.token_model_forbidden"
	MsgDistributorModelNameRequired       = "distributor.model_name_required"
	MsgDistributorInvalidPlayground       = "distributor.invalid_playground_request"
	MsgDistributorGroupAccessDenied       = "distributor.group_access_denied"
	MsgDistributorGetChannelFailed        = "distributor.get_channel_failed"
	MsgDistributorNoAvailableChannel      = "distributor.no_available_channel"
	MsgDistributorInvalidMidjourney       = "distributor.invalid_midjourney_request"
	MsgDistributorInvalidParseModel       = "distributor.invalid_request_parse_model"
)

// Custom OAuth provider related messages
const (
	MsgCustomOAuthNotFound          = "custom_oauth.not_found"
	MsgCustomOAuthSlugEmpty         = "custom_oauth.slug_empty"
	MsgCustomOAuthSlugExists        = "custom_oauth.slug_exists"
	MsgCustomOAuthNameEmpty         = "custom_oauth.name_empty"
	MsgCustomOAuthHasBindings       = "custom_oauth.has_bindings"
	MsgCustomOAuthBindingNotFound   = "custom_oauth.binding_not_found"
	MsgCustomOAuthProviderIdInvalid = "custom_oauth.provider_id_field_invalid"
)

// TOTP messages
const (
	MsgTotpCodeMustBe6Digits = "totp.code_must_be_6_digits"
	MsgTotpCodeDigitsOnly    = "totp.code_digits_only"
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

	// channel_affinity_cache
	MsgChannelAffinityMissingRuleName = "channel.affinity.missing_rule_name"
	MsgChannelAffinityMissingKeyFp    = "channel.affinity.missing_key_fp"

	// channel-billing
	MsgChannelBillingMultiKeyNotSupported = "channel.billing.multi_key_not_supported"

	// channel (key management)
	MsgChannelNotFound              = "channel.not_found"
	MsgChannelNotMultiKey           = "channel.not_multi_key"
	MsgChannelKeyIndexNotSpecified  = "channel.key_index_not_specified"
	MsgChannelKeyIndexOutOfRange    = "channel.key_index_out_of_range"
	MsgChannelKeyHasBeenDisabled    = "channel.key_has_been_disabled"
	MsgChannelKeyHasBeenEnabled     = "channel.key_has_been_enabled"
	MsgChannelKeyHasBeenDeleted     = "channel.key_has_been_deleted"
	MsgChannelNoKeysToDisable       = "channel.no_keys_to_disable"
	MsgChannelNoAutoDisabledKeys    = "channel.no_auto_disabled_keys"
	MsgChannelCannotDeleteLastKey   = "channel.cannot_delete_last_key"
	MsgChannelUnsupportedOperation  = "channel.unsupported_operation"
	MsgChannelUnsupportedAddMode    = "channel.unsupported_add_mode"
	MsgChannelTagEmpty              = "channel.tag_empty"
	MsgChannelParamOverrideInvalid  = "channel.param_override_invalid"
	MsgChannelHeaderOverrideInvalid = "channel.header_override_invalid"
	MsgChannelGetSuccess            = "channel.get_success"
	MsgChannelRefreshed             = "channel.refreshed"
	MsgChannelEnabledKeys           = "channel.enabled_keys"
	MsgChannelDisabledKeys          = "channel.disabled_keys"
	MsgChannelDeletedAutoKeys       = "channel.deleted_auto_keys"
	MsgChannelOllamaOnly            = "channel.ollama_only"
	MsgChannelModelPullSuccess      = "channel.model_pull_success"
	MsgChannelModelDeleteSuccess    = "channel.model_delete_success"
	MsgChannelFetchModelsFailed     = "channel.fetch_models_failed"
)

// Codex messages
const (
	MsgCodexParseAuthFailed        = "codex.parse_auth_failed"
	MsgCodexExchangeFailed         = "codex.exchange_failed"
	MsgCodexParseCredFailed        = "codex.parse_cred_failed"
	MsgCodexGetUsageFailed         = "codex.get_usage_failed"
	MsgCodexMissingCode            = "codex.missing_code"
	MsgCodexMissingState           = "codex.missing_state"
	MsgCodexChannelTypeNotCodex    = "codex.channel_type_not_codex"
	MsgCodexFlowExpired            = "codex.flow_expired"
	MsgCodexStateMismatch          = "codex.state_mismatch"
	MsgCodexExtractAccountIdFailed = "codex.extract_account_id_failed"
	MsgCodexSaved                  = "codex.saved"
	MsgCodexGenerated              = "codex.generated"
)

// Custom OAuth extended messages
const (
	MsgCustomOAuthInvalidId          = "custom_oauth.invalid_id"
	MsgCustomOAuthInvalidParams      = "custom_oauth.invalid_params"
	MsgCustomOAuthDiscoveryEmpty     = "custom_oauth.discovery_url_empty"
	MsgCustomOAuthDiscoveryInvalid   = "custom_oauth.discovery_url_invalid"
	MsgCustomOAuthDiscoveryFailed    = "custom_oauth.discovery_fetch_failed"
	MsgCustomOAuthDiscoveryParseFail = "custom_oauth.discovery_parse_failed"
	MsgCustomOAuthSlugConflict       = "custom_oauth.slug_conflict"
	MsgCustomOAuthInvalidProviderId  = "custom_oauth.invalid_provider_id"
	MsgCustomOAuthCheckBindingFailed = "custom_oauth.check_binding_failed"
	MsgCustomOAuthDeleteHasBindings  = "custom_oauth.delete_has_bindings"
	MsgCustomOAuthNotLoggedIn        = "custom_oauth.not_logged_in"
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
	MsgPasskeySettingsNotFound   = "passkey.settings_not_found"
	MsgPasskeySessionExpired     = "passkey.session_expired"
	MsgPasskeySessionFormatInvalid = "passkey.session_format_invalid"
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
	MsgVerifySuccess           = "verify.success"
)

// Topup extended messages
const (
	MsgTopupAmountTooSmall        = "topup.amount_too_small"
	MsgTopupAmountTooLarge        = "topup.amount_too_large"
	MsgTopupGetGroupFailed        = "topup.get_group_failed"
	MsgTopupAmountTooLow2         = "topup.amount_too_low"
	MsgTopupUnsupportedProvider   = "topup.unsupported_provider"
	MsgTopupRedirectNotTrusted    = "topup.redirect_not_trusted"
	MsgTopupCancelRedirectBad     = "topup.cancel_redirect_not_trusted"
	MsgTopupWaffoPancakeNotConfig = "topup.waffo_pancake_not_configured"
	MsgTopupWaffoNotConfig        = "topup.waffo_not_configured"
	MsgTopupSelectProduct         = "topup.select_product"
	MsgTopupProductConfigError    = "topup.product_config_error"
	MsgTopupReadQueryError        = "topup.read_query_error"
	MsgTopupSaveConfigFailed      = "topup.save_config_failed"
	MsgTopupFetchCatalogFailed    = "topup.fetch_catalog_failed"
	MsgTopupPlanNameEmpty         = "topup.plan_name_empty"
	MsgTopupPlanPriceEmpty        = "topup.plan_price_empty"
	MsgTopupWaffoPancakeNotFull   = "topup.waffo_pancake_not_full_configured"
	MsgTopupCreatePlanFailed      = "topup.create_plan_failed"
	MsgTopupFetchProductsFailed   = "topup.fetch_products_failed"
)

// Subscription payment messages
const (
	MsgSubPaymentInvalidParams = "subscription_payment.invalid_params"
)

// Playground messages
const (
	MsgPlaygroundNoAccessToken = "playground.no_access_token"
)

// Option messages
const (
	MsgOptionComplianceFieldRestricted = "option.compliance_field_restricted"
)

// Ratio sync messages
const (
	MsgRatioSyncInvalidParams = "ratio_sync.invalid_params"
)

// Task video messages
const (
	MsgTaskVideoGetChannelFailed = "task_video.get_channel_failed"
	MsgTaskVideoGetModelFailed   = "task_video.get_model_failed"
	MsgTaskVideoUpstreamFailed   = "task_video.upstream_failed"
)

// Ali relay messages
const (
	MsgAliImageRequired     = "relay.ali.image_required"
	MsgAliImageOpenFailed   = "relay.ali.image_open_failed"
	MsgAliImageReadFailed   = "relay.ali.image_read_failed"
	MsgAliImageBase64Failed = "relay.ali.image_base64_failed"
	MsgAliAsyncTimeout      = "relay.ali.async_timeout"
)

// 2FA extended messages
const (
	MsgTwoFAGenerateKeyFailed  = "twofa.generate_key_failed"
	MsgTwoFAGenerateCodeFailed = "twofa.generate_code_failed"
	MsgTwoFASaveCodeFailed     = "twofa.save_code_failed"
	MsgTwoFAGetCountFailed     = "twofa.get_count_failed"
)

// Model sync messages
const (
	MsgModelSyncGetFailed         = "model_sync.get_failed"
	MsgModelSyncGetUpstreamFailed = "model_sync.get_upstream_failed"
)

// Console config messages
const (
	MsgConsoleGetConfigFailed = "console.get_config_failed"
)

// Prefill group messages
const (
	MsgPrefillGroupNameTypeEmpty = "prefill_group.name_type_empty"
	MsgPrefillGroupNameExists    = "prefill_group.name_exists"
	MsgPrefillGroupIdMissing     = "prefill_group.id_missing"
)

// Contact messages
const (
	MsgContactInvalidRequest = "contact.invalid_request"
	MsgContactRequiredFields = "contact.required_fields"
	MsgContactInvalidEmail   = "contact.invalid_email"
	MsgContactSendFailed     = "contact.send_failed"
	MsgContactSuccess        = "contact.success"
)

// API deprecation / log messages
const (
	MsgApiDeprecated = "common.api_deprecated"
)

// Usedata messages
const (
	MsgUsedataTimeRangeExceeded = "usedata.time_range_exceeded"
)

// Ratio config messages
const (
	MsgRatioConfigDisabled = "ratio_config.disabled"
)

// Setup messages
const (
	MsgSetupAlreadyInitialized   = "setup.already_initialized"
	MsgSetupUsernameMaxLength    = "setup.username_max_length"
	MsgSetupPasswordMismatch     = "setup.password_mismatch"
	MsgSetupPasswordMinLength    = "setup.password_min_length"
	MsgSetupSuccess              = "setup.success"
)

// 2FA controller messages
const (
	MsgTwoFAAlreadyEnabledFirst = "twofa.already_enabled_first"
	MsgTwoFAKeyFailed           = "twofa.key_failed"
	MsgTwoFABackupCodeFailed    = "twofa.backup_code_failed"
	MsgTwoFASaveBackupFailed    = "twofa.save_backup_failed"
	MsgTwoFAInitSuccess         = "twofa.init_success"
	MsgTwoFAInitFirst           = "twofa.init_first"
	MsgTwoFAAlreadyActive       = "twofa.already_active"
	MsgTwoFAIncorrectCode       = "twofa.incorrect_code"
	MsgTwoFAEnabled             = "twofa.enabled"
	MsgTwoFADisabled            = "twofa.disabled"
	MsgTwoFABackupRegenSuccess  = "twofa.backup_regen_success"
	MsgTwoFASessionExpired      = "twofa.session_expired"
	MsgTwoFASessionInvalid      = "twofa.session_invalid"
	MsgTwoFAInvalidUserId       = "twofa.invalid_user_id"
	MsgTwoFAPrivilegeError      = "twofa.privilege_error"
	MsgTwoFAForceDisabled       = "twofa.force_disabled"
)

// Middleware messages
const (
	MsgTurnstileTokenEmpty          = "middleware.turnstile_token_empty"
	MsgTurnstileVerificationFailed  = "middleware.turnstile_verification_failed"
	MsgSessionSaveFailed            = "middleware.session_save_failed"
	MsgSecureVerificationRequired   = "middleware.secure_verification_required"
	MsgVerificationStateError       = "middleware.verification_state_error"
	MsgVerificationExpired          = "middleware.verification_expired"
	MsgEmailSendingTooFrequent      = "middleware.email_sending_too_frequent"
	MsgEmailSendingTooFrequentRetry = "middleware.email_sending_too_frequent_retry"
	MsgModuleDisabled               = "middleware.module_disabled"
	MsgPanicDetected                = "middleware.panic_detected"
)

// Option controller messages
const (
	MsgOptionGitHubConfigMissing   = "option.github_config_missing"
	MsgOptionDiscordConfigMissing  = "option.discord_config_missing"
	MsgOptionOIDCConfigMissing     = "option.oidc_config_missing"
	MsgOptionLinuxDOConfigMissing  = "option.linuxdo_config_missing"
	MsgOptionEmailDomainMissing    = "option.email_domain_missing"
	MsgOptionWeChatConfigMissing   = "option.wechat_config_missing"
	MsgOptionTurnstileConfigMissing = "option.turnstile_config_missing"
	MsgOptionTelegramConfigMissing = "option.telegram_config_missing"
	MsgOptionInvalidTheme          = "option.invalid_theme"
)

// Codex usage messages
const (
	MsgCodexMultiKeyNotSupported = "codex.multi_key_not_supported"
	MsgCodexAccessTokenRequired  = "codex.access_token_required"
	MsgCodexAccountIdRequired    = "codex.account_id_required"
)

// Email sending messages
const (
	MsgEmailSendFailed = "email.send_failed"
)

// Misc messages
const (
	MsgMigrated               = "common.migrated"
	MsgContactNotConfigured   = "contact.not_configured"
	MsgEmailRecipientRequired = "email.recipient_required"
	MsgLogTimestampRequired   = "log.timestamp_required"
	MsgDbConnectionFailed     = "common.db_connection_failed"
	MsgInvalidEmail           = "common.invalid_email"
	MsgEmailDomainRestricted  = "common.email_domain_restricted"
	MsgEmailAliasRestricted   = "common.email_alias_restricted"
	MsgEmailTaken             = "common.email_taken"
	MsgResetLinkInvalid       = "common.reset_link_invalid"
	MsgUserGroupFailed        = "common.user_group_failed"
	MsgOrgNotFound            = "common.org_not_found"
	MsgModelRequired          = "common.model_required"
	MsgPaymentComplianceDashboardOnly = "payment.compliance_dashboard_only"
)

// Passkey controller messages
const (
	MsgPasskeyNotEnabled   = "passkey.not_enabled"
	MsgPasskeyRegistered   = "passkey.registered"
	MsgPasskeyUnbound      = "passkey.unbound"
	MsgPasskeyNotBound     = "passkey.not_bound"
	MsgPasskeyReset        = "passkey.reset"
	MsgPasskeyVerified     = "passkey.verified"
	MsgPasskeyNotLoggedIn  = "passkey.not_logged_in"
)

// Organization controller messages
const (
	MsgOrgRootRequired        = "org.root_required"
	MsgOrgOwnerRequired       = "org.owner_required"
	MsgOrgAdminRequired       = "org.admin_required"
	MsgOrgNoFieldsToUpdate    = "org.no_fields_to_update"
	MsgOrgNoUserFieldsToUpdate = "org.no_user_fields_to_update"
	MsgOrgInvalidStartTs      = "org.invalid_start_timestamp"
	MsgOrgInvalidEndTs        = "org.invalid_end_timestamp"
	MsgOrgInvalidPreset       = "org.invalid_preset"
	MsgOrgTimestampOrder      = "org.timestamp_order"
	MsgOrgIdRequired          = "org.id_required"
	MsgOrgSubNoFieldsToUpdate = "org.subscription_no_fields_to_update"
	MsgOrgTargetPermDenied    = "org.target_permission_denied"
	MsgOrgGlobalAdminUnmanageable = "org.global_admin_unmanageable"
	MsgOrgCannotReassignSelf  = "org.cannot_reassign_self"
	MsgOrgInvalidRole         = "org.invalid_role"
	MsgOrgOnlyOwnerCanAssignOwner = "org.only_owner_can_assign_owner"
	MsgOrgUserNotInOrg        = "org.user_not_in_org"
	MsgOrgOwnerCannotBeRemoved = "org.owner_cannot_be_removed"
)
