package model

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/performance_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

var ErrTossOptionValidation = errors.New("invalid Toss option update")
var ErrTossConfigRevisionStale = errors.New("Toss configuration changed on another node")
var ErrTossConfigStoredValuesInvalid = errors.New("stored Toss configuration values are invalid")

const tossOptionWriteLockKey = "__internal_toss_config_write_lock"

var tossOptionApplyMutex sync.Mutex

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Where(commonKeyCol+" NOT IN ?", []string{tossOptionWriteLockKey, tossConfigMaintenanceGateKey}).Find(&options).Error
	return options, err
}

func loadOptionsWithTossRevision() ([]*Option, string, error) {
	var options []*Option
	var revision string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		revision, err = lockTossOptionRowsTx(tx)
		if err != nil {
			return err
		}
		return tx.Where(commonKeyCol+" NOT IN ?", []string{tossOptionWriteLockKey, tossConfigMaintenanceGateKey}).Find(&options).Error
	})
	return options, revision, err
}

func tossOptionKeys() []string {
	defaults := defaultTossOptionValues()
	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func tossConfigSnapshotAttestation(snapshot setting.TossConfigSnapshot) string {
	// Store an HMAC rather than a plain semantic hash. This prevents the
	// revision marker from becoming an offline verifier for secret option
	// guesses while keeping the comparison deterministic across application
	// nodes that share the required persistent crypto/session secret.
	return common.GenerateHMAC("toss-config-v2:" + setting.TossConfigSnapshotDigest(snapshot))
}

// resolveTossOptionRows builds the complete fail-closed plaintext snapshot used
// both by the loader and by the provider POST preflight. The digest is computed
// only from parsed in-memory values, never from randomized ciphertext.
func resolveTossOptionRows(rows []*Option) (values map[string]string, legacySecrets map[string]string, digest string, hasRows bool, err error) {
	stored := defaultTossOptionValues()
	for _, option := range rows {
		if option == nil || !setting.IsTossOptionKey(option.Key) {
			continue
		}
		hasRows = true
		stored[option.Key] = option.Value
	}
	values, legacySecrets, err = decodeTossOptionValues(stored)
	if err != nil {
		return nil, nil, "", hasRows, err
	}
	setting.NormalizeLegacyTossOptionValues(values)
	snapshot, err := setting.ResolveTossConfigSnapshot(values)
	if err != nil {
		return nil, nil, "", hasRows, err
	}
	return values, legacySecrets, tossConfigSnapshotAttestation(snapshot), hasRows, nil
}

// attestLoadedTossOptions verifies that a database snapshot was committed by
// the atomic writer. An unbound legacy generation is never blessed from the
// rows currently present: a legacy writer may have stopped after changing only
// one half of a key pair. An operator must save Toss settings once through the
// atomic endpoint before provider POSTs can resume after a mixed-version roll.
func attestLoadedTossOptions(revision, loadedDigest string) (string, error) {
	var attestedRevision string
	err := DB.Transaction(func(tx *gorm.DB) error {
		currentRevision, err := lockTossOptionRowsTx(tx)
		if err != nil {
			return err
		}
		if currentRevision != strings.TrimSpace(revision) {
			return ErrTossConfigRevisionStale
		}
		var rows []*Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(commonKeyCol+" IN ?", tossOptionKeys()).
			Order(commonKeyCol + " asc").
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(tossOptionKeys()) {
			return ErrTossConfigRevisionStale
		}
		_, _, actualDigest, _, err := resolveTossOptionRows(rows)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrTossConfigStoredValuesInvalid, err)
		}
		if actualDigest != strings.ToLower(strings.TrimSpace(loadedDigest)) {
			return ErrTossConfigRevisionStale
		}
		if expectedDigest, ok := tossConfigRevisionDigest(currentRevision); ok {
			if expectedDigest != actualDigest {
				return ErrTossConfigRevisionStale
			}
			attestedRevision = currentRevision
			return nil
		}
		return ErrTossConfigRevisionStale
	})
	return attestedRevision, err
}

// readTossConfigRevisionAttestation returns a stable revision token and verifies
// its digest against the current database rows. The revision is read before and
// after the option scan so a concurrent atomic writer cannot produce a mixed
// observation. Any non-v2 or quarantine marker is rejected; only the atomic
// writer can publish an attested generation that authorizes provider POSTs.
func readTossConfigRevisionAttestation() (revision string, digest string, attested bool, err error) {
	revision, digest, attested, _, err = readTossConfigRevisionAttestationAndMaintenance()
	return
}

func readTossConfigRevisionAttestationAndMaintenance() (revision string, digest string, attested bool, maintenance bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		var before Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("value").Where(commonKeyCol+" = ?", tossOptionWriteLockKey).First(&before).Error; err != nil {
			return err
		}
		revision = strings.TrimSpace(before.Value)
		var gate Option
		gateErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("value").Where(commonKeyCol+" = ?", tossConfigMaintenanceGateKey).First(&gate).Error
		if gateErr == nil {
			maintenance = true
		} else if !errors.Is(gateErr, gorm.ErrRecordNotFound) {
			return gateErr
		}
		expectedDigest, ok := tossConfigRevisionDigest(revision)
		if !ok {
			return ErrTossConfigRevisionStale
		}
		var rows []*Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(commonKeyCol+" IN ?", tossOptionKeys()).
			Order(commonKeyCol + " asc").
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(tossOptionKeys()) {
			return ErrTossConfigRevisionStale
		}
		_, _, actualDigest, _, err := resolveTossOptionRows(rows)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrTossConfigStoredValuesInvalid, err)
		}
		var after Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("value").Where(commonKeyCol+" = ?", tossOptionWriteLockKey).First(&after).Error; err != nil {
			return err
		}
		if strings.TrimSpace(after.Value) != revision || actualDigest != expectedDigest {
			return ErrTossConfigRevisionStale
		}
		digest = actualDigest
		attested = true
		return nil
	})
	return revision, digest, attested, maintenance, err
}

