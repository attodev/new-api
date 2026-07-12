package controller

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

var completionRatioMetaOptionKeys = []string{
	"ModelPrice",
	"ModelRatio",
	"CompletionRatio",
	"CacheRatio",
	"CreateCacheRatio",
	"ImageRatio",
	"AudioRatio",
	"AudioCompletionRatio",
}

func isPaymentComplianceOptionKey(key string) bool {
	return strings.HasPrefix(key, "payment_setting.compliance_")
}

func isPositiveOptionValue(value string) bool {
	intValue, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil {
		return intValue > 0
	}
	floatValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && floatValue > 0
}

func collectModelNamesFromOptionValue(raw string, modelNames map[string]struct{}) {
	if strings.TrimSpace(raw) == "" {
		return
	}

	var parsed map[string]any
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return
	}

	for modelName := range parsed {
		modelNames[modelName] = struct{}{}
	}
}

func buildCompletionRatioMetaValue(optionValues map[string]string) string {
	modelNames := make(map[string]struct{})
	for _, key := range completionRatioMetaOptionKeys {
		collectModelNamesFromOptionValue(optionValues[key], modelNames)
	}

	meta := make(map[string]ratio_setting.CompletionRatioInfo, len(modelNames))
	for modelName := range modelNames {
		meta[modelName] = ratio_setting.GetCompletionRatioInfo(modelName)
	}

	jsonBytes, err := common.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func isPublicTossClientOption(key, value string) bool {
	switch key {
	case "TossClientKey", "TossTestClientKey", "TossBillingClientKey", "TossBillingTestClientKey":
		// Empty values are safe and keep the settings form deterministic. For a
		// populated legacy row, trust the key's actual Toss role rather than the
		// database field name: old/swapped sk or gsk values must never reach a
		// browser through the public options endpoint.
		value = strings.ToLower(strings.TrimSpace(value))
		return value == "" || (tossKeyEnvironment(value) != "" && hasTossKeyRolePrefix(value, "ck"))
	default:
		return false
	}
}

func GetOptions(c *gin.Context) {
	var options []*model.Option
	optionValues := make(map[string]string)
	common.OptionMapRWMutex.Lock()
	for k, v := range common.OptionMap {
		value := common.Interface2String(v)
		isSensitiveKey := strings.HasSuffix(k, "Token") ||
			strings.HasSuffix(k, "Secret") ||
			strings.HasSuffix(k, "Key") ||
			strings.HasSuffix(k, "secret") ||
			strings.HasSuffix(k, "api_key")
		if isSensitiveKey && !isPublicTossClientOption(k, value) {
			continue
		}
		options = append(options, &model.Option{
			Key:   k,
			Value: value,
		})
		for _, optionKey := range completionRatioMetaOptionKeys {
			if optionKey == k {
				optionValues[k] = value
				break
			}
		}
	}
	common.OptionMapRWMutex.Unlock()
	options = append(options, &model.Option{
		Key:   "CompletionRatioMeta",
		Value: buildCompletionRatioMetaValue(optionValues),
	})
	tossState, err := model.GetTossConfigState()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	options = append(options,
		&model.Option{Key: "TossConfigRepairRequired", Value: strconv.FormatBool(tossState.RepairRequired)},
		&model.Option{Key: "TossConfigMaintenanceRequired", Value: strconv.FormatBool(tossState.MaintenanceRequired)},
	)
	if tossState.RepairRequired {
		options = append(options, &model.Option{Key: "TossConfigRepairToken", Value: tossState.RepairToken})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

var tossAtomicOptionKeys = map[string]struct{}{
	"TossEnabled":                   {},
	"TossBillingEnabled":            {},
	"TossWalletAutoRechargeEnabled": {},
	"TossTestMode":                  {},
	"TossClientKey":                 {},
	"TossSecretKey":                 {},
	"TossTestClientKey":             {},
	"TossTestSecretKey":             {},
	"TossBillingClientKey":          {},
	"TossBillingSecretKey":          {},
	"TossBillingTestClientKey":      {},
	"TossBillingTestSecretKey":      {},
	"TossUnitPrice":                 {},
	"TossMinTopUp":                  {},
}

type tossOptionKeyPair struct {
	client   string
	secret   string
	expected string
}

var tossOptionKeyPairs = []tossOptionKeyPair{
	{client: "TossClientKey", secret: "TossSecretKey", expected: "live"},
	{client: "TossTestClientKey", secret: "TossTestSecretKey", expected: "test"},
	{client: "TossBillingClientKey", secret: "TossBillingSecretKey", expected: "live"},
	{client: "TossBillingTestClientKey", secret: "TossBillingTestSecretKey", expected: "test"},
}

func optionValueToString(value any) string {
	switch typed := value.(type) {
	case bool:
		return common.Interface2String(typed)
	case float64:
		return common.Interface2String(typed)
	case int:
		return common.Interface2String(typed)
	default:
		return fmt.Sprintf("%v", value)
	}
}

type tossOptionsUpdateRequest struct {
	Updates             []OptionUpdateRequest `json:"updates"`
	RepairCompleteSet   bool                  `json:"repair_complete_set"`
	ExpectedRepairToken string                `json:"expected_repair_token"`
	RetryMaintenance    bool                  `json:"retry_maintenance"`
}

func normalizeTossOptionUpdate(update OptionUpdateRequest) (string, error) {
	const maxTossKeyBytes = 2048

	switch update.Key {
	case "TossEnabled", "TossBillingEnabled", "TossWalletAutoRechargeEnabled", "TossTestMode":
		value, ok := update.Value.(bool)
		if !ok {
			return "", errors.New("Toss boolean option must be a boolean")
		}
		return strconv.FormatBool(value), nil
	case "TossUnitPrice":
		value, ok := update.Value.(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > float64(setting.TossMaximumChargeAmountKRW) {
			return "", fmt.Errorf("Toss unit price must be positive and at most %d KRW", setting.TossMaximumChargeAmountKRW)
		}
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	case "TossMinTopUp":
		value, ok := update.Value.(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < float64(setting.TossCardMinimumAmountKRW) || value > float64(setting.TossMaximumChargeAmountKRW) {
			return "", fmt.Errorf("Toss minimum top-up must be an integer between %d and %d KRW", setting.TossCardMinimumAmountKRW, setting.TossMaximumChargeAmountKRW)
		}
		return strconv.FormatInt(int64(value), 10), nil
	default:
		value, ok := update.Value.(string)
		if !ok {
			return "", errors.New("Toss key option must be a string")
		}
		value = strings.TrimSpace(value)
		if len(value) > maxTossKeyBytes {
			return "", errors.New("Toss key option is too long")
		}
		return value, nil
	}
}

func currentTossOptionValues() map[string]string {
	snapshot := setting.GetTossConfigSnapshot()
	return map[string]string{
		"TossEnabled":                   strconv.FormatBool(snapshot.Enabled),
		"TossBillingEnabled":            strconv.FormatBool(snapshot.BillingEnabled),
		"TossWalletAutoRechargeEnabled": strconv.FormatBool(snapshot.WalletAutoRechargeEnabled),
		"TossTestMode":                  strconv.FormatBool(snapshot.TestMode),
		"TossClientKey":                 strings.TrimSpace(snapshot.ClientKey),
		"TossSecretKey":                 strings.TrimSpace(snapshot.SecretKey),
		"TossTestClientKey":             strings.TrimSpace(snapshot.TestClientKey),
		"TossTestSecretKey":             strings.TrimSpace(snapshot.TestSecretKey),
		"TossBillingClientKey":          strings.TrimSpace(snapshot.BillingClientKey),
		"TossBillingSecretKey":          strings.TrimSpace(snapshot.BillingSecretKey),
		"TossBillingTestClientKey":      strings.TrimSpace(snapshot.BillingTestClientKey),
		"TossBillingTestSecretKey":      strings.TrimSpace(snapshot.BillingTestSecretKey),
		"TossUnitPrice":                 strconv.FormatFloat(snapshot.UnitPrice, 'f', -1, 64),
		"TossMinTopUp":                  strconv.Itoa(snapshot.MinTopUp),
	}
}

func tossKeyEnvironment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.HasPrefix(value, "test_"):
		return "test"
	case strings.HasPrefix(value, "live_"):
		return "live"
	default:
		return ""
	}
}

func validateTossKeyPairEnvironment(clientKey, secretKey, expected string) error {
	clientLower := strings.ToLower(strings.TrimSpace(clientKey))
	secretLower := strings.ToLower(strings.TrimSpace(secretKey))
	if hasTossKeyRolePrefix(clientLower, "sk") || hasTossKeyRolePrefix(clientLower, "gsk") {
		return errors.New("Toss client key field contains a secret key")
	}
	if hasTossKeyRolePrefix(secretLower, "ck") || hasTossKeyRolePrefix(secretLower, "gck") {
		return errors.New("Toss secret key field contains a client key")
	}
	// This code uses the payment-window SDK (`payment.requestPayment`) and the
	// automatic-billing API, both of which require API-individual ck/sk keys.
	// Toss widget gck/gsk pairs are a different integration namespace.
	if hasTossKeyRolePrefix(clientLower, "gck") || hasTossKeyRolePrefix(secretLower, "gsk") {
		return errors.New("Toss payment-window integration requires API-individual ck/sk keys, not widget gck/gsk keys")
	}
	clientEnv := tossKeyEnvironment(clientKey)
	secretEnv := tossKeyEnvironment(secretKey)
	if clientEnv == "" || secretEnv == "" {
		return errors.New("Toss keys must use an official test_ or live_ prefix")
	}
	if !hasTossKeyRolePrefix(clientLower, "ck") {
		return errors.New("Toss client key must be an API-individual ck key")
	}
	if !hasTossKeyRolePrefix(secretLower, "sk") {
		return errors.New("Toss secret key must be an API-individual sk key")
	}
	if clientEnv != "" && clientEnv != expected {
		return fmt.Errorf("Toss client key contains a %s key", clientEnv)
	}
	if secretEnv != "" && secretEnv != expected {
		return fmt.Errorf("Toss secret key contains a %s key", secretEnv)
	}
	if clientEnv != "" && secretEnv != "" && clientEnv != secretEnv {
		return errors.New("Toss client and secret key environments do not match")
	}
	return nil
}

// Toss issues both API-individual keys (ck/sk) and payment-widget keys
// (gck/gsk). Secret variants must never be accepted in a client-key field,
// because client keys are intentionally returned to the browser.
func hasTossKeyRolePrefix(value, role string) bool {
	return strings.HasPrefix(value, "test_"+role+"_") ||
		strings.HasPrefix(value, "live_"+role+"_")
}

func validateTossOptionUpdateSet(current, updates map[string]string) error {
	final := make(map[string]string, len(current)+len(updates))
	for key, value := range current {
		final[key] = strings.TrimSpace(value)
	}
	for key, value := range updates {
		final[key] = strings.TrimSpace(value)
	}

	for _, pair := range tossOptionKeyPairs {
		_, clientUpdated := updates[pair.client]
		_, secretUpdated := updates[pair.secret]
		pairTouched := clientUpdated || secretUpdated
		finalClient := strings.TrimSpace(final[pair.client])
		finalSecret := strings.TrimSpace(final[pair.secret])

		// Bind every credential change to the pair observed by the caller. This
		// prevents a stale secret-only request on one API node from being applied
		// after another node switches the client key/MID.
		if pairTouched && (!clientUpdated || !secretUpdated) {
			return fmt.Errorf("%s and %s must be updated together", pair.client, pair.secret)
		}
		if pairTouched && (finalClient == "") != (finalSecret == "") {
			return fmt.Errorf("%s and %s must both be configured or both be empty", pair.client, pair.secret)
		}
		if pairTouched && finalClient != "" {
			if err := validateTossKeyPairEnvironment(finalClient, finalSecret, pair.expected); err != nil {
				return fmt.Errorf("%s/%s: %w", pair.client, pair.secret, err)
			}
		}
	}

	testMode, err := strconv.ParseBool(final["TossTestMode"])
	if err != nil {
		return errors.New("invalid Toss test mode")
	}
	tossEnabled, err := strconv.ParseBool(final["TossEnabled"])
	if err != nil {
		return errors.New("invalid Toss enabled option")
	}
	billingEnabled, err := strconv.ParseBool(final["TossBillingEnabled"])
	if err != nil {
		return errors.New("invalid Toss billing enabled option")
	}
	walletAutoRechargeEnabled, err := strconv.ParseBool(final["TossWalletAutoRechargeEnabled"])
	if err != nil {
		return errors.New("invalid Toss wallet auto recharge enabled option")
	}
	if walletAutoRechargeEnabled && !billingEnabled {
		return errors.New("Toss wallet auto recharge requires Toss subscription billing to be enabled")
	}

	activeClient, activeSecret := "TossClientKey", "TossSecretKey"
	activeBillingClient, activeBillingSecret := "TossBillingClientKey", "TossBillingSecretKey"
	if testMode {
		activeClient, activeSecret = "TossTestClientKey", "TossTestSecretKey"
		activeBillingClient, activeBillingSecret = "TossBillingTestClientKey", "TossBillingTestSecretKey"
	}
	if tossEnabled && (final[activeClient] == "" || final[activeSecret] == "") {
		return errors.New("the active Toss client/secret key pair is incomplete")
	}
	activeEnvironment := "live"
	if testMode {
		activeEnvironment = "test"
	}
	if tossEnabled {
		if err := validateTossKeyPairEnvironment(final[activeClient], final[activeSecret], activeEnvironment); err != nil {
			return err
		}
	}
	if billingEnabled && (final[activeBillingClient] == "" || final[activeBillingSecret] == "") {
		return errors.New("the active Toss billing client/secret key pair is incomplete")
	}
	if billingEnabled {
		if err := validateTossKeyPairEnvironment(final[activeBillingClient], final[activeBillingSecret], activeEnvironment); err != nil {
			return err
		}
	}
	return nil
}

// UpdateTossOptions persists a Toss configuration change as one database
// transaction. Client/secret pairs and the selected test/live mode must not be
// partially saved: a mixed pair belongs to no valid Toss MID.
func UpdateTossOptions(c *gin.Context) {
	var request tossOptionsUpdateRequest
	if err := decodeTossPaymentRequestJSON(c, &request); err != nil {
		if errors.Is(err, common.ErrRequestBodyTooLarge) || common.IsRequestBodyTooLargeError(err) {
			respondTossPaymentRequestBodyTooLarge(c)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if request.RetryMaintenance {
		if request.RepairCompleteSet || len(request.Updates) != 0 || strings.TrimSpace(request.ExpectedRepairToken) != "" {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		err := model.CompleteTossConfigurationMaintenance()
		if err != nil && !errors.Is(err, model.ErrTossConfigMaintenanceRequired) {
			common.ApiError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "maintenance_pending": err != nil})
		return
	}
	if len(request.Updates) == 0 || len(request.Updates) > len(tossAtomicOptionKeys) ||
		(!request.RepairCompleteSet && strings.TrimSpace(request.ExpectedRepairToken) != "") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if request.RepairCompleteSet && (len(request.Updates) != len(tossAtomicOptionKeys) || strings.TrimSpace(request.ExpectedRepairToken) == "") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	values := make(map[string]string, len(request.Updates))
	for _, update := range request.Updates {
		if _, ok := tossAtomicOptionKeys[update.Key]; !ok {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		value, err := normalizeTossOptionUpdate(update)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		if _, duplicate := values[update.Key]; duplicate {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		values[update.Key] = value
	}
	var err error
	if request.RepairCompleteSet {
		err = model.RepairTossOptionsBulk(values, request.ExpectedRepairToken, validateTossOptionUpdateSet)
	} else {
		err = model.UpdateTossOptionsBulk(values, currentTossOptionValues(), validateTossOptionUpdateSet)
	}
	if err != nil {
		if errors.Is(err, model.ErrTossOptionValidation) {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		common.ApiError(c, err)
		return
	}
	maintenancePending := false
	if request.RepairCompleteSet {
		maintenanceErr := model.CompleteTossConfigurationMaintenance()
		maintenancePending = maintenanceErr != nil
		// The complete replacement already committed. Preserve its successful
		// response even if post-commit maintenance must be retried; the durable
		// gate remains visible in GetOptions and retry never accepts blank secrets.
		if maintenanceErr != nil && !errors.Is(maintenanceErr, model.ErrTossConfigMaintenanceRequired) &&
			!errors.Is(maintenanceErr, model.ErrTossConfigRevisionStale) {
			common.SysError("Toss configuration repair committed; maintenance retry required")
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "maintenance_pending": maintenancePending})
}

func UpdateOption(c *gin.Context) {
	var option OptionUpdateRequest
	err := common.DecodeJson(c.Request.Body, &option)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgInvalidParams),
		})
		return
	}
	if _, isTossOption := tossAtomicOptionKeys[option.Key]; isTossOption {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	option.Value = optionValueToString(option.Value)
	switch option.Key {
	case "QuotaForInviter", "QuotaForInvitee":
		if isPositiveOptionValue(option.Value.(string)) && !operation_setting.IsPaymentComplianceConfirmed() {
			common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
			return
		}
	default:
		if isPaymentComplianceOptionKey(option.Key) {
			common.ApiErrorI18n(c, i18n.MsgOptionComplianceFieldRestricted)
			return
		}
	}
	switch option.Key {
	case "GitHubOAuthEnabled":
		if option.Value == "true" && common.GitHubClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionGitHubConfigMissing),
			})
			return
		}
	case "discord.enabled":
		if option.Value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionDiscordConfigMissing),
			})
			return
		}
	case "oidc.enabled":
		if option.Value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionOIDCConfigMissing),
			})
			return
		}
	case "LinuxDOOAuthEnabled":
		if option.Value == "true" && common.LinuxDOClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionLinuxDOConfigMissing),
			})
			return
		}
	case "EmailDomainRestrictionEnabled":
		if option.Value == "true" && len(common.EmailDomainWhitelist) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionEmailDomainMissing),
			})
			return
		}
	case "WeChatAuthEnabled":
		if option.Value == "true" && common.WeChatServerAddress == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionWeChatConfigMissing),
			})
			return
		}
	case "TurnstileCheckEnabled":
		if option.Value == "true" && common.TurnstileSiteKey == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionTurnstileConfigMissing),
			})

			return
		}
	case "TelegramOAuthEnabled":
		if option.Value == "true" && common.TelegramBotToken == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionTelegramConfigMissing),
			})
			return
		}
	case "theme.frontend":
		if option.Value != "default" && option.Value != "classic" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionInvalidTheme),
			})
			return
		}
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionImageRatioFailed, map[string]any{"Error": err.Error()}),
			})
			return
		}
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionAudioRatioFailed, map[string]any{"Error": err.Error()}),
			})
			return
		}
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionAudioCompletionRatioFailed, map[string]any{"Error": err.Error()}),
			})
			return
		}
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgOptionCacheCreationRatioFailed, map[string]any{"Error": err.Error()}),
			})
			return
		}
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticDisableStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "ApiInfo")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "Announcements")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "FAQ")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "UptimeKumaGroups")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	err = model.UpdateOption(option.Key, option.Value.(string))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