func InitOptionMap() {
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)
	tossConfig := setting.GetTossConfigSnapshot()

	common.OptionMap["FileUploadPermission"] = strconv.Itoa(common.FileUploadPermission)
	common.OptionMap["FileDownloadPermission"] = strconv.Itoa(common.FileDownloadPermission)
	common.OptionMap["ImageUploadPermission"] = strconv.Itoa(common.ImageUploadPermission)
	common.OptionMap["ImageDownloadPermission"] = strconv.Itoa(common.ImageDownloadPermission)
	common.OptionMap["PasswordLoginEnabled"] = strconv.FormatBool(common.PasswordLoginEnabled)
	common.OptionMap["PasswordRegisterEnabled"] = strconv.FormatBool(common.PasswordRegisterEnabled)
	common.OptionMap["EmailVerificationEnabled"] = strconv.FormatBool(common.EmailVerificationEnabled)
	common.OptionMap["GitHubOAuthEnabled"] = strconv.FormatBool(common.GitHubOAuthEnabled)
	common.OptionMap["LinuxDOOAuthEnabled"] = strconv.FormatBool(common.LinuxDOOAuthEnabled)
	common.OptionMap["TelegramOAuthEnabled"] = strconv.FormatBool(common.TelegramOAuthEnabled)
	common.OptionMap["WeChatAuthEnabled"] = strconv.FormatBool(common.WeChatAuthEnabled)
	common.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(common.TurnstileCheckEnabled)
	common.OptionMap["RegisterEnabled"] = strconv.FormatBool(common.RegisterEnabled)
	common.OptionMap["AutomaticDisableChannelEnabled"] = strconv.FormatBool(common.AutomaticDisableChannelEnabled)
	common.OptionMap["AutomaticEnableChannelEnabled"] = strconv.FormatBool(common.AutomaticEnableChannelEnabled)
	common.OptionMap["LogConsumeEnabled"] = strconv.FormatBool(common.LogConsumeEnabled)
	common.OptionMap["DisplayInCurrencyEnabled"] = strconv.FormatBool(common.DisplayInCurrencyEnabled)
	common.OptionMap["DisplayTokenStatEnabled"] = strconv.FormatBool(common.DisplayTokenStatEnabled)
	common.OptionMap["DrawingEnabled"] = strconv.FormatBool(common.DrawingEnabled)
	common.OptionMap["TaskEnabled"] = strconv.FormatBool(common.TaskEnabled)
	common.OptionMap["DataExportEnabled"] = strconv.FormatBool(common.DataExportEnabled)
	common.OptionMap["ChannelDisableThreshold"] = strconv.FormatFloat(common.ChannelDisableThreshold, 'f', -1, 64)
	common.OptionMap["EmailDomainRestrictionEnabled"] = strconv.FormatBool(common.EmailDomainRestrictionEnabled)
	common.OptionMap["EmailAliasRestrictionEnabled"] = strconv.FormatBool(common.EmailAliasRestrictionEnabled)
	common.OptionMap["EmailDomainWhitelist"] = strings.Join(common.EmailDomainWhitelist, ",")
	common.OptionMap["SMTPServer"] = ""
	common.OptionMap["SMTPFrom"] = ""
	common.OptionMap["SMTPPort"] = strconv.Itoa(common.SMTPPort)
	common.OptionMap["SMTPAccount"] = ""
	common.OptionMap["SMTPToken"] = ""
	common.OptionMap["SMTPSSLEnabled"] = strconv.FormatBool(common.SMTPSSLEnabled)
	common.OptionMap["SMTPForceAuthLogin"] = strconv.FormatBool(common.SMTPForceAuthLogin)
	common.OptionMap["ContactEmail"] = ""
	common.OptionMap["Notice"] = ""
	common.OptionMap["About"] = ""
	common.OptionMap["HomePageContent"] = ""
	common.OptionMap["Footer"] = common.Footer
	common.OptionMap["SystemName"] = common.SystemName
	common.OptionMap["Logo"] = common.Logo
	common.OptionMap["ServerAddress"] = ""
	common.OptionMap["WorkerUrl"] = system_setting.WorkerUrl
	common.OptionMap["WorkerValidKey"] = system_setting.WorkerValidKey
	common.OptionMap["WorkerAllowHttpImageRequestEnabled"] = strconv.FormatBool(system_setting.WorkerAllowHttpImageRequestEnabled)
	common.OptionMap["PayAddress"] = ""
	common.OptionMap["CustomCallbackAddress"] = ""
	common.OptionMap["EpayId"] = ""
	common.OptionMap["EpayKey"] = ""
	common.OptionMap["Price"] = strconv.FormatFloat(operation_setting.Price, 'f', -1, 64)
	common.OptionMap["USDExchangeRate"] = strconv.FormatFloat(operation_setting.USDExchangeRate, 'f', -1, 64)
	common.OptionMap["MinTopUp"] = strconv.Itoa(operation_setting.MinTopUp)
	common.OptionMap["StripeMinTopUp"] = strconv.Itoa(setting.StripeMinTopUp)
	common.OptionMap["StripeApiSecret"] = setting.StripeApiSecret
	common.OptionMap["StripeWebhookSecret"] = setting.StripeWebhookSecret
	common.OptionMap["StripePriceId"] = setting.StripePriceId
	common.OptionMap["StripeUnitPrice"] = strconv.FormatFloat(setting.StripeUnitPrice, 'f', -1, 64)
	common.OptionMap["StripePromotionCodesEnabled"] = strconv.FormatBool(setting.StripePromotionCodesEnabled)
	common.OptionMap["PayPalClientId"] = setting.PayPalClientId
	common.OptionMap["PayPalClientSecret"] = setting.PayPalClientSecret
	common.OptionMap["PayPalWebhookID"] = setting.PayPalWebhookID
	common.OptionMap["PayPalSandbox"] = strconv.FormatBool(setting.PayPalSandbox)
	common.OptionMap["PayPalUnitPrice"] = strconv.FormatFloat(setting.PayPalUnitPrice, 'f', -1, 64)
	common.OptionMap["PayPalMinTopUp"] = strconv.Itoa(setting.PayPalMinTopUp)
	common.OptionMap["TossEnabled"] = strconv.FormatBool(tossConfig.Enabled)
	common.OptionMap["TossBillingEnabled"] = strconv.FormatBool(tossConfig.BillingEnabled)
	common.OptionMap["TossWalletAutoRechargeEnabled"] = strconv.FormatBool(tossConfig.WalletAutoRechargeEnabled)
	common.OptionMap["TossTestMode"] = strconv.FormatBool(tossConfig.TestMode)
	common.OptionMap["TossClientKey"] = tossConfig.ClientKey
	common.OptionMap["TossTestClientKey"] = tossConfig.TestClientKey
	common.OptionMap["TossBillingClientKey"] = tossConfig.BillingClientKey
	common.OptionMap["TossBillingTestClientKey"] = tossConfig.BillingTestClientKey
	common.OptionMap["TossUnitPrice"] = strconv.FormatFloat(tossConfig.UnitPrice, 'f', -1, 64)
	common.OptionMap["TossMinTopUp"] = strconv.Itoa(tossConfig.MinTopUp)
	common.OptionMap["CreemApiKey"] = setting.CreemApiKey
	common.OptionMap["CreemProducts"] = setting.CreemProducts
	common.OptionMap["CreemTestMode"] = strconv.FormatBool(setting.CreemTestMode)
	common.OptionMap["CreemWebhookSecret"] = setting.CreemWebhookSecret
	common.OptionMap["WaffoEnabled"] = strconv.FormatBool(setting.WaffoEnabled)
	common.OptionMap["WaffoApiKey"] = setting.WaffoApiKey
	common.OptionMap["WaffoPrivateKey"] = setting.WaffoPrivateKey
	common.OptionMap["WaffoPublicCert"] = setting.WaffoPublicCert
	common.OptionMap["WaffoSandboxPublicCert"] = setting.WaffoSandboxPublicCert
	common.OptionMap["WaffoSandboxApiKey"] = setting.WaffoSandboxApiKey
	common.OptionMap["WaffoSandboxPrivateKey"] = setting.WaffoSandboxPrivateKey
	common.OptionMap["WaffoSandbox"] = strconv.FormatBool(setting.WaffoSandbox)
	common.OptionMap["WaffoMerchantId"] = setting.WaffoMerchantId
	common.OptionMap["WaffoNotifyUrl"] = setting.WaffoNotifyUrl
	common.OptionMap["WaffoReturnUrl"] = setting.WaffoReturnUrl
	common.OptionMap["WaffoSubscriptionReturnUrl"] = setting.WaffoSubscriptionReturnUrl
	common.OptionMap["WaffoCurrency"] = setting.WaffoCurrency
	common.OptionMap["WaffoUnitPrice"] = strconv.FormatFloat(setting.WaffoUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoMinTopUp"] = strconv.Itoa(setting.WaffoMinTopUp)
	common.OptionMap["WaffoPayMethods"] = setting.WaffoPayMethods2JsonString()
	common.OptionMap["WaffoPancakeMerchantID"] = setting.WaffoPancakeMerchantID
	common.OptionMap["WaffoPancakePrivateKey"] = setting.WaffoPancakePrivateKey
	common.OptionMap["WaffoPancakeReturnURL"] = setting.WaffoPancakeReturnURL
	common.OptionMap["WaffoPancakeUnitPrice"] = strconv.FormatFloat(setting.WaffoPancakeUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoPancakeMinTopUp"] = strconv.Itoa(setting.WaffoPancakeMinTopUp)
	common.OptionMap["WaffoPancakeStoreID"] = setting.WaffoPancakeStoreID
	common.OptionMap["WaffoPancakeProductID"] = setting.WaffoPancakeProductID
	common.OptionMap["TopupGroupRatio"] = common.TopupGroupRatio2JSONString()
	common.OptionMap["Chats"] = setting.Chats2JsonString()
	common.OptionMap["AutoGroups"] = setting.AutoGroups2JsonString()
	common.OptionMap["DefaultUseAutoGroup"] = strconv.FormatBool(setting.DefaultUseAutoGroup)
	common.OptionMap["PayMethods"] = operation_setting.PayMethods2JsonString()
	common.OptionMap["GitHubClientId"] = ""
	common.OptionMap["GitHubClientSecret"] = ""
	common.OptionMap["TelegramBotToken"] = ""
	common.OptionMap["TelegramBotName"] = ""
	common.OptionMap["WeChatServerAddress"] = ""
	common.OptionMap["WeChatServerToken"] = ""
	common.OptionMap["WeChatAccountQRCodeImageURL"] = ""
	common.OptionMap["TurnstileSiteKey"] = ""
	common.OptionMap["TurnstileSecretKey"] = ""
	common.OptionMap["QuotaForNewUser"] = strconv.FormatInt(common.QuotaForNewUser, 10)
	common.OptionMap["QuotaForInviter"] = strconv.FormatInt(common.QuotaForInviter, 10)
	common.OptionMap["QuotaForInvitee"] = strconv.FormatInt(common.QuotaForInvitee, 10)
	common.OptionMap["QuotaRemindThreshold"] = strconv.FormatInt(common.QuotaRemindThreshold, 10)
	common.OptionMap["PreConsumedQuota"] = strconv.Itoa(common.PreConsumedQuota)
	common.OptionMap["ModelRequestRateLimitCount"] = strconv.Itoa(setting.ModelRequestRateLimitCount)
	common.OptionMap["ModelRequestRateLimitDurationMinutes"] = strconv.Itoa(setting.ModelRequestRateLimitDurationMinutes)
	common.OptionMap["ModelRequestRateLimitSuccessCount"] = strconv.Itoa(setting.ModelRequestRateLimitSuccessCount)
	common.OptionMap["ModelRequestRateLimitGroup"] = setting.ModelRequestRateLimitGroup2JSONString()
	common.OptionMap["ModelRatio"] = ratio_setting.ModelRatio2JSONString()
	common.OptionMap["ModelPrice"] = ratio_setting.ModelPrice2JSONString()
	common.OptionMap["CacheRatio"] = ratio_setting.CacheRatio2JSONString()
	common.OptionMap["CreateCacheRatio"] = ratio_setting.CreateCacheRatio2JSONString()
	common.OptionMap["GroupRatio"] = ratio_setting.GroupRatio2JSONString()
	common.OptionMap["GroupGroupRatio"] = ratio_setting.GroupGroupRatio2JSONString()
	common.OptionMap["ModelDiscount"] = ratio_setting.ModelDiscount2JSONString()
	common.OptionMap["VendorDiscount"] = ratio_setting.VendorDiscount2JSONString()
	common.OptionMap["UserUsableGroups"] = setting.UserUsableGroups2JSONString()
	common.OptionMap["CompletionRatio"] = ratio_setting.CompletionRatio2JSONString()
	common.OptionMap["ImageRatio"] = ratio_setting.ImageRatio2JSONString()
	common.OptionMap["AudioRatio"] = ratio_setting.AudioRatio2JSONString()
	common.OptionMap["AudioCompletionRatio"] = ratio_setting.AudioCompletionRatio2JSONString()
	common.OptionMap["TopUpLink"] = common.TopUpLink
	//common.OptionMap["ChatLink"] = common.ChatLink
	//common.OptionMap["ChatLink2"] = common.ChatLink2
	common.OptionMap["QuotaPerUnit"] = strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64)
	common.OptionMap["RetryTimes"] = strconv.Itoa(common.RetryTimes)
	common.OptionMap["DataExportInterval"] = strconv.Itoa(common.DataExportInterval)
	common.OptionMap["DataExportDefaultTime"] = common.DataExportDefaultTime
	common.OptionMap["DefaultCollapseSidebar"] = strconv.FormatBool(common.DefaultCollapseSidebar)
	common.OptionMap["MjNotifyEnabled"] = strconv.FormatBool(setting.MjNotifyEnabled)
	common.OptionMap["MjAccountFilterEnabled"] = strconv.FormatBool(setting.MjAccountFilterEnabled)
	common.OptionMap["MjModeClearEnabled"] = strconv.FormatBool(setting.MjModeClearEnabled)
	common.OptionMap["MjForwardUrlEnabled"] = strconv.FormatBool(setting.MjForwardUrlEnabled)
	common.OptionMap["MjActionCheckSuccessEnabled"] = strconv.FormatBool(setting.MjActionCheckSuccessEnabled)
	common.OptionMap["CheckSensitiveEnabled"] = strconv.FormatBool(setting.CheckSensitiveEnabled)
	common.OptionMap["DemoSiteEnabled"] = strconv.FormatBool(operation_setting.DemoSiteEnabled)
	common.OptionMap["SelfUseModeEnabled"] = strconv.FormatBool(operation_setting.SelfUseModeEnabled)
	common.OptionMap["ModelRequestRateLimitEnabled"] = strconv.FormatBool(setting.ModelRequestRateLimitEnabled)
	common.OptionMap["CheckSensitiveOnPromptEnabled"] = strconv.FormatBool(setting.CheckSensitiveOnPromptEnabled)
	common.OptionMap["StopOnSensitiveEnabled"] = strconv.FormatBool(setting.StopOnSensitiveEnabled)
	common.OptionMap["SensitiveWords"] = setting.SensitiveWordsToString()
	common.OptionMap["StreamCacheQueueLength"] = strconv.Itoa(setting.StreamCacheQueueLength)
	common.OptionMap["AutomaticDisableKeywords"] = operation_setting.AutomaticDisableKeywordsToString()
	common.OptionMap["AutomaticDisableStatusCodes"] = operation_setting.AutomaticDisableStatusCodesToString()
	common.OptionMap["AutomaticRetryStatusCodes"] = operation_setting.AutomaticRetryStatusCodesToString()
	common.OptionMap["ExposeRatioEnabled"] = strconv.FormatBool(ratio_setting.IsExposeRatioEnabled())

	modelConfigs := config.GlobalConfig.ExportAllConfigs()
	for k, v := range modelConfigs {
		common.OptionMap[k] = v
	}

	common.OptionMapRWMutex.Unlock()
	loadOptionsFromDatabase()
}

func loadOptionsFromDatabase() {
	tossOptionApplyMutex.Lock()
	defer tossOptionApplyMutex.Unlock()
	loadOptionsFromDatabaseLocked()
}

// loadOptionsFromDatabaseLocked applies one database snapshot while the caller
// holds tossOptionApplyMutex. Keeping the lock boundary separate lets provider
// preflight requests coalesce a revision-mismatch burst instead of serially
// repeating the same full option reload.
func loadOptionsFromDatabaseLocked() {
	options, revision, err := loadOptionsWithTossRevision()
	if err != nil {
		common.SysLog("failed to load options from database: " + err.Error())
		return
	}
	plainTossValues, legacySecrets, digest, _, tossErr := resolveTossOptionRows(options)
	if tossErr == nil {
		revision, tossErr = attestLoadedTossOptions(revision, digest)
	}
	if tossErr == nil && len(legacySecrets) > 0 && IsTossOptionSecretEncryptionEnabled() {
		tossErr = migrateLegacyTossOptionSecrets(legacySecrets)
	}
	if tossErr != nil {
		// Never publish rows changed outside the attested generation. In
		// particular, a legacy node may have written only one half of a key pair
		// without advancing the revision marker.
		failClosedTossOptionSecrets()
		common.SysLog("failed to load attested Toss options; Toss payments disabled: " + tossErr.Error())
	} else if err = setting.ApplyTossOptionValuesWithRevision(plainTossValues, revision); err != nil {
		failClosedTossOptionSecrets()
		_ = setting.ApplyTossOptionValuesWithRevision(nil, revision)
		common.SysLog("failed to update Toss option snapshot; Toss payments disabled: " + err.Error())
	} else {
		applyTossOptionMapValues(plainTossValues)
	}
	for _, option := range options {
		if setting.IsTossOptionKey(option.Key) || isInternalTossOptionKey(option.Key) {
			continue
		}
		err := updateOptionMap(option.Key, option.Value)
		if err != nil {
			common.SysLog("failed to update option map: " + err.Error())
		}
	}
}

// refreshTossOptionsAfterRevisionMismatch returns caughtUp=true only when a
// different goroutine already applied the exact authoritative revision that
// the caller observed. The authoritative row is read again after acquiring the
// reload mutex: if it changed while this request waited, the request remains
// stale even when the newer revision is already loaded.
func refreshTossOptionsAfterRevisionMismatch(observedRevision string) (snapshot setting.TossConfigSnapshot, caughtUp bool, err error) {
	observedRevision = strings.TrimSpace(observedRevision)
	if observedRevision == "" {
		return setting.TossConfigSnapshot{}, false, ErrTossConfigRevisionStale
	}
	tossOptionApplyMutex.Lock()
	defer tossOptionApplyMutex.Unlock()

	var lockRow Option
	if err := DB.Select("value").Where(commonKeyCol+" = ?", tossOptionWriteLockKey).First(&lockRow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return setting.TossConfigSnapshot{}, false, ErrTossConfigRevisionStale
		}
		return setting.TossConfigSnapshot{}, false, err
	}
	currentAuthoritativeRevision := strings.TrimSpace(lockRow.Value)
	if currentAuthoritativeRevision == "" {
		return setting.TossConfigSnapshot{}, false, ErrTossConfigRevisionStale
	}
	latest := setting.GetTossConfigSnapshot()
	if currentAuthoritativeRevision == observedRevision && tossSnapshotMatchesRevisionAttestation(latest, currentAuthoritativeRevision) {
		return latest, true, nil
	}
	// Either this goroutine is the first waiter for observedRevision, or the DB
	// revision changed again while it waited. Reload once under the shared mutex,
	// but keep this request stale; a later preflight must verify the applied
	// revision against the database before authorizing a provider POST.
	if latest.Revision != currentAuthoritativeRevision || !tossSnapshotMatchesRevisionAttestation(latest, currentAuthoritativeRevision) {
		loadOptionsFromDatabaseLocked()
	}
	return setting.GetTossConfigSnapshot(), false, nil
}

func tossSnapshotMatchesRevisionAttestation(snapshot setting.TossConfigSnapshot, revision string) bool {
	revision = strings.TrimSpace(revision)
	if strings.TrimSpace(snapshot.Revision) != revision {
		return false
	}
	expectedDigest, ok := tossConfigRevisionDigest(revision)
	if ok {
		return tossConfigSnapshotAttestation(snapshot) == expectedDigest
	}
	return revision == ""
}

// RequireFreshTossConfig fails closed when this node has not yet observed the
// latest atomic Toss option write. It is intentionally called immediately
// before provider-side create/confirm/charge requests; read-only recovery and
// revocation remain available while payments are disabled or rotating.
func RequireFreshTossConfig() error {
	_, err := GetFreshTossConfigSnapshot()
	return err
}

type tossProviderPOSTCapability int

const (
	tossProviderPOSTTopUp tossProviderPOSTCapability = iota
	tossProviderPOSTBilling
	tossProviderPOSTWalletAutoRecharge
)

type tossProviderPOSTBarrier struct {
	PolicyError error
}

// inspectTossProviderPOSTBarrierTx is the final in-transaction authorization
// barrier used immediately before an attempt marker is persisted. It locks the
// same option row as maintenance installation, verifies all 14 rows against the
// attested/runtime revision, and rejects a gate that appeared after an earlier
// request-level preflight.
func inspectTossProviderPOSTBarrierTx(tx *gorm.DB, capability tossProviderPOSTCapability) (tossProviderPOSTBarrier, error) {
	if tx == nil {
		return tossProviderPOSTBarrier{}, errors.New("database transaction is unavailable")
	}
	if DB == nil || !tx.Migrator().HasTable(&Option{}) {
		// Provider unit tests and pre-schema embedded callers have no durable gate
		// to race with. Production initialization always creates Option before any
		// route/task can authorize a provider POST.
		if strings.TrimSpace(setting.GetTossConfigSnapshot().Revision) != "" {
			return tossProviderPOSTBarrier{PolicyError: ErrTossConfigRevisionStale}, nil
		}
		snapshot := setting.GetTossConfigSnapshot()
		enabled := false
		switch capability {
		case tossProviderPOSTTopUp:
			enabled = snapshot.Enabled && operation_setting.IsPaymentComplianceConfirmed()
		case tossProviderPOSTBilling:
			enabled = snapshot.BillingEnabled && operation_setting.IsPaymentComplianceConfirmed()
		case tossProviderPOSTWalletAutoRecharge:
			enabled = snapshot.BillingEnabled && snapshot.WalletAutoRechargeEnabled && operation_setting.IsPaymentComplianceConfirmed()
		default:
			return tossProviderPOSTBarrier{}, errors.New("invalid Toss provider POST capability")
		}
		if !enabled {
			return tossProviderPOSTBarrier{PolicyError: ErrTossBillingOperationallyDisabled}, nil
		}
		return tossProviderPOSTBarrier{}, nil
	}
	revision, err := lockTossOptionRowsTx(tx)
	if err != nil {
		return tossProviderPOSTBarrier{}, err
	}
	maintenance, err := tossConfigMaintenanceRequiredTx(tx)
	if err != nil {
		return tossProviderPOSTBarrier{}, err
	}
	if maintenance {
		return tossProviderPOSTBarrier{PolicyError: ErrTossConfigMaintenanceRequired}, nil
	}
	if tx.Migrator().HasTable(&UserSubscription{}) {
		pendingRenewal, pendingErr := hasPendingLegacyTossRenewalContractsDB(tx)
		if pendingErr != nil {
			return tossProviderPOSTBarrier{}, pendingErr
		}
		if pendingRenewal {
			return tossProviderPOSTBarrier{PolicyError: ErrTossConfigMaintenanceRequired}, nil
		}
	}
	var rows []*Option
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(commonKeyCol+" IN ?", tossOptionKeys()).
		Order(commonKeyCol + " asc").Find(&rows).Error; err != nil {
		return tossProviderPOSTBarrier{}, err
	}
	if len(rows) != len(tossOptionKeys()) {
		return tossProviderPOSTBarrier{PolicyError: ErrTossConfigRevisionStale}, nil
	}
	values, _, actualDigest, _, err := resolveTossOptionRows(rows)
	if err != nil {
		return tossProviderPOSTBarrier{PolicyError: err}, nil
	}
	expectedDigest, ok := tossConfigRevisionDigest(revision)
	if !ok || expectedDigest != actualDigest {
		return tossProviderPOSTBarrier{PolicyError: ErrTossConfigRevisionStale}, nil
	}
	snapshot, err := setting.ResolveTossConfigSnapshot(values)
	if err != nil {
		return tossProviderPOSTBarrier{PolicyError: err}, nil
	}
	runtimeSnapshot := setting.GetTossConfigSnapshot()
	if runtimeSnapshot.Revision != revision || tossConfigSnapshotAttestation(runtimeSnapshot) != actualDigest {
		return tossProviderPOSTBarrier{PolicyError: ErrTossConfigRevisionStale}, nil
	}
	switch capability {
	case tossProviderPOSTTopUp:
		if !snapshot.Enabled || !operation_setting.IsPaymentComplianceConfirmed() {
			return tossProviderPOSTBarrier{PolicyError: ErrTossBillingOperationallyDisabled}, nil
		}
	case tossProviderPOSTBilling:
		if !snapshot.BillingEnabled || !operation_setting.IsPaymentComplianceConfirmed() {
			return tossProviderPOSTBarrier{PolicyError: ErrTossBillingOperationallyDisabled}, nil
		}
	case tossProviderPOSTWalletAutoRecharge:
		if !snapshot.BillingEnabled || !snapshot.WalletAutoRechargeEnabled || !operation_setting.IsPaymentComplianceConfirmed() {
			return tossProviderPOSTBarrier{PolicyError: ErrTossBillingOperationallyDisabled}, nil
		}
	default:
		return tossProviderPOSTBarrier{}, errors.New("invalid Toss provider POST capability")
	}
	return tossProviderPOSTBarrier{}, nil
}

func GetFreshTossConfigSnapshot() (setting.TossConfigSnapshot, error) {
	return getFreshTossConfigSnapshot(false)
}

func getFreshTossConfigSnapshot(ignoreMaintenance bool) (setting.TossConfigSnapshot, error) {
	snapshot := setting.GetTossConfigSnapshot()
	if DB == nil {
		return snapshot, nil
	}
	if !DB.Migrator().HasTable(&Option{}) {
		// A freshly constructed test/legacy process can legitimately have no
		// options table and no observed database generation. Once this node has
		// loaded a revision, however, HasTable=false can also mean a transient
		// metadata/connection failure. Never turn that failure into permission
		// to POST with a stale key pair.
		if strings.TrimSpace(snapshot.Revision) != "" {
			return setting.TossConfigSnapshot{}, ErrTossConfigRevisionStale
		}
		return snapshot, nil
	}
	authoritativeRevision, authoritativeDigest, attested, maintenance, err := readTossConfigRevisionAttestationAndMaintenance()
	if err == nil && maintenance && !ignoreMaintenance {
		return setting.TossConfigSnapshot{}, ErrTossConfigMaintenanceRequired
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Legacy installations without an atomic Toss write do not have a
		// revision yet. Once this process has observed a revision, however, a
		// missing row means the database was restored, truncated, or modified
		// behind the atomic writer. Treating that as legacy would authorize new
		// provider POSTs from a stale in-memory key pair.
		if strings.TrimSpace(snapshot.Revision) != "" {
			return setting.TossConfigSnapshot{}, ErrTossConfigRevisionStale
		}
		var tossRowCount int64
		if countErr := DB.Model(&Option{}).
			Where(commonKeyCol+" IN ?", tossOptionKeys()).
			Count(&tossRowCount).Error; countErr != nil {
			return setting.TossConfigSnapshot{}, countErr
		}
		if tossRowCount > 0 {
			// Persisted payment configuration without an attested generation may
			// be the result of a legacy node's partial key-pair write or a restore
			// that omitted the marker. It must be adopted through the atomic API.
			return setting.TossConfigSnapshot{}, ErrTossConfigRevisionStale
		}
		return snapshot, nil
	}
	if err != nil {
		return setting.TossConfigSnapshot{}, err
	}
	if authoritativeRevision == "" {
		return setting.TossConfigSnapshot{}, ErrTossConfigRevisionStale
	}
	if authoritativeRevision == snapshot.Revision && (!attested || tossConfigSnapshotAttestation(snapshot) == authoritativeDigest) {
		return snapshot, nil
	}
	latest, caughtUp, refreshErr := refreshTossOptionsAfterRevisionMismatch(authoritativeRevision)
	if refreshErr != nil {
		return setting.TossConfigSnapshot{}, refreshErr
	}
	if caughtUp {
		currentRevision, currentDigest, currentAttested, currentMaintenance, currentErr := readTossConfigRevisionAttestationAndMaintenance()
		if currentErr == nil && currentMaintenance && !ignoreMaintenance {
			return setting.TossConfigSnapshot{}, ErrTossConfigMaintenanceRequired
		}
		if currentErr == nil && currentRevision == latest.Revision &&
			(!currentAttested || tossConfigSnapshotAttestation(latest) == currentDigest) {
			return latest, nil
		}
		if currentErr != nil {
			return setting.TossConfigSnapshot{}, currentErr
		}
	}
	return setting.TossConfigSnapshot{}, ErrTossConfigRevisionStale
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing options from database")
		loadOptionsFromDatabase()
	}
}

func isTossCredentialNamespaceOption(key string) bool {
	switch key {
	case "TossTestMode",
		"TossClientKey", "TossSecretKey", "TossTestClientKey", "TossTestSecretKey",
		"TossBillingClientKey", "TossBillingSecretKey", "TossBillingTestClientKey", "TossBillingTestSecretKey":
		return true
	default:
		return false
	}
}

func containsTossCredentialNamespaceUpdate(values map[string]string) bool {
	for key := range values {
		if isTossCredentialNamespaceOption(key) {
			return true
		}
	}
	return false
}

func containsEveryTossOption(values map[string]string) bool {
	for _, key := range tossOptionKeys() {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

func isTossEmergencyDisableOnly(values map[string]string) bool {
	if len(values) == 0 {
		return false
	}
	for key, value := range values {
		switch key {
		case "TossEnabled", "TossBillingEnabled", "TossWalletAutoRechargeEnabled":
			if strings.TrimSpace(value) != "false" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

type TossOptionSetValidator func(current, updates map[string]string) error

type TossOptionUpdateMode struct {
	RepairCompleteSet   bool
	ExpectedRepairToken string
}

// UpdateTossOptionsBulk serializes every Toss configuration write through a
// database row lock. The validator runs after the lock and after re-reading the
// authoritative rows, so API nodes cannot validate against different stale
// in-memory snapshots and commit a mixed client/secret pair.
func UpdateTossOptionsBulk(values, _ map[string]string, validate TossOptionSetValidator) error {
	return updateTossOptionsBulk(values, validate, TossOptionUpdateMode{})
}

// RepairTossOptionsBulk is the only operation allowed to replace an
// unattested/quarantined Toss generation. The opaque token binds the complete
// all-disabled replacement to the exact generation inspected by the admin UI.
func RepairTossOptionsBulk(values map[string]string, expectedRepairToken string, validate TossOptionSetValidator) error {
	return updateTossOptionsBulk(values, validate, TossOptionUpdateMode{
		RepairCompleteSet:   true,
		ExpectedRepairToken: strings.TrimSpace(expectedRepairToken),
	})
}

func updateTossOptionsBulk(values map[string]string, validate TossOptionSetValidator, mode TossOptionUpdateMode) error {
	if len(values) == 0 {
		return nil
	}
	if mode.RepairCompleteSet {
		if strings.TrimSpace(mode.ExpectedRepairToken) == "" || len(values) != len(tossOptionKeys()) || !containsEveryTossOption(values) {
			return fmt.Errorf("%w: repair requires the complete Toss option set and expected token", ErrTossOptionValidation)
		}
		for _, key := range []string{"TossEnabled", "TossBillingEnabled", "TossWalletAutoRechargeEnabled"} {
			if strings.TrimSpace(values[key]) != "false" {
				return fmt.Errorf("%w: repair must keep every Toss payment mode disabled", ErrTossOptionValidation)
			}
		}
	}
	for _, pair := range [][2]string{
		{"TossClientKey", "TossSecretKey"},
		{"TossTestClientKey", "TossTestSecretKey"},
		{"TossBillingClientKey", "TossBillingSecretKey"},
		{"TossBillingTestClientKey", "TossBillingTestSecretKey"},
	} {
		_, clientUpdated := values[pair[0]]
		_, secretUpdated := values[pair[1]]
		if clientUpdated != secretUpdated {
			return fmt.Errorf("%w: %s and %s must be updated together", ErrTossOptionValidation, pair[0], pair[1])
		}
	}
	if DB == nil {
		return errors.New("database is unavailable")
	}
	if err := requireTossOptionSecretWriteSafety(values); err != nil {
		if errors.Is(err, ErrTossBillingCryptoSecretNotPersistent) {
			failClosedTossOptionSecrets()
		}
		return err
	}
	// Database rows, not a potentially stale caller snapshot, are authoritative.
	// Seed missing rows only with fail-closed defaults (or the explicit values in
	// this request). Otherwise an unrelated settings write could resurrect a key
	// that another node deleted during a restore or emergency revocation.
	defaults := defaultTossOptionValues()
	for key, value := range values {
		if !setting.IsTossOptionKey(key) {
			return fmt.Errorf("%w: unsupported option %s", ErrTossOptionValidation, key)
		}
		// The explicit request value is also the correct seed when the row does
		// not exist yet. Existing rows win through ON CONFLICT DO NOTHING and are
		// re-read under the shared write lock below.
		defaults[key] = value
	}

	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tossMIDTables := tossLegacyMIDBackfillTables{
		billingKeys:        DB.Migrator().HasTable(&UserBillingKey{}),
		topUps:             DB.Migrator().HasTable(&TopUp{}),
		subscriptionOrders: DB.Migrator().HasTable(&SubscriptionOrder{}),
		walletPolicies:     DB.Migrator().HasTable(&WalletAutoRecharge{}),
	}
	hasUserSubscriptions := DB.Migrator().HasTable(&UserSubscription{})
	expectedRevision := ""
	if containsTossCredentialNamespaceUpdate(values) && !mode.RepairCompleteSet {
		preRotationSnapshot, err := GetFreshTossConfigSnapshot()
		if err != nil {
			return err
		}
		expectedRevision = strings.TrimSpace(preRotationSnapshot.Revision)
		if expectedRevision == "" {
			return ErrTossConfigRevisionStale
		}
		if _, err := backfillLegacyTossProviderMIDFingerprintsBatched(preRotationSnapshot, expectedRevision, tossMIDTables); err != nil {
			return err
		}
	}
	tossOptionApplyMutex.Lock()
	defer tossOptionApplyMutex.Unlock()
	finalValues := make(map[string]string, len(defaults))
	committedRevision := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Updating a dedicated row acquires the same write/row lock on PostgreSQL,
		// MySQL, and SQLite. Every writer performs it before reading configuration.
		var err error
		previousRevision, generation, err := lockTossOptionWritesTx(tx)
		if err != nil {
			return err
		}
		if mode.RepairCompleteSet {
			if tossConfigRepairToken(previousRevision) != mode.ExpectedRepairToken {
				return ErrTossConfigRevisionStale
			}
		} else if expectedRevision != "" && previousRevision != expectedRevision {
			return ErrTossConfigRevisionStale
		}
		maintenanceRequired, err := tossConfigMaintenanceRequiredTx(tx)
		if err != nil {
			return err
		}
		pendingRenewalMaintenance := false
		if hasUserSubscriptions {
			pendingRenewalMaintenance, err = hasPendingLegacyTossRenewalContractsDB(tx)
			if err != nil {
				return err
			}
		}
		maintenanceActive := maintenanceRequired || pendingRenewalMaintenance
		if maintenanceActive && !mode.RepairCompleteSet && !isTossEmergencyDisableOnly(values) {
			return ErrTossConfigMaintenanceRequired
		}
		emergencyDisable := maintenanceActive && isTossEmergencyDisableOnly(values)
		committedRevision = generation
		var priorRows []*Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(commonKeyCol+" IN ?", keys).
			Order(commonKeyCol + " asc").
			Find(&priorRows).Error; err != nil {
			return err
		}
		_, _, priorDigest, _, resolveErr := resolveTossOptionRows(priorRows)
		expectedPriorDigest, hasPriorAttestation := tossConfigRevisionDigest(previousRevision)
		priorAttested := resolveErr == nil && len(priorRows) == len(keys) && hasPriorAttestation && expectedPriorDigest == priorDigest
		if mode.RepairCompleteSet {
			if priorAttested {
				return ErrTossConfigRepairNotRequired
			}
			// A repair is deliberately able to replace malformed/corrupt encrypted
			// legacy rows. It never decrypts or preserves them; the submitted exact
			// 14-key all-disabled set becomes the new authoritative snapshot.
		} else {
			if resolveErr != nil {
				return resolveErr
			}
			if !priorAttested {
				return ErrTossConfigRevisionStale
			}
		}

		seed := make([]Option, 0, len(keys))
		for _, key := range keys {
			value := defaults[key]
			if isTossSecretOption(key) && strings.TrimSpace(value) != "" {
				// Do not materialize a plaintext process fallback in the database
				// before the explicit rolling-upgrade gate is enabled.
				if !IsTossOptionSecretEncryptionEnabled() {
					continue
				}
				var err error
				value, err = encryptTossOptionSecret(key, value)
				if err != nil {
					return err
				}
			}
			seed = append(seed, Option{Key: key, Value: value})
		}
		if len(seed) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
				return err
			}
		}

		var rows []Option
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(commonKeyCol+" IN ?", keys).
			Order(commonKeyCol + " asc").
			Find(&rows).Error; err != nil {
			return err
		}
		storedValues := make(map[string]string, len(rows))
		for i := range rows {
			storedValues[rows[i].Key] = rows[i].Value
		}
		current := make(map[string]string, len(defaults))
		for key, value := range defaultTossOptionValues() {
			current[key] = value
		}
		if !mode.RepairCompleteSet {
			decodedValues, legacySecrets, decodeErr := decodeTossOptionValues(storedValues)
			if decodeErr != nil {
				return decodeErr
			}
			for key, value := range decodedValues {
				current[key] = value
			}
			if len(legacySecrets) > 0 && IsTossOptionSecretEncryptionEnabled() {
				for key, plaintext := range legacySecrets {
					encrypted, encryptErr := encryptTossOptionSecret(key, plaintext)
					if encryptErr != nil {
						return encryptErr
					}
					result := tx.Model(&Option{}).
						Where(commonKeyCol+" = ?", key).
						Where(tossOptionExactValuePredicate(), plaintext).
						UpdateColumn("value", encrypted)
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected != 1 {
						return fmt.Errorf("%w: Toss option changed while encrypting %s", ErrTossOptionValidation, key)
					}
				}
			}
			if setting.NormalizeLegacyTossOptionValues(current) {
				if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", "TossMinTopUp").Update("value", current["TossMinTopUp"]).Error; err != nil {
					return err
				}
			}
		}
		if validate != nil {
			if err := validate(current, values); err != nil {
				return fmt.Errorf("%w: %v", ErrTossOptionValidation, err)
			}
		}
		for key, value := range values {
			storedValue := value
			if isTossSecretOption(key) && strings.TrimSpace(value) != "" {
				storedValue, err = encryptTossOptionSecret(key, value)
				if err != nil {
					return err
				}
			}
			row := Option{Key: key, Value: storedValue}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value"}),
			}).Create(&row).Error; err != nil {
				return err
			}
			current[key] = value
		}
		finalSnapshot, err := setting.ResolveTossConfigSnapshot(current)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrTossOptionValidation, err)
		}
		if finalSnapshot.Enabled || finalSnapshot.BillingEnabled || finalSnapshot.WalletAutoRechargeEnabled {
			if err := ValidateTossBillingCryptoConfiguration(); err != nil {
				return err
			}
			if !emergencyDisable {
				maintenanceRequiredNow, err := tossConfigMaintenanceRequiredTx(tx)
				if err != nil {
					return err
				}
				pendingRenewal := false
				if hasUserSubscriptions {
					pendingRenewal, err = hasPendingLegacyTossRenewalContractsDB(tx)
					if err != nil {
						return err
					}
				}
				if maintenanceRequiredNow || pendingRenewal {
					return ErrTossConfigMaintenanceRequired
				}
			}
		}
		committedRevision, err = finalizeTossOptionWritesTx(
			tx,
			committedRevision,
			tossConfigSnapshotAttestation(finalSnapshot),
			false,
		)
		if err != nil {
			return err
		}
		if mode.RepairCompleteSet || maintenanceActive {
			if err := setTossConfigMaintenanceRequiredTx(tx, committedRevision); err != nil {
				return err
			}
		}
		for key, value := range current {
			finalValues[key] = value
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrTossOptionSecretDecrypt) || errors.Is(err, ErrTossBillingCryptoSecretNotPersistent) {
			failClosedTossOptionSecrets()
		}
		return err
	}
	if err := setting.ApplyTossOptionValuesWithRevision(finalValues, committedRevision); err != nil {
		failClosedTossOptionSecrets()
		return err
	}
	applyTossOptionMapValues(finalValues)
	return nil
}

// CompleteTossConfigurationMaintenance resumes an interrupted post-repair
// migration without accepting credentials from the caller. Every batch is
// bound to the attested option revision; the internal fence is cleared only
// after renewal contracts are frozen and the bounded MID pass completes.
// Unresolvable historical rows remain safely exact-credential-only rather than
// permanently bricking rotations or re-enablement.
func CompleteTossConfigurationMaintenance() error {
	if DB == nil || !DB.Migrator().HasTable(&Option{}) {
		return errors.New("database is unavailable")
	}
	snapshot, err := getFreshTossConfigSnapshot(true)
	if err != nil {
		return err
	}
	expectedRevision := strings.TrimSpace(snapshot.Revision)
	if expectedRevision == "" {
		return ErrTossConfigRevisionStale
	}
	if err := ensureTossConfigurationMaintenanceGate(expectedRevision); err != nil {
		return err
	}
	tables := tossLegacyMIDBackfillTables{
		billingKeys:        DB.Migrator().HasTable(&UserBillingKey{}),
		topUps:             DB.Migrator().HasTable(&TopUp{}),
		subscriptionOrders: DB.Migrator().HasTable(&SubscriptionOrder{}),
		walletPolicies:     DB.Migrator().HasTable(&WalletAutoRecharge{}),
	}
	if _, err := backfillLegacyTossProviderMIDFingerprintsBatched(snapshot, expectedRevision, tables); err != nil {
		return err
	}
	if DB.Migrator().HasTable(&UserSubscription{}) && DB.Migrator().HasTable(&SubscriptionPlan{}) && DB.Migrator().HasTable(&UserBillingKey{}) {
		if _, _, err := backfillLegacyTossRenewalContractsPinned(snapshot, expectedRevision); err != nil {
			return err
		}
	}
	pendingRenewal, err := HasPendingLegacyTossRenewalContracts()
	if err != nil {
		return err
	}
	if pendingRenewal {
		return ErrTossConfigMaintenanceRequired
	}
	return clearTossConfigurationMaintenanceGate(expectedRevision)
}

func ensureTossConfigurationMaintenanceGate(expectedRevision string) error {
	expectedRevision = strings.TrimSpace(expectedRevision)
	if _, ok := tossConfigRevisionDigest(expectedRevision); !ok {
		return ErrTossConfigRevisionStale
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		currentRevision, err := lockTossOptionRowsTx(tx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(currentRevision) != expectedRevision {
			return ErrTossConfigRevisionStale
		}
		var gate Option
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(commonKeyCol+" = ?", tossConfigMaintenanceGateKey).First(&gate).Error
		if err == nil && strings.TrimSpace(gate.Value) != expectedRevision {
			return ErrTossConfigRevisionStale
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return setTossConfigMaintenanceRequiredTx(tx, expectedRevision)
	})
}

func clearTossConfigurationMaintenanceGate(expectedRevision string) error {
	expectedRevision = strings.TrimSpace(expectedRevision)
	hasUserSubscriptions := DB != nil && DB.Migrator().HasTable(&UserSubscription{})
	return DB.Transaction(func(tx *gorm.DB) error {
		currentRevision, err := lockTossOptionRowsTx(tx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(currentRevision) != expectedRevision {
			return ErrTossConfigRevisionStale
		}
		if hasUserSubscriptions {
			pending, err := hasPendingLegacyTossRenewalContractsDB(tx)
			if err != nil {
				return err
			}
			if pending {
				return ErrTossConfigMaintenanceRequired
			}
		}
		result := tx.Where(commonKeyCol+" = ? AND value = ?", tossConfigMaintenanceGateKey, expectedRevision).Delete(&Option{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", tossConfigMaintenanceGateKey).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrTossConfigRevisionStale
			}
		}
		return nil
	})
}

func UpdateOption(key string, value string) error {
	if setting.IsTossOptionKey(key) || isInternalTossOptionKey(key) {
		return fmt.Errorf("%w: use the atomic Toss option updater", ErrTossOptionValidation)
	}
	// Save to database first
	option := Option{
		Key: key,
	}
	// https://gorm.io/docs/update.html#Save-All-Fields
	if err := DB.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
		return err
	}
	option.Value = value
	// Save is a combination function.
	// If save value does not contain primary key, it will execute Create,
	// otherwise it will execute Update (with all fields).
	if err := DB.Save(&option).Error; err != nil {
		return err
	}
	// Update OptionMap
	return updateOptionMap(key, value)
}

// UpdateOptionsBulk persists multiple key/value pairs in a single database
// transaction, then dispatches them through updateOptionMap in one pass. If
// any DB write fails the whole transaction rolls back and no in-memory state
// is touched — safe for callers that must commit a set of related options
// atomically (e.g. payment gateway binding).
func UpdateOptionsBulk(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	for key := range values {
		if setting.IsTossOptionKey(key) || isInternalTossOptionKey(key) {
			return fmt.Errorf("%w: use the atomic Toss option updater", ErrTossOptionValidation)
		}
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		for k, v := range values {
			option := Option{Key: k}
			if err := tx.FirstOrCreate(&option, Option{Key: k}).Error; err != nil {
				return err
			}
			option.Value = v
			if err := tx.Save(&option).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for k, v := range values {
		if err := updateOptionMap(k, v); err != nil {
			return err
		}
	}
	return nil
}

func updateOptionMap(key string, value string) (err error) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	common.OptionMap[key] = value

	if handleConfigUpdate(key, value) {
		return nil
	}

	// ...
	if strings.HasSuffix(key, "Permission") {
		intValue, _ := strconv.Atoi(value)
		switch key {
		case "FileUploadPermission":
			common.FileUploadPermission = intValue
		case "FileDownloadPermission":
			common.FileDownloadPermission = intValue
		case "ImageUploadPermission":
			common.ImageUploadPermission = intValue
		case "ImageDownloadPermission":
			common.ImageDownloadPermission = intValue
		}
	}
	if strings.HasSuffix(key, "Enabled") || key == "DefaultCollapseSidebar" || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" {
		boolValue := value == "true"
		switch key {
		case "PasswordRegisterEnabled":
			common.PasswordRegisterEnabled = boolValue
		case "PasswordLoginEnabled":
			common.PasswordLoginEnabled = boolValue
		case "EmailVerificationEnabled":
			common.EmailVerificationEnabled = boolValue
		case "GitHubOAuthEnabled":
			common.GitHubOAuthEnabled = boolValue
		case "LinuxDOOAuthEnabled":
			common.LinuxDOOAuthEnabled = boolValue
		case "WeChatAuthEnabled":
			common.WeChatAuthEnabled = boolValue
		case "TelegramOAuthEnabled":
			common.TelegramOAuthEnabled = boolValue
		case "TurnstileCheckEnabled":
			common.TurnstileCheckEnabled = boolValue
		case "RegisterEnabled":
			common.RegisterEnabled = boolValue
		case "EmailDomainRestrictionEnabled":
			common.EmailDomainRestrictionEnabled = boolValue
		case "EmailAliasRestrictionEnabled":
			common.EmailAliasRestrictionEnabled = boolValue
		case "AutomaticDisableChannelEnabled":
			common.AutomaticDisableChannelEnabled = boolValue
		case "AutomaticEnableChannelEnabled":
			common.AutomaticEnableChannelEnabled = boolValue
		case "LogConsumeEnabled":
			common.LogConsumeEnabled = boolValue
		case "DisplayInCurrencyEnabled":
			// general_setting.quota_display_type
			// true -> USD, false -> TOKENS
			newVal := "USD"
			if !boolValue {
				newVal = "TOKENS"
			}
			if cfg := config.GlobalConfig.Get("general_setting"); cfg != nil {
				_ = config.UpdateConfigFromMap(cfg, map[string]string{"quota_display_type": newVal})
			}
		case "DisplayTokenStatEnabled":
			common.DisplayTokenStatEnabled = boolValue
		case "DrawingEnabled":
			common.DrawingEnabled = boolValue
		case "TaskEnabled":
			common.TaskEnabled = boolValue
		case "DataExportEnabled":
			common.DataExportEnabled = boolValue
		case "DefaultCollapseSidebar":
			common.DefaultCollapseSidebar = boolValue
		case "MjNotifyEnabled":
			setting.MjNotifyEnabled = boolValue
		case "MjAccountFilterEnabled":
			setting.MjAccountFilterEnabled = boolValue
		case "MjModeClearEnabled":
			setting.MjModeClearEnabled = boolValue
		case "MjForwardUrlEnabled":
			setting.MjForwardUrlEnabled = boolValue
		case "MjActionCheckSuccessEnabled":
			setting.MjActionCheckSuccessEnabled = boolValue
		case "CheckSensitiveEnabled":
			setting.CheckSensitiveEnabled = boolValue
		case "DemoSiteEnabled":
			operation_setting.DemoSiteEnabled = boolValue
		case "SelfUseModeEnabled":
			operation_setting.SelfUseModeEnabled = boolValue
		case "CheckSensitiveOnPromptEnabled":
			setting.CheckSensitiveOnPromptEnabled = boolValue
		case "ModelRequestRateLimitEnabled":
			setting.ModelRequestRateLimitEnabled = boolValue
		case "StopOnSensitiveEnabled":
			setting.StopOnSensitiveEnabled = boolValue
		case "SMTPSSLEnabled":
			common.SMTPSSLEnabled = boolValue
		case "SMTPForceAuthLogin":
			common.SMTPForceAuthLogin = boolValue
		case "WorkerAllowHttpImageRequestEnabled":
			system_setting.WorkerAllowHttpImageRequestEnabled = boolValue
		case "DefaultUseAutoGroup":
			setting.DefaultUseAutoGroup = boolValue
		case "ExposeRatioEnabled":
			ratio_setting.SetExposeRatioEnabled(boolValue)
		}
	}
	switch key {
	case "EmailDomainWhitelist":
		common.EmailDomainWhitelist = strings.Split(value, ",")
	case "SMTPServer":
		common.SMTPServer = value
	case "SMTPPort":
		intValue, _ := strconv.Atoi(value)
		common.SMTPPort = intValue
	case "SMTPAccount":
		common.SMTPAccount = value
	case "SMTPFrom":
		common.SMTPFrom = value
	case "SMTPToken":
		common.SMTPToken = value
	case "ContactEmail":
		common.ContactEmail = value
	case "ServerAddress":
		system_setting.ServerAddress = value
	case "WorkerUrl":
		system_setting.WorkerUrl = value
	case "WorkerValidKey":
		system_setting.WorkerValidKey = value
	case "PayAddress":
		operation_setting.PayAddress = value
	case "Chats":
		err = setting.UpdateChatsByJsonString(value)
	case "AutoGroups":
		err = setting.UpdateAutoGroupsByJsonString(value)
	case "CustomCallbackAddress":
		operation_setting.CustomCallbackAddress = value
	case "EpayId":
		operation_setting.EpayId = value
	case "EpayKey":
		operation_setting.EpayKey = value
	case "Price":
		operation_setting.Price, _ = strconv.ParseFloat(value, 64)
	case "USDExchangeRate":
		operation_setting.USDExchangeRate, _ = strconv.ParseFloat(value, 64)
	case "MinTopUp":
		operation_setting.MinTopUp, _ = strconv.Atoi(value)
	case "StripeApiSecret":
		setting.StripeApiSecret = value
	case "StripeWebhookSecret":
		setting.StripeWebhookSecret = value
	case "StripePriceId":
		setting.StripePriceId = value
	case "StripeUnitPrice":
		setting.StripeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "StripeMinTopUp":
		setting.StripeMinTopUp, _ = strconv.Atoi(value)
	case "StripePromotionCodesEnabled":
		setting.StripePromotionCodesEnabled = value == "true"
	case "PayPalClientId":
		setting.PayPalClientId = value
	case "PayPalClientSecret":
		setting.PayPalClientSecret = value
	case "PayPalWebhookID":
		setting.PayPalWebhookID = value
	case "PayPalSandbox":
		setting.PayPalSandbox = value == "true"
	case "PayPalUnitPrice":
		setting.PayPalUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "PayPalMinTopUp":
		setting.PayPalMinTopUp, _ = strconv.Atoi(value)
	case "TossEnabled", "TossBillingEnabled", "TossWalletAutoRechargeEnabled", "TossTestMode",
		"TossClientKey", "TossSecretKey", "TossTestClientKey", "TossTestSecretKey",
		"TossBillingClientKey", "TossBillingSecretKey", "TossBillingTestClientKey", "TossBillingTestSecretKey",
		"TossUnitPrice", "TossMinTopUp":
		return setting.ApplyTossOptionValues(map[string]string{key: value})
	case "CreemApiKey":
		setting.CreemApiKey = value
	case "CreemProducts":
		setting.CreemProducts = value
	case "CreemTestMode":
		setting.CreemTestMode = value == "true"
	case "CreemWebhookSecret":
		setting.CreemWebhookSecret = value
	case "WaffoEnabled":
		setting.WaffoEnabled = value == "true"
	case "WaffoApiKey":
		setting.WaffoApiKey = value
	case "WaffoPrivateKey":
		setting.WaffoPrivateKey = value
	case "WaffoPublicCert":
		setting.WaffoPublicCert = value
	case "WaffoSandboxPublicCert":
		setting.WaffoSandboxPublicCert = value
	case "WaffoSandboxApiKey":
		setting.WaffoSandboxApiKey = value
	case "WaffoSandboxPrivateKey":
		setting.WaffoSandboxPrivateKey = value
	case "WaffoSandbox":
		setting.WaffoSandbox = value == "true"
	case "WaffoMerchantId":
		setting.WaffoMerchantId = value
	case "WaffoNotifyUrl":
		setting.WaffoNotifyUrl = value
	case "WaffoReturnUrl":
		setting.WaffoReturnUrl = value
	case "WaffoSubscriptionReturnUrl":
		setting.WaffoSubscriptionReturnUrl = value
	case "WaffoCurrency":
		setting.WaffoCurrency = value
	case "WaffoUnitPrice":
		setting.WaffoUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoMinTopUp":
		setting.WaffoMinTopUp, _ = strconv.Atoi(value)
	case "WaffoPancakeMerchantID":
		setting.WaffoPancakeMerchantID = value
	case "WaffoPancakePrivateKey":
		setting.WaffoPancakePrivateKey = value
	case "WaffoPancakeReturnURL":
		setting.WaffoPancakeReturnURL = value
	case "WaffoPancakeStoreID":
		setting.WaffoPancakeStoreID = value
	case "WaffoPancakeProductID":
		setting.WaffoPancakeProductID = value
	case "WaffoPancakeUnitPrice":
		setting.WaffoPancakeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoPancakeMinTopUp":
		setting.WaffoPancakeMinTopUp, _ = strconv.Atoi(value)
	case "TopupGroupRatio":
		err = common.UpdateTopupGroupRatioByJSONString(value)
	case "GitHubClientId":
		common.GitHubClientId = value
	case "GitHubClientSecret":
		common.GitHubClientSecret = value
	case "LinuxDOClientId":
		common.LinuxDOClientId = value
	case "LinuxDOClientSecret":
		common.LinuxDOClientSecret = value
	case "LinuxDOMinimumTrustLevel":
		common.LinuxDOMinimumTrustLevel, _ = strconv.Atoi(value)
	case "Footer":
		common.Footer = value
	case "SystemName":
		common.SystemName = value
	case "Logo":
		common.Logo = value
	case "WeChatServerAddress":
		common.WeChatServerAddress = value
	case "WeChatServerToken":
		common.WeChatServerToken = value
	case "WeChatAccountQRCodeImageURL":
		common.WeChatAccountQRCodeImageURL = value
	case "TelegramBotToken":
		common.TelegramBotToken = value
	case "TelegramBotName":
		common.TelegramBotName = value
	case "TurnstileSiteKey":
		common.TurnstileSiteKey = value
	case "TurnstileSecretKey":
		common.TurnstileSecretKey = value
	case "QuotaForNewUser":
		common.QuotaForNewUser, _ = strconv.ParseInt(value, 10, 64)
	case "QuotaForInviter":
		common.QuotaForInviter, _ = strconv.ParseInt(value, 10, 64)
	case "QuotaForInvitee":
		common.QuotaForInvitee, _ = strconv.ParseInt(value, 10, 64)
	case "QuotaRemindThreshold":
		common.QuotaRemindThreshold, _ = strconv.ParseInt(value, 10, 64)
	case "PreConsumedQuota":
		common.PreConsumedQuota, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitCount":
		setting.ModelRequestRateLimitCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitDurationMinutes":
		setting.ModelRequestRateLimitDurationMinutes, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitSuccessCount":
		setting.ModelRequestRateLimitSuccessCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitGroup":
		err = setting.UpdateModelRequestRateLimitGroupByJSONString(value)
	case "RetryTimes":
		common.RetryTimes, _ = strconv.Atoi(value)
	case "DataExportInterval":
		common.DataExportInterval, _ = strconv.Atoi(value)
	case "DataExportDefaultTime":
		common.DataExportDefaultTime = value
	case "ModelRatio":
		err = ratio_setting.UpdateModelRatioByJSONString(value)
	case "GroupRatio":
		err = ratio_setting.UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		err = ratio_setting.UpdateGroupGroupRatioByJSONString(value)
	case "ModelDiscount":
		err = ratio_setting.UpdateModelDiscountByJSONString(value)
		if err == nil {
			InvalidatePricingCache()
		}
	case "VendorDiscount":
		err = ratio_setting.UpdateVendorDiscountByJSONString(value)
		if err == nil {
			InvalidatePricingCache()
		}
	case "UserUsableGroups":
		err = setting.UpdateUserUsableGroupsByJSONString(value)
	case "CompletionRatio":
		err = ratio_setting.UpdateCompletionRatioByJSONString(value)
	case "ModelPrice":
		err = ratio_setting.UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		err = ratio_setting.UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(value)
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(value)
	case "TopUpLink":
		common.TopUpLink = value
	//case "ChatLink":
	//	common.ChatLink = value
	//case "ChatLink2":
	//	common.ChatLink2 = value
	case "ChannelDisableThreshold":
		common.ChannelDisableThreshold, _ = strconv.ParseFloat(value, 64)
	case "QuotaPerUnit":
		common.QuotaPerUnit, _ = strconv.ParseFloat(value, 64)
	case "SensitiveWords":
		setting.SensitiveWordsFromString(value)
	case "AutomaticDisableKeywords":
		operation_setting.AutomaticDisableKeywordsFromString(value)
	case "AutomaticDisableStatusCodes":
		err = operation_setting.AutomaticDisableStatusCodesFromString(value)
	case "AutomaticRetryStatusCodes":
		err = operation_setting.AutomaticRetryStatusCodesFromString(value)
	case "StreamCacheQueueLength":
		setting.StreamCacheQueueLength, _ = strconv.Atoi(value)
	case "PayMethods":
		err = operation_setting.UpdatePayMethodsByJsonString(value)
	case "WaffoPayMethods":
		// WaffoPayMethods is read directly from OptionMap via setting.GetWaffoPayMethods().
		// The value is already stored in OptionMap at the top of this function (line: common.OptionMap[key] = value).
		// No additional in-memory variable to update.
	}
	return err
}

// handleConfigUpdate
func handleConfigUpdate(key, value string) bool {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return false
	}

	configName := parts[0]
	configKey := parts[1]

	cfg := config.GlobalConfig.Get(configName)
	if cfg == nil {
		return false
	}

	configMap := map[string]string{
		configKey: value,
	}
	config.UpdateConfigFromMap(cfg, configMap)

	if configName == "performance_setting" {
		performance_setting.UpdateAndSync()
	} else if configName == "tool_price_setting" {
		operation_setting.RebuildToolPriceIndex()
	} else if configName == "billing_setting" {
		InvalidatePricingCache()
		ratio_setting.InvalidateExposedDataCache()
	} else if configName == "theme" {
		system_setting.UpdateAndSyncTheme()
	}

	return true
}
