package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	tossTransactionReconciliationInterval        = time.Hour
	tossTransactionReconciliationInitialLookback = 30 * 24 * time.Hour
	// Toss's integration-test store exposes transaction reconciliation for only
	// the most recent three days. Keep a small margin inside the boundary so
	// request transit and provider-clock drift cannot turn an exactly-three-day
	// startDate into an out-of-range request.
	tossTransactionTestEnvironmentLookback     = 3 * 24 * time.Hour
	tossTransactionTestEnvironmentSafetyMargin = 5 * time.Minute
	tossTransactionReconciliationRecentDays    = 7
	tossTransactionReconciliationWindow        = 24 * time.Hour
	tossTransactionReconciliationOverlap       = time.Minute
	tossTransactionReconciliationMaxWindows    = 3
	// Keep a full page comfortably below the shared 2 MiB provider-response
	// ceiling; the API permits 5000, but rich Transaction objects can exceed it.
	tossTransactionReconciliationPageLimit = 1000
	tossTransactionReconciliationMaxPages  = 20
	tossTransactionLookupTimeout           = 70 * time.Second
	// Leave discovery/cursor/database work outside the provider contexts and
	// reserve enough time for one Transaction GET plus one authoritative Payment
	// GET. A 75-second source could spend 60 seconds listing transactions and
	// then be permanently unable to start even its first 60-second detail lookup.
	tossTransactionSourceBudgetMargin  = 5 * time.Second
	tossTransactionMinimumSourceBudget = 2*tossTransactionLookupTimeout + tossTransactionSourceBudgetMargin
	// A source context can consume at most the enclosing 20-minute run. Keep
	// the durable lease beyond that hard deadline so a slow/canceling owner can
	// never overlap a successor, while still recovering before the next hourly
	// tick after a process crash.
	tossTransactionReconciliationLeaseDuration = 25 * time.Minute
)

var (
	tossTransactionReconciliationOnce    sync.Once
	tossTransactionReconciliationRunning atomic.Bool
	errTossTransactionPageLimit          = errors.New("Toss transaction reconciliation exceeded the page safety limit")
)

// tossTransactionPermanentMismatch marks authenticated provider evidence that
// cannot become applicable by retrying the same local operation. The page loop
// persists these rows in the operator reconciliation queue before advancing its
// durable transactionKey checkpoint. All untyped errors remain retryable and
// continue to stop the cursor (provider transport, malformed responses, DB
// failures, and concurrent local state transitions).
type tossTransactionPermanentMismatch struct {
	reason  string
	payment *tossConfirmResponse
	cause   error
}

func (e *tossTransactionPermanentMismatch) Error() string {
	if e == nil {
		return "permanent Toss transaction mismatch"
	}
	if e.cause != nil {
		return fmt.Sprintf("permanent Toss transaction mismatch (%s): %v", e.reason, e.cause)
	}
	return fmt.Sprintf("permanent Toss transaction mismatch (%s)", e.reason)
}

func (e *tossTransactionPermanentMismatch) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newTossTransactionPermanentMismatch(reason string, payment *tossConfirmResponse, cause error) error {
	return &tossTransactionPermanentMismatch{
		reason:  strings.TrimSpace(reason),
		payment: payment,
		cause:   cause,
	}
}

func classifyTossTransactionSubscriptionPlanError(
	reason string,
	auth *tossConfirmResponse,
	order *model.SubscriptionOrder,
	err error,
) error {
	if err == nil {
		return nil
	}
	// Snapshots and legacy plan identity are immutable inputs to settlement.
	// Retrying cannot repair a corrupt/mismatched snapshot, an invalid legacy
	// plan id, or a plan row that no longer exists. Preserve the authenticated
	// Payment in the operator queue and let later MID transactions continue.
	// All other errors (including context cancellation and database failures)
	// remain untyped so the durable cursor stays pinned and retries them.
	if errors.Is(err, model.ErrSubscriptionPlanSnapshotInvalid) ||
		errors.Is(err, model.ErrSubscriptionPlanSnapshotMismatch) ||
		errors.Is(err, gorm.ErrRecordNotFound) ||
		(order != nil && order.PlanId <= 0) {
		return newTossTransactionPermanentMismatch(reason, auth, err)
	}
	return err
}

func classifyTossTransactionCancellationEvidenceError(reason string, auth *tossConfirmResponse, err error) error {
	if errors.Is(err, errTossCancellationEvidenceInvalid) || errors.Is(err, model.ErrTossPaymentEventKeyConflict) {
		return newTossTransactionPermanentMismatch(reason, auth, err)
	}
	return err
}

func classifyTossTransactionRefundFinalizationError(auth *tossConfirmResponse, err error) error {
	if errors.Is(err, model.ErrTopUpStatusInvalid) ||
		errors.Is(err, model.ErrPaymentMethodMismatch) ||
		errors.Is(err, model.ErrTossPaymentKeyConflict) {
		return newTossTransactionPermanentMismatch(
			"refund_fenced_topup_finalization_conflict", auth, err,
		)
	}
	return err
}

func classifyTossTransactionTopUpSettlementTargetError(auth *tossConfirmResponse, err error) error {
	if errors.Is(err, model.ErrTossTopUpSettlementTargetMissing) {
		return newTossTransactionPermanentMismatch(
			"topup_fulfillment_settlement_target_missing", auth, err,
		)
	}
	return err
}

type tossTransactionCredentialSource struct {
	SourceKey      string
	SecretKey      string
	MIDFingerprint string
	InitialCursor  int64
	CursorAliases  []model.TossTransactionReconciliationCursorAlias
}

type tossCredentialRow struct {
	ProviderClientKeyHash string
	ProviderCredential    string
	CreateTime            int64
}

type tossTransactionCredentialDecoder func(string) (string, error)

type tossTransaction struct {
	MID            string `json:"mId"`
	TransactionKey string `json:"transactionKey"`
	PaymentKey     string `json:"paymentKey"`
	OrderID        string `json:"orderId"`
	Method         string `json:"method"`
	Status         string `json:"status"`
	TransactionAt  string `json:"transactionAt"`
	Currency       string `json:"currency"`
	Amount         int64  `json:"amount"`
}

var errTossTransactionCredentialNamespaceMismatch = errors.New("Toss transaction credential namespace does not match the local order")

// tossTransactionSourceMatchesStoredNamespace proves that a Transaction API
// row was read from the same Toss credential namespace as the local order. New
// rows carry an immutable client-key/MID fingerprint and can safely accept a
// rotated secret for that same MID. Pre-fingerprint rows must instead match the
// exact encrypted secret that was durably pinned before the provider request.
// Missing or unreadable legacy evidence is an error: reconciliation must never
// trade financial isolation for availability.
func tossTransactionSourceMatchesStoredNamespace(source tossTransactionCredentialSource, storedMIDFingerprint, encryptedCredential string) (bool, error) {
	if model.IsValidTossClientKeyFingerprint(storedMIDFingerprint) {
		sourceMIDFingerprint := source.MIDFingerprint
		if !model.IsValidTossClientKeyFingerprint(sourceMIDFingerprint) {
			return false, nil
		}
		return storedMIDFingerprint == sourceMIDFingerprint, nil
	}

	encryptedCredential = strings.TrimSpace(encryptedCredential)
	if encryptedCredential == "" || strings.TrimSpace(source.SecretKey) == "" {
		return false, errors.New("legacy or corrupt Toss transaction credential namespace cannot be proven")
	}
	storedSecret, err := model.DecryptProviderCredential(encryptedCredential)
	if err != nil {
		return false, fmt.Errorf("decrypt legacy Toss transaction credential: %w", err)
	}
	storedDigest := sha256.Sum256([]byte(strings.TrimSpace(storedSecret)))
	sourceDigest := sha256.Sum256([]byte(strings.TrimSpace(source.SecretKey)))
	return subtle.ConstantTimeCompare(storedDigest[:], sourceDigest[:]) == 1, nil
}

func tossSubscriptionOrderTransactionCredential(order *model.SubscriptionOrder) string {
	if order == nil {
		return ""
	}
	if strings.TrimSpace(order.BillingAttemptCredential) != "" {
		return order.BillingAttemptCredential
	}
	return order.ProviderCredential
}

func tossTransactionSourceMatchesSubscriptionOrder(source tossTransactionCredentialSource, order *model.SubscriptionOrder) (bool, error) {
	if order == nil {
		return false, errors.New("Toss transaction subscription order is missing")
	}
	return tossTransactionSourceMatchesStoredNamespace(
		source,
		order.ProviderClientKeyHash,
		tossSubscriptionOrderTransactionCredential(order),
	)
}

func tossTransactionSourceMatchesTopUp(source tossTransactionCredentialSource, topUp *model.TopUp) (bool, error) {
	if topUp == nil {
		return false, errors.New("Toss transaction top-up is missing")
	}
	return tossTransactionSourceMatchesStoredNamespace(source, topUp.ProviderClientKeyHash, topUp.ProviderCredential)
}

// tossTransactionSourceMatchesWalletPolicyAfterChargeProof is called only
// after the charge's TopUp namespace has been proven. Wallet policies created
// before credential snapshots were introduced have neither field, while their
// later charge TopUp still pins the exact provider secret before POST. That
// charge-level proof is sufficient for such a legacy policy. Any policy that
// does carry namespace evidence must independently match it.
func tossTransactionSourceMatchesWalletPolicyAfterChargeProof(source tossTransactionCredentialSource, policy *model.WalletAutoRecharge) (bool, error) {
	if policy == nil {
		return false, errors.New("Toss transaction wallet policy is missing")
	}
	if strings.TrimSpace(policy.ProviderClientKeyHash) == "" && strings.TrimSpace(policy.ProviderCredential) == "" {
		return true, nil
	}
	return tossTransactionSourceMatchesStoredNamespace(source, policy.ProviderClientKeyHash, policy.ProviderCredential)
}

// skipUnprovenTossTransactionLocalOrder classifies one legacy/corrupt local row
// as a permanent mismatch. The page loop durably dead-letters the authenticated
// Transaction before advancing, so the row remains fail-closed without pinning
// every later transaction from the same MID.
func skipUnprovenTossTransactionLocalOrder(ctx context.Context, source tossTransactionCredentialSource, orderID string, err error) (string, bool, error) {
	logger.LogError(ctx, fmt.Sprintf(
		"TOSS RECONCILIATION REQUIRED: local order has unprovable credential namespace order_id=%s source=%s error=%v",
		orderID,
		source.SourceKey,
		err,
	))
	return "", false, newTossTransactionPermanentMismatch("local_credential_namespace_unprovable", nil, err)
}

func tossTransactionSourceKey(midHash, secretKey string) string {
	if model.IsValidTossClientKeyFingerprint(midHash) {
		return midHash
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(secretKey)))
	return hex.EncodeToString(digest[:])
}

// tossTransactionConfiguredMIDHashForSecret maps a pre-fingerprint credential
// to a stable MID source only when the currently loaded configuration proves a
// unique, exact client/secret pairing. A secret reused by different client keys
// is ambiguous and must remain isolated under its secret hash.
func tossTransactionConfiguredMIDHashForSecret(snapshot setting.TossConfigSnapshot, secretKey string) string {
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return ""
	}
	matchedHash := ""
	for _, pair := range [][2]string{
		{snapshot.ClientKey, snapshot.SecretKey},
		{snapshot.TestClientKey, snapshot.TestSecretKey},
		{snapshot.BillingClientKey, snapshot.BillingSecretKey},
		{snapshot.BillingTestClientKey, snapshot.BillingTestSecretKey},
	} {
		if secretKey != strings.TrimSpace(pair[1]) {
			continue
		}
		candidateHash := model.TossClientKeyFingerprint(pair[0])
		if candidateHash == "" {
			continue
		}
		if matchedHash != "" && matchedHash != candidateHash {
			return ""
		}
		matchedHash = candidateHash
	}
	return matchedHash
}

func tossHashCondition(db *gorm.DB, hash string) *gorm.DB {
	if strings.TrimSpace(hash) == "" {
		return db.Where("provider_client_key_hash = '' OR provider_client_key_hash IS NULL")
	}
	return db.Where("provider_client_key_hash = ?", strings.TrimSpace(hash))
}

func latestReadableTossCredential(ctx context.Context, hash string) (*tossCredentialRow, string, error) {
	readLatest := func(db *gorm.DB, credentialProjection, timeProjection, credentialPredicate, orderBy string) (*tossCredentialRow, string, error) {
		query := db.WithContext(ctx).
			Select("provider_client_key_hash", credentialProjection, timeProjection).
			Where("payment_provider = ?", model.PaymentProviderToss).
			Where(credentialPredicate)
		rows, err := tossHashCondition(query, hash).Order(orderBy).Rows()
		if err != nil {
			return nil, "", err
		}
		defer rows.Close()
		for rows.Next() {
			var row tossCredentialRow
			if err := rows.Scan(&row.ProviderClientKeyHash, &row.ProviderCredential, &row.CreateTime); err != nil {
				return nil, "", err
			}
			secretKey, err := model.DecryptProviderCredential(row.ProviderCredential)
			if err != nil || strings.TrimSpace(secretKey) == "" {
				logger.LogWarn(ctx, fmt.Sprintf("Toss transaction reconciliation skipped unreadable credential mid_hash=%s", hash))
				continue
			}
			return &row, strings.TrimSpace(secretKey), nil
		}
		return nil, "", rows.Err()
	}

	var best *tossCredentialRow
	bestSecret := ""
	if model.DB.Migrator().HasTable(&model.TopUp{}) {
		row, secretKey, err := readLatest(
			model.DB.Model(&model.TopUp{}),
			"provider_credential",
			"create_time",
			"provider_credential <> ''",
			"create_time desc, id desc",
		)
		if err != nil {
			return nil, "", err
		}
		best, bestSecret = row, secretKey
	}
	if model.DB.Migrator().HasTable(&model.SubscriptionOrder{}) {
		// BillingAttemptCredential is the exact API-key namespace that actually
		// reached Toss. It can differ from the checkout's ProviderCredential after
		// an explicit same-MID key promotion, so prefer it and its immutable attempt
		// time when discovering the historical Transaction API source.
		credentialProjection := "CASE WHEN billing_attempt_credential IS NOT NULL AND billing_attempt_credential <> '' THEN billing_attempt_credential ELSE provider_credential END AS provider_credential"
		timeProjection := "CASE WHEN billing_attempt_credential IS NOT NULL AND billing_attempt_credential <> '' AND billing_attempt_time > 0 THEN billing_attempt_time ELSE create_time END AS create_time"
		row, secretKey, err := readLatest(
			model.DB.Model(&model.SubscriptionOrder{}),
			credentialProjection,
			timeProjection,
			"(billing_attempt_credential <> '' OR provider_credential <> '')",
			"CASE WHEN billing_attempt_credential IS NOT NULL AND billing_attempt_credential <> '' AND billing_attempt_time > 0 THEN billing_attempt_time ELSE create_time END DESC, id DESC",
		)
		if err != nil {
			return nil, "", err
		}
		if row != nil && (best == nil || row.CreateTime > best.CreateTime) {
			best, bestSecret = row, secretKey
		}
	}
	return best, bestSecret, nil
}

func oldestTossOrderTime(ctx context.Context, hash string) (int64, error) {
	oldest := int64(0)
	collect := func(db *gorm.DB) error {
		var row struct {
			CreateTime int64
		}
		query := db.WithContext(ctx).
			Select("COALESCE(MIN(create_time), 0) AS create_time").
			Where("payment_provider = ?", model.PaymentProviderToss)
		if err := tossHashCondition(query, hash).Scan(&row).Error; err != nil {
			return err
		}
		if row.CreateTime > 0 && (oldest == 0 || row.CreateTime < oldest) {
			oldest = row.CreateTime
		}
		return nil
	}
	if model.DB.Migrator().HasTable(&model.TopUp{}) {
		if err := collect(model.DB.Model(&model.TopUp{})); err != nil {
			return 0, err
		}
	}
	if model.DB.Migrator().HasTable(&model.SubscriptionOrder{}) {
		if err := collect(model.DB.Model(&model.SubscriptionOrder{})); err != nil {
			return 0, err
		}
	}
	return oldest, nil
}

func discoverTossTransactionCredentialSources(ctx context.Context) ([]tossTransactionCredentialSource, error) {
	snapshot := setting.GetTossConfigSnapshot()
	hashes := make(map[string]struct{})
	collect := func(db *gorm.DB, credentialPredicate string) error {
		var values []string
		if err := db.WithContext(ctx).Distinct("provider_client_key_hash").
			Where("payment_provider = ? AND provider_client_key_hash IS NOT NULL AND provider_client_key_hash <> ''", model.PaymentProviderToss).
			Where(credentialPredicate).
			Pluck("provider_client_key_hash", &values).Error; err != nil {
			return err
		}
		for _, value := range values {
			if model.IsValidTossClientKeyFingerprint(value) {
				hashes[value] = struct{}{}
			}
		}
		return nil
	}
	if model.DB.Migrator().HasTable(&model.TopUp{}) {
		if err := collect(model.DB.Model(&model.TopUp{}), "provider_credential <> ''"); err != nil {
			return nil, err
		}
	}
	if model.DB.Migrator().HasTable(&model.SubscriptionOrder{}) {
		if err := collect(model.DB.Model(&model.SubscriptionOrder{}), "(billing_attempt_credential <> '' OR provider_credential <> '')"); err != nil {
			return nil, err
		}
	}

	sources := make(map[string]tossTransactionCredentialSource)
	addAlias := func(sourceKey, aliasKey string, initialCursor int64) {
		sourceKey = strings.TrimSpace(sourceKey)
		aliasKey = strings.TrimSpace(aliasKey)
		if sourceKey == "" || aliasKey == "" || sourceKey == aliasKey {
			return
		}
		source, found := sources[sourceKey]
		if !found {
			return
		}
		for i := range source.CursorAliases {
			if source.CursorAliases[i].SourceKey != aliasKey {
				continue
			}
			if initialCursor > 0 && (source.CursorAliases[i].InitialCursor <= 0 || initialCursor < source.CursorAliases[i].InitialCursor) {
				source.CursorAliases[i].InitialCursor = initialCursor
				sources[sourceKey] = source
			}
			return
		}
		source.CursorAliases = append(source.CursorAliases, model.TossTransactionReconciliationCursorAlias{
			SourceKey:     aliasKey,
			InitialCursor: initialCursor,
		})
		sources[sourceKey] = source
	}
	putSource := func(sourceKey, secretKey, midFingerprint string, initialCursor int64) {
		existing, found := sources[sourceKey]
		if found && existing.InitialCursor > 0 && (initialCursor <= 0 || existing.InitialCursor < initialCursor) {
			initialCursor = existing.InitialCursor
		}
		midFingerprint = strings.ToLower(strings.TrimSpace(midFingerprint))
		if midFingerprint == "" {
			midFingerprint = existing.MIDFingerprint
		}
		sources[sourceKey] = tossTransactionCredentialSource{
			SourceKey: sourceKey, SecretKey: secretKey, MIDFingerprint: midFingerprint,
			InitialCursor: initialCursor, CursorAliases: existing.CursorAliases,
		}
		// A prior release used a credential hash as the cursor key. For a stable
		// MID source, remember that old key as an existing-only alias so a cursor
		// already created before fingerprint backfill is not abandoned.
		addAlias(sourceKey, tossTransactionSourceKey("", secretKey), 0)
	}
	for hash := range hashes {
		row, secretKey, err := latestReadableTossCredential(ctx, hash)
		if err != nil {
			return nil, err
		}
		if row == nil {
			continue
		}
		initialCursor, err := oldestTossOrderTime(ctx, hash)
		if err != nil {
			return nil, err
		}
		sourceKey := tossTransactionSourceKey(hash, secretKey)
		putSource(sourceKey, secretKey, hash, initialCursor)
	}

	// Rows written before MID fingerprints were introduced cannot safely be
	// grouped in SQL: they can span multiple historical MIDs and credentials,
	// and MySQL's default text collation can merge case-sensitive base64 values.
	// Stream every snapshot, decrypt it, and deduplicate by a one-way secret
	// fingerprint in memory. A corrupt row must not hide another historical MID.
	collectLegacy := func(db *gorm.DB) error {
		return collectLegacyTossTransactionCredentialSources(ctx, db, model.DecryptProviderCredential, func(legacySourceKey, secretKey string, createTime int64) {
			midHash := tossTransactionConfiguredMIDHashForSecret(snapshot, secretKey)
			if midHash == "" {
				putSource(legacySourceKey, secretKey, "", createTime)
				return
			}
			midSourceKey := tossTransactionSourceKey(midHash, secretKey)
			putSource(midSourceKey, secretKey, midHash, createTime)
			// Ensure the old source exists exactly once as a rollout bridge. Both
			// cursors are advanced atomically, so mixed-version nodes remain safe.
			addAlias(midSourceKey, legacySourceKey, createTime)
		})
	}
	if model.DB.Migrator().HasTable(&model.TopUp{}) {
		if err := collectLegacy(model.DB.Model(&model.TopUp{})); err != nil {
			return nil, err
		}
	}
	if model.DB.Migrator().HasTable(&model.SubscriptionOrder{}) {
		if err := collectLegacy(model.DB.Model(&model.SubscriptionOrder{})); err != nil {
			return nil, err
		}
		if err := collectLegacyTossTransactionAttemptCredentialSources(ctx, model.DB.Model(&model.SubscriptionOrder{}), model.DecryptProviderCredential, func(legacySourceKey, secretKey string, createTime int64) {
			midHash := tossTransactionConfiguredMIDHashForSecret(snapshot, secretKey)
			if midHash == "" {
				putSource(legacySourceKey, secretKey, "", createTime)
				return
			}
			midSourceKey := tossTransactionSourceKey(midHash, secretKey)
			putSource(midSourceKey, secretKey, midHash, createTime)
			addAlias(midSourceKey, legacySourceKey, createTime)
		}); err != nil {
			return nil, err
		}
	}

	// Prefer currently configured credentials for their MID. Stored operation
	// snapshots remain the fallback for a historical MID no longer configured.
	regularClient, regularSecret := setting.TossActiveKeyPairFromSnapshot(snapshot)
	billingClient, billingSecret := setting.TossActiveBillingKeyPairFromSnapshot(snapshot)
	for _, pair := range [][2]string{{regularClient, regularSecret}, {billingClient, billingSecret}} {
		if strings.TrimSpace(pair[0]) == "" || strings.TrimSpace(pair[1]) == "" {
			continue
		}
		hash := model.TossClientKeyFingerprint(pair[0])
		initialCursor, err := oldestTossOrderTime(ctx, hash)
		if err != nil {
			return nil, err
		}
		sourceKey := tossTransactionSourceKey(hash, pair[1])
		putSource(sourceKey, pair[1], hash, initialCursor)
	}

	result := make([]tossTransactionCredentialSource, 0, len(sources))
	for _, source := range sources {
		result = append(result, source)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].SourceKey < result[j].SourceKey
	})
	return result, nil
}

func collectLegacyTossTransactionCredentialSources(
	ctx context.Context,
	db *gorm.DB,
	decode tossTransactionCredentialDecoder,
	putSource func(sourceKey, secretKey string, initialCursor int64),
) error {
	if db == nil || decode == nil || putSource == nil {
		return errors.New("invalid legacy Toss credential source collector")
	}
	rows, err := db.WithContext(ctx).
		Select("provider_credential, create_time, provider_client_key_hash").
		Where("payment_provider = ? AND provider_credential <> ''", model.PaymentProviderToss).
		Order("create_time ASC, id ASC").
		Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var encrypted string
		var createTime int64
		var fingerprint sql.NullString
		if err := rows.Scan(&encrypted, &createTime, &fingerprint); err != nil {
			return err
		}
		if fingerprint.Valid && model.IsValidTossClientKeyFingerprint(fingerprint.String) {
			continue
		}
		secretKey, err := decode(encrypted)
		if err != nil || strings.TrimSpace(secretKey) == "" {
			logger.LogWarn(ctx, "Toss transaction reconciliation skipped unreadable legacy credential")
			continue
		}
		secretKey = strings.TrimSpace(secretKey)
		putSource(tossTransactionSourceKey("", secretKey), secretKey, createTime)
	}
	return rows.Err()
}

// collectLegacyTossTransactionAttemptCredentialSources preserves the exact
// credential namespace that reached Toss for pre-fingerprint subscription
// charges. A same-MID credential promotion can make it differ from the
// checkout snapshot stored in provider_credential.
func collectLegacyTossTransactionAttemptCredentialSources(
	ctx context.Context,
	db *gorm.DB,
	decode tossTransactionCredentialDecoder,
	putSource func(sourceKey, secretKey string, initialCursor int64),
) error {
	if db == nil || decode == nil || putSource == nil {
		return errors.New("invalid legacy Toss attempt credential source collector")
	}
	rows, err := db.WithContext(ctx).
		Select("billing_attempt_credential, CASE WHEN billing_attempt_time > 0 THEN billing_attempt_time ELSE create_time END AS attempt_time, provider_client_key_hash").
		Where("payment_provider = ? AND billing_attempt_credential <> ''", model.PaymentProviderToss).
		Order("CASE WHEN billing_attempt_time > 0 THEN billing_attempt_time ELSE create_time END ASC, id ASC").
		Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var encrypted string
		var attemptTime int64
		var fingerprint sql.NullString
		if err := rows.Scan(&encrypted, &attemptTime, &fingerprint); err != nil {
			return err
		}
		if fingerprint.Valid && model.IsValidTossClientKeyFingerprint(fingerprint.String) {
			continue
		}
		secretKey, err := decode(encrypted)
		if err != nil || strings.TrimSpace(secretKey) == "" {
			logger.LogWarn(ctx, "Toss transaction reconciliation skipped unreadable legacy attempt credential")
			continue
		}
		secretKey = strings.TrimSpace(secretKey)
		putSource(tossTransactionSourceKey("", secretKey), secretKey, attemptTime)
	}
	return rows.Err()
}

func orderTossTransactionSourcesForRun(sources []tossTransactionCredentialSource, now time.Time) []tossTransactionCredentialSource {
	ordered := append([]tossTransactionCredentialSource(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].SourceKey < ordered[j].SourceKey
	})
	if len(ordered) < 2 {
		return ordered
	}
	hour := now.Unix() / int64(tossTransactionReconciliationInterval/time.Second)
	offset := int(hour % int64(len(ordered)))
	if offset < 0 {
		offset += len(ordered)
	}
	return append(ordered[offset:], ordered[:offset]...)
}

func getTossTransactions(ctx context.Context, secretKey string, start, end time.Time, startingAfter string) ([]tossTransaction, error) {
	return getTossTransactionsWithLimit(ctx, secretKey, start, end, startingAfter, tossTransactionReconciliationPageLimit)
}

func getTossTransactionsWithLimit(ctx context.Context, secretKey string, start, end time.Time, startingAfter string, pageLimit int) ([]tossTransaction, error) {
	if pageLimit <= 0 || pageLimit > 5000 {
		return nil, errors.New("invalid Toss transaction page limit")
	}
	ctx, cancel := context.WithTimeout(ctx, tossTransactionLookupTimeout)
	defer cancel()
	seoul := time.FixedZone("Asia/Seoul", 9*60*60)
	query := url.Values{}
	query.Set("startDate", start.In(seoul).Format("2006-01-02T15:04:05"))
	query.Set("endDate", end.In(seoul).Format("2006-01-02T15:04:05"))
	query.Set("limit", fmt.Sprintf("%d", pageLimit))
	if startingAfter != "" {
		if !isValidTossTransactionKey(startingAfter) {
			return nil, errors.New("invalid Toss transaction pagination cursor")
		}
		query.Set("startingAfter", startingAfter)
	}
	statusCode, body, err := doTossPaymentProcessingAPIRequestWithSecret(ctx, http.MethodGet, tossAPIBase+"/v1/transactions?"+query.Encode(), nil, "", secretKey, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("Toss transaction lookup failed status=%d: %w", statusCode, err)
	}
	var transactions []tossTransaction
	if err := common.Unmarshal(body, &transactions); err != nil {
		return nil, err
	}
	if transactions == nil {
		// The documented success body is always a JSON array. Treating JSON null
		// as an empty page would complete the durable time cursor and permanently
		// skip every transaction in this provider interval.
		return nil, errors.New("Toss transaction lookup returned a non-array response")
	}
	if len(transactions) > pageLimit {
		return nil, errors.New("Toss transaction lookup exceeded requested page limit")
	}
	return transactions, nil
}

func getTossPaymentForTransactionReconciliation(ctx context.Context, paymentKey, secretKey string) (*tossConfirmResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, tossTransactionLookupTimeout)
	defer cancel()
	statusCode, body, err := doTossAPIRequestWithSecret(ctx, http.MethodGet, tossAPIBase+"/v1/payments/"+url.PathEscape(paymentKey), nil, "", secretKey, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("Toss reconciliation payment lookup failed status=%d: %w", statusCode, err)
	}
	var result tossConfirmResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	sanitizeTossPaymentResponse(&result)
	if err := validateTossPaymentResponseShape(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func applyTossTransactionCancellation(ctx context.Context, source tossTransactionCredentialSource, auth *tossConfirmResponse) error {
	if auth == nil || !isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
		return errors.New("Toss transaction reconciliation received a non-cancellation payment")
	}
	orderID := strings.TrimSpace(auth.OrderId)
	if !LockOrderWithContext(ctx, orderID) {
		return context.DeadlineExceeded
	}
	defer UnlockOrder(orderID)

	if order, err := model.GetSubscriptionOrderByTradeNoWithErrorContext(ctx, orderID); err == nil && order.PaymentProvider == model.PaymentProviderToss {
		matches, matchErr := tossTransactionSourceMatchesSubscriptionOrder(source, order)
		if matchErr != nil {
			return newTossTransactionPermanentMismatch("subscription_cancellation_namespace_unprovable", auth, matchErr)
		}
		if !matches {
			return newTossTransactionPermanentMismatch("subscription_cancellation_namespace_mismatch", auth, errTossTransactionCredentialNamespaceMismatch)
		}
		if auth.Type != "BILLING" {
			return newTossTransactionPermanentMismatch(
				"subscription_cancellation_type_mismatch",
				auth,
				errors.New("Toss reconciled subscription cancellation has an unexpected payment type"),
			)
		}
		expectedAmount := order.ProviderAmount
		if expectedAmount <= 0 {
			plan, planErr := model.ResolveTossSubscriptionOrderPlanWithContext(ctx, order)
			if planErr != nil {
				return classifyTossTransactionSubscriptionPlanError(
					"subscription_cancellation_contract_unresolvable", auth, order, planErr,
				)
			}
			expectedAmount = tossSubscriptionOrderChargeKRW(order, plan)
		}
		if !model.IsTossCardAmountPayableKRW(expectedAmount) || !isValidTossCardCancellation(auth, orderID, expectedAmount) {
			return newTossTransactionPermanentMismatch(
				"subscription_cancellation_contract_mismatch",
				auth,
				errors.New("Toss reconciled subscription cancellation does not match the local order"),
			)
		}
		created, canceledAmount, err := persistExpectedTossCancellationEvents(ctx, auth, orderID, expectedAmount)
		if err != nil {
			return classifyTossTransactionCancellationEvidenceError(
				"subscription_cancellation_evidence_mismatch", auth, err,
			)
		}
		payload, err := common.Marshal(auth)
		if err != nil {
			return err
		}
		if err := model.StopTossSubscriptionBillingAfterCancellationWithContext(ctx, orderID, string(payload)); err != nil {
			if errors.Is(err, model.ErrTossRenewalIdentityConflict) ||
				errors.Is(err, model.ErrTossRecurringOrderIDEvidenceCorrupt) {
				// The cancellation ledger is already durable, while these two errors
				// describe immutable renewal-order identity corruption. Retrying the
				// same provider transaction cannot repair it and would pin the MID.
				return newTossTransactionPermanentMismatch(
					"subscription_cancellation_identity_conflict", auth, err,
				)
			}
			return err
		}
		if created > 0 {
			model.RecordTopupLogWithContext(ctx, order.UserId, fmt.Sprintf("Toss subscription payment %s recovered by transaction reconciliation (canceled: %d KRW, balance: %d KRW)", auth.Status, canceledAmount, auth.BalanceAmount), "toss-transaction-reconciliation", order.PaymentMethod, "toss-subscription-cancel")
		}
		return nil
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		return err
	}

	topUp, err := model.GetTopUpByTradeNoWithErrorContext(ctx, orderID)
	if errors.Is(err, model.ErrTopUpNotFound) {
		// reconcileTossTransactionRow already proved this was a local order before
		// the authoritative Payment GET. If the row disappears during that network
		// gap, acknowledging the transaction would advance the durable checkpoint
		// without either a ledger entry or an operator-visible recovery record.
		return newTossTransactionPermanentMismatch(
			"local_order_disappeared_during_cancellation", auth, err,
		)
	}
	if err != nil {
		return err
	}
	if topUp.PaymentProvider != model.PaymentProviderToss || topUp.PaymentMethod != model.PaymentMethodToss {
		return newTossTransactionPermanentMismatch(
			"local_order_payment_discriminator_changed_during_cancellation",
			auth,
			errors.New("local project order no longer has its Toss payment discriminator"),
		)
	}
	matches, matchErr := tossTransactionSourceMatchesTopUp(source, topUp)
	if matchErr != nil {
		return newTossTransactionPermanentMismatch("topup_cancellation_namespace_unprovable", auth, matchErr)
	}
	if !matches {
		return newTossTransactionPermanentMismatch("topup_cancellation_namespace_mismatch", auth, errTossTransactionCredentialNamespaceMismatch)
	}

	if policy, found, lookupErr := model.GetWalletAutoRechargeByChargeTradeNoWithContext(ctx, orderID); lookupErr != nil {
		return lookupErr
	} else if found {
		matches, matchErr := tossTransactionSourceMatchesWalletPolicyAfterChargeProof(source, policy)
		if matchErr != nil {
			return newTossTransactionPermanentMismatch("wallet_cancellation_namespace_unprovable", auth, matchErr)
		}
		if !matches {
			return newTossTransactionPermanentMismatch("wallet_cancellation_namespace_mismatch", auth, errTossTransactionCredentialNamespaceMismatch)
		}
		if auth.Type != "BILLING" {
			return newTossTransactionPermanentMismatch(
				"wallet_cancellation_type_mismatch",
				auth,
				errors.New("Toss reconciled wallet cancellation has an unexpected payment type"),
			)
		}
		if !isValidTossCardCancellation(auth, orderID, topUp.Amount) {
			return newTossTransactionPermanentMismatch(
				"wallet_cancellation_contract_mismatch",
				auth,
				errors.New("Toss reconciled wallet cancellation does not match the local charge"),
			)
		}
		if _, _, err := persistExpectedTossCancellationEvents(ctx, auth, orderID, topUp.Amount); err != nil {
			return classifyTossTransactionCancellationEvidenceError(
				"wallet_cancellation_evidence_mismatch", auth, err,
			)
		}
		payload, err := common.Marshal(auth)
		if err != nil {
			return err
		}
		requiresReconciliation := strings.EqualFold(auth.Status, "PARTIAL_CANCELED") || auth.BalanceAmount != 0
		terminalErr := model.ApplyWalletAutoRechargeWebhookTerminalWithContext(
			ctx, policy.Id, orderID, auth.Status, string(payload), requiresReconciliation, time.Time{},
		)
		if errors.Is(terminalErr, model.ErrWalletAutoRechargeReconciliationRequired) {
			// Cancellation evidence and the wallet policy's manual-reconciliation
			// fence are already durable. This typed result is the expected terminal
			// outcome for a partial/non-zero-balance cancellation, not a retryable
			// failure that should pin every later MID transaction.
			return nil
		}
		return terminalErr
	}
	refundRequired, err := model.HasRequiredTossTopUpRefundWithContext(ctx, orderID, auth.PaymentKey)
	if err != nil {
		return err
	}
	if refundRequired {
		if auth.Type != "NORMAL" || strings.TrimSpace(auth.PaymentKey) != strings.TrimSpace(topUp.ProviderOrderId) ||
			auth.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
			return newTossTransactionPermanentMismatch(
				"refund_fenced_topup_cancellation_identity_mismatch",
				auth,
				errors.New("Toss reconciled refund-fenced cancellation does not match the local order"),
			)
		}
		created, canceledAmount, persistErr := persistTossCancellationEventsWithContext(ctx, auth)
		if persistErr != nil {
			return classifyTossTransactionCancellationEvidenceError(
				"refund_fenced_topup_cancellation_evidence_mismatch", auth, persistErr,
			)
		}
		if created > 0 {
			model.RecordTopupLogWithContext(ctx, topUp.UserId, fmt.Sprintf("Toss refund-fenced payment %s recovered by transaction reconciliation (canceled: %d KRW, balance: %d KRW)", auth.Status, canceledAmount, auth.BalanceAmount), "toss-transaction-reconciliation", topUp.PaymentMethod, "toss-cancel")
		}
		if !isFullyCanceledTossTopUp(auth, topUp) {
			// PARTIAL_CANCELED and virtual-account refunds whose bank refund is
			// pending/unproved remain owned by the durable refund fence.
			return nil
		}
		payload, marshalErr := common.Marshal(auth)
		if marshalErr != nil {
			return marshalErr
		}
		finalizeErr := model.FinalizeFullyRefundedTossTopUpWithContext(ctx, orderID, auth.PaymentKey, string(payload))
		// The full provider cancellation is already durable. Only typed immutable
		// conflicts are dead-lettered; DB/context and ordinary not-found failures
		// remain retryable and continue to pin the checkpoint.
		return classifyTossTransactionRefundFinalizationError(auth, finalizeErr)
	}
	boundPaymentKey := strings.TrimSpace(topUp.ProviderOrderId)
	if auth.Type != "NORMAL" || strings.TrimSpace(auth.OrderId) != strings.TrimSpace(topUp.TradeNo) ||
		strings.TrimSpace(auth.PaymentKey) == "" ||
		(boundPaymentKey != "" && boundPaymentKey != strings.TrimSpace(topUp.TradeNo) && boundPaymentKey != strings.TrimSpace(auth.PaymentKey)) ||
		auth.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
		return newTossTransactionPermanentMismatch(
			"topup_cancellation_identity_mismatch",
			auth,
			errors.New("Toss reconciled top-up cancellation identity does not match the local order"),
		)
	}
	if topUp.Status != common.TopUpStatusSuccess {
		created, canceledAmount, persistErr := persistUncreditedTossTopUpCancellationWithContext(
			ctx,
			topUp,
			auth,
			"transaction reconciliation found an uncredited cancellation whose provider refund is incomplete",
		)
		if persistErr != nil {
			return classifyTossTransactionCancellationEvidenceError(
				"topup_cancellation_evidence_mismatch", auth, persistErr,
			)
		}
		if created > 0 {
			model.RecordTopupLogWithContext(ctx, topUp.UserId, fmt.Sprintf("Toss payment %s recovered by transaction reconciliation (canceled: %d KRW, balance: %d KRW)", auth.Status, canceledAmount, auth.BalanceAmount), "toss-transaction-reconciliation", topUp.PaymentMethod, "toss-cancel")
		}
		if tossTopUpCancellationNeedsRefundFence(auth) {
			return nil
		}
		if topUp.Status == common.TopUpStatusPending {
			if err := model.UpdatePendingTopUpStatusForMethodContext(ctx, orderID, model.PaymentProviderToss, model.PaymentMethodToss, common.TopUpStatusFailed); err != nil && !errors.Is(err, model.ErrTopUpStatusInvalid) {
				return err
			}
		}
		return nil
	}

	// A historically credited order may predate the current purchase-contract
	// validator. Exact provider identity and amount are enough to preserve every
	// later cancellation transaction; quota reversal remains an operator action.
	created, canceledAmount, err := persistTossCancellationEventsWithContext(ctx, auth)
	if err != nil {
		return classifyTossTransactionCancellationEvidenceError(
			"topup_cancellation_evidence_mismatch", auth, err,
		)
	}
	if created > 0 {
		model.RecordTopupLogWithContext(ctx, topUp.UserId, fmt.Sprintf("Toss payment %s recovered by transaction reconciliation (canceled: %d KRW, balance: %d KRW)", auth.Status, canceledAmount, auth.BalanceAmount), "toss-transaction-reconciliation", topUp.PaymentMethod, "toss-cancel")
	}
	return nil
}

// applyTossTransactionFulfillment recovers a successful approval that outlived
// both browser callback delivery and Toss's finite webhook retry schedule. The
// server-created billing flows already retain a deterministic provider attempt
// and have their own recovery workers; the otherwise-unrecoverable gap is a
// normal checkout for which the browser/webhook were the only sources of the
// paymentKey.
func applyTossTransactionFulfillment(ctx context.Context, source tossTransactionCredentialSource, auth *tossConfirmResponse) error {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Status), "DONE") {
		return errors.New("Toss transaction reconciliation received a non-DONE payment")
	}
	orderID := strings.TrimSpace(auth.OrderId)
	if !LockOrderWithContext(ctx, orderID) {
		return context.DeadlineExceeded
	}
	defer UnlockOrder(orderID)

	// Subscription and wallet charges are server-created with durable orderId,
	// billing-key, credential, and attempt snapshots. Their dedicated recovery
	// workers re-run the exact idempotent attempt and settlement protocol. Do not
	// route them through the generic top-up settlement path.
	if order, err := model.GetSubscriptionOrderByTradeNoWithErrorContext(ctx, orderID); err == nil && order.PaymentProvider == model.PaymentProviderToss {
		matches, matchErr := tossTransactionSourceMatchesSubscriptionOrder(source, order)
		if matchErr != nil {
			return newTossTransactionPermanentMismatch("subscription_fulfillment_namespace_unprovable", auth, matchErr)
		}
		if !matches {
			return newTossTransactionPermanentMismatch("subscription_fulfillment_namespace_mismatch", auth, errTossTransactionCredentialNamespaceMismatch)
		}
		if auth.Type != "BILLING" {
			return newTossTransactionPermanentMismatch(
				"subscription_fulfillment_type_mismatch",
				auth,
				errors.New("Toss reconciled subscription fulfillment has an unexpected payment type"),
			)
		}
		expectedAmount := order.ProviderAmount
		if expectedAmount <= 0 {
			plan, planErr := model.ResolveTossSubscriptionOrderPlanWithContext(ctx, order)
			if planErr != nil {
				return classifyTossTransactionSubscriptionPlanError(
					"subscription_fulfillment_contract_unresolvable", auth, order, planErr,
				)
			}
			expectedAmount = tossSubscriptionOrderChargeKRW(order, plan)
		}
		if !isValidTossBillingCharge(auth, orderID, expectedAmount) {
			return newTossTransactionPermanentMismatch(
				"subscription_fulfillment_contract_mismatch",
				auth,
				errors.New("Toss reconciled subscription fulfillment does not match the local order"),
			)
		}
		if order.Status == common.TopUpStatusSuccess {
			// The subscription settlement transaction is the authoritative proof
			// that the paid entitlement was granted. A Transaction API approval for
			// an already-successful order must not create a new manual-work item.
			return model.ResolveTossPaymentEventsWithContext(ctx, orderID, model.TossPaymentEventTypeFulfillment)
		}
		// Persist evidence before returning. The existing subscription reconciler
		// owns activation/renewal semantics and will resolve this idempotent event
		// once local fulfillment succeeds.
		if _, err := persistTossFulfillmentEventWithContext(ctx, auth); err != nil {
			return err
		}
		return nil
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		return err
	}

	topUp, err := model.GetTopUpByTradeNoWithErrorContext(ctx, orderID)
	if errors.Is(err, model.ErrTopUpNotFound) {
		return newTossTransactionPermanentMismatch(
			"local_order_disappeared_during_fulfillment", auth, err,
		)
	}
	if err != nil {
		return err
	}
	if topUp.PaymentProvider != model.PaymentProviderToss || topUp.PaymentMethod != model.PaymentMethodToss {
		return newTossTransactionPermanentMismatch(
			"local_order_payment_discriminator_changed_during_fulfillment",
			auth,
			errors.New("local project order no longer has its Toss payment discriminator"),
		)
	}
	matches, matchErr := tossTransactionSourceMatchesTopUp(source, topUp)
	if matchErr != nil {
		return newTossTransactionPermanentMismatch("topup_fulfillment_namespace_unprovable", auth, matchErr)
	}
	if !matches {
		return newTossTransactionPermanentMismatch("topup_fulfillment_namespace_mismatch", auth, errTossTransactionCredentialNamespaceMismatch)
	}

	if policy, found, lookupErr := model.GetWalletAutoRechargeByChargeTradeNoWithContext(ctx, orderID); lookupErr != nil {
		return lookupErr
	} else if found {
		matches, matchErr := tossTransactionSourceMatchesWalletPolicyAfterChargeProof(source, policy)
		if matchErr != nil {
			return newTossTransactionPermanentMismatch("wallet_fulfillment_namespace_unprovable", auth, matchErr)
		}
		if !matches {
			return newTossTransactionPermanentMismatch("wallet_fulfillment_namespace_mismatch", auth, errTossTransactionCredentialNamespaceMismatch)
		}
		if auth.Type != "BILLING" || !isValidTossBillingCharge(auth, orderID, topUp.Amount) {
			return newTossTransactionPermanentMismatch(
				"wallet_fulfillment_contract_mismatch",
				auth,
				errors.New("Toss reconciled wallet fulfillment does not match the local charge"),
			)
		}
		payload, marshalErr := common.Marshal(auth)
		if marshalErr != nil {
			return marshalErr
		}
		if evidenceErr := model.RecordWalletAutoRechargeProviderDONEEvidenceWithContext(ctx, policy.Id, orderID, &model.TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  auth.Status,
			Total:           auth.TotalAmount,
			BalanceAmount:   auth.BalanceAmount,
			PaymentKey:      auth.PaymentKey,
			ProviderPayload: string(payload),
		}); evidenceErr != nil {
			if errors.Is(evidenceErr, model.ErrWalletAutoRechargeReconciliationRequired) {
				return nil
			}
			return evidenceErr
		}
		settleErr := model.SettleWalletAutoRechargeWebhookDoneWithContext(ctx, policy.Id, orderID, auth.PaymentKey, string(payload), time.Time{})
		if errors.Is(settleErr, model.ErrWalletAutoRechargeReconciliationRequired) {
			// The wallet settlement function has durably recorded a manual event.
			return nil
		}
		return settleErr
	}
	if topUp.Status == common.TopUpStatusSuccess {
		if auth.Type != "NORMAL" || strings.TrimSpace(topUp.ProviderOrderId) != strings.TrimSpace(auth.PaymentKey) {
			return newTossTransactionPermanentMismatch(
				"credited_topup_fulfillment_identity_mismatch",
				auth,
				errors.New("Toss reconciled approval does not match the credited top-up identity"),
			)
		}
		if isValidTossTopUpPayment(auth, orderID, topUp.Amount) {
			return model.ResolveTossPaymentEventsWithContext(ctx, orderID, model.TossPaymentEventTypeFulfillment)
		}
		// Never refund an order whose quota was already granted. A newly tightened
		// method/tax contract can classify historical successful payments as
		// invalid; preserve an audit item and advance the transaction cursor rather
		// than trying to install an uncredited-payment refund fence.
		_, persistErr := persistTossFinancialMismatchEventWithContext(ctx, auth, "transaction reconciliation found a contract mismatch on an already-credited top-up")
		return persistErr
	}
	refundRequired, err := model.HasRequiredTossTopUpRefundWithContext(ctx, orderID, auth.PaymentKey)
	if err != nil {
		return err
	}
	if refundRequired {
		return nil
	}

	if auth.Type == "NORMAL" && auth.TotalAmount == topUp.Amount &&
		strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") &&
		!isValidTossTopUpPayment(auth, orderID, topUp.Amount) {
		// The payment belongs to this immutable order and the exact amount moved,
		// but browser-controlled method/tax/escrow fields violate the server
		// contract. Install the same durable refund fence used by callbacks; the
		// dedicated top-up recovery lane owns the idempotent cancellation POST.
		if _, err := queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "transaction reconciliation found an unfulfillable top-up contract"); err != nil {
			return err
		}
		return nil
	}
	if auth.Type != "NORMAL" || !isValidTossTopUpPayment(auth, orderID, topUp.Amount) {
		return newTossTransactionPermanentMismatch(
			"topup_fulfillment_contract_mismatch",
			auth,
			errors.New("Toss reconciled top-up fulfillment does not match the local order"),
		)
	}
	if err := model.RecoverTossTopUpPaymentKeyWithContext(ctx, orderID, auth.PaymentKey); err != nil {
		if errors.Is(err, model.ErrTossTopUpSettlementTargetMissing) {
			// The local order exists and the authenticated Payment matches it, but
			// an incomplete restore or legacy orphan removed the immutable quota
			// target. Preserve the Payment in the operator queue and advance.
			return classifyTossTransactionTopUpSettlementTargetError(auth, err)
		}
		if errors.Is(err, model.ErrTossPaymentKeyConflict) {
			// A different non-placeholder binding is immutable financial evidence,
			// not a transient database race. Never overwrite it, but also do not let
			// this one poisoned local order pin every later transaction from the MID.
			// The page loop will persist the authenticated DONE Payment before it
			// advances this row's transactionKey checkpoint.
			return newTossTransactionPermanentMismatch(
				"topup_fulfillment_payment_key_conflict",
				auth,
				err,
			)
		}
		return err
	}
	if err := model.RechargeTossWithContext(ctx, orderID, auth.PaymentKey, "toss-transaction-reconciliation"); err != nil {
		if errors.Is(err, model.ErrTossTopUpSettlementTargetMissing) {
			return classifyTossTransactionTopUpSettlementTargetError(auth, err)
		}
		if errors.Is(err, model.ErrTossCancellationPrecedesFulfillment) {
			return nil
		}
		if errors.Is(err, model.ErrTopUpQuotaCapacityExceeded) {
			if _, queueErr := queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "quota capacity prevented transaction-reconciled fulfillment"); queueErr != nil {
				return errors.Join(err, queueErr)
			}
			return nil
		}
		if errors.Is(err, model.ErrTossRefundRequiredPrecedesFulfillment) {
			return nil
		}
		return err
	}
	if err := model.ResolveTossPaymentEventsWithContext(ctx, orderID, model.TossPaymentEventTypeFulfillment); err != nil {
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss top-up payment recovered by transaction reconciliation order_id=%s", orderID))
	return nil
}

func isLowerHexSuffix(value, prefix string, hexLength int) bool {
	if hexLength <= 0 || !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+hexLength {
		return false
	}
	for i := len(prefix); i < len(value); i++ {
		ch := value[i]
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') {
			continue
		}
		return false
	}
	return true
}

func isPositiveDecimal(value string) bool {
	if value == "" {
		return false
	}
	nonZero := false
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
		if value[i] != '0' {
			nonZero = true
		}
	}
	return nonZero
}

func isLegacyTossWalletOrderID(orderID string) bool {
	rest, ok := strings.CutPrefix(orderID, "wallet_auto_")
	if !ok {
		return false
	}
	parts := strings.Split(rest, "_")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for i := range parts {
		if !isPositiveDecimal(parts[i]) {
			return false
		}
	}
	return true
}

// isReservedTossProjectOrderID recognizes only provider IDs this project can
// actually generate. A bare "toss_" prefix is intentionally insufficient: a
// MID may be shared with another application, and a foreign order must not
// create an operator incident merely because it chose a similar label.
func isReservedTossProjectOrderID(orderID string) bool {
	orderID = strings.TrimSpace(orderID)
	if isLowerHexSuffix(orderID, "toss_", 40) ||
		isLowerHexSuffix(orderID, "toss_sub_", 40) ||
		isLowerHexSuffix(orderID, "trn_", 40) ||
		isLowerHexSuffix(orderID, "twa_", 40) ||
		isLegacyTossWalletOrderID(orderID) {
		return true
	}
	_, _, renewal := model.ParseTossRenewalTradeNo(orderID)
	return renewal
}

func lookupLocalTossTransactionOrderStatus(ctx context.Context, source tossTransactionCredentialSource, orderID string) (status string, local bool, exists bool, err error) {
	localOrderExists := false
	reservedLocalDiscriminatorCorrupt := false
	order, err := model.GetSubscriptionOrderByTradeNoWithErrorContext(ctx, orderID)
	if err == nil {
		localOrderExists = true
		if order.PaymentProvider == model.PaymentProviderToss {
			matches, matchErr := tossTransactionSourceMatchesSubscriptionOrder(source, order)
			if matchErr != nil {
				status, local, err := skipUnprovenTossTransactionLocalOrder(ctx, source, orderID, matchErr)
				return status, local, true, err
			}
			if !matches {
				return "", false, true, nil
			}
			return order.Status, true, true, nil
		}
		reservedLocalDiscriminatorCorrupt = isReservedTossProjectOrderID(orderID)
	} else if !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		return "", false, false, err
	}

	topUp, err := model.GetTopUpByTradeNoWithErrorContext(ctx, orderID)
	if err == nil {
		localOrderExists = true
		if topUp.PaymentProvider == model.PaymentProviderToss && topUp.PaymentMethod == model.PaymentMethodToss {
			matches, matchErr := tossTransactionSourceMatchesTopUp(source, topUp)
			if matchErr != nil {
				status, local, err := skipUnprovenTossTransactionLocalOrder(ctx, source, orderID, matchErr)
				return status, local, true, err
			}
			if !matches {
				return "", false, true, nil
			}
			policy, found, lookupErr := model.GetWalletAutoRechargeByChargeTradeNoWithContext(ctx, orderID)
			if lookupErr != nil {
				return "", false, true, lookupErr
			}
			if found {
				matches, matchErr = tossTransactionSourceMatchesWalletPolicyAfterChargeProof(source, policy)
				if matchErr != nil {
					status, local, err := skipUnprovenTossTransactionLocalOrder(ctx, source, orderID, matchErr)
					return status, local, true, err
				}
				if !matches {
					return "", false, true, nil
				}
			}
			return topUp.Status, true, true, nil
		}
		reservedLocalDiscriminatorCorrupt = reservedLocalDiscriminatorCorrupt || isReservedTossProjectOrderID(orderID)
	}
	if errors.Is(err, model.ErrTopUpNotFound) {
		if reservedLocalDiscriminatorCorrupt {
			// Exact project-generated order identity plus a surviving local row is
			// stronger evidence than mutable discriminator columns. A partial restore
			// can preserve trade_no while clearing payment_provider/payment_method;
			// silently treating that paid Transaction as a foreign shared-MID order
			// would make the financial gap invisible forever. Keep it fail-closed and
			// let the page loop durably dead-letter the provider row for repair.
			return "", false, true, newTossTransactionPermanentMismatch(
				"local_order_payment_discriminator_corrupt",
				nil,
				errors.New("local project order lost its Toss payment discriminator"),
			)
		}
		return "", false, localOrderExists, nil
	}
	if err == nil && reservedLocalDiscriminatorCorrupt {
		return "", false, true, newTossTransactionPermanentMismatch(
			"local_order_payment_discriminator_corrupt",
			nil,
			errors.New("local project order lost its Toss payment discriminator"),
		)
	}
	return "", false, localOrderExists, err
}

func localTossTransactionOrderStatus(ctx context.Context, source tossTransactionCredentialSource, orderID string) (string, bool, error) {
	status, local, _, err := lookupLocalTossTransactionOrderStatus(ctx, source, orderID)
	return status, local, err
}

func reconcileTossTransactionWindow(ctx context.Context, source tossTransactionCredentialSource, start, end time.Time) error {
	return reconcileTossTransactionWindowWithLimits(ctx, source, start, end, tossTransactionReconciliationPageLimit, tossTransactionReconciliationMaxPages)
}

func reconcileTossTransactionWindowWithLimits(ctx context.Context, source tossTransactionCredentialSource, start, end time.Time, pageLimit, maxPages int) error {
	if !end.After(start) || pageLimit <= 0 || maxPages <= 0 {
		return errors.New("invalid Toss transaction reconciliation window")
	}
	err := reconcileTossTransactionWindowPages(ctx, source, start, end, pageLimit, maxPages)
	if !errors.Is(err, errTossTransactionPageLimit) {
		return err
	}

	// A fixed page ceiling must not poison the durable time cursor forever for a
	// high-volume MID. Split only after the bounded page scan fills completely,
	// then require both halves to finish before the caller advances its cursor.
	// Provider actions and local event writes are idempotent, so re-reading the
	// already processed prefix during the split is safe.
	midpoint := start.Add(end.Sub(start) / 2).Truncate(time.Second)
	if !midpoint.After(start) || !midpoint.Before(end) {
		return fmt.Errorf("%w: minimum time window exhausted", errTossTransactionPageLimit)
	}
	if err := reconcileTossTransactionWindowWithLimits(ctx, source, start, midpoint, pageLimit, maxPages); err != nil {
		return err
	}
	return reconcileTossTransactionWindowWithLimits(ctx, source, midpoint, end, pageLimit, maxPages)
}

func reconcileTossTransactionWindowPages(ctx context.Context, source tossTransactionCredentialSource, start, end time.Time, pageLimit, maxPages int) error {
	return reconcileTossTransactionWindowPagesFrom(ctx, source, start, end, pageLimit, maxPages, "", nil)
}

type tossTransactionPageAdvance func(expectedStartingAfter, nextStartingAfter string) error

type tossTransactionMismatchEvidence struct {
	SourceKey   string               `json:"sourceKey"`
	Reason      string               `json:"reason"`
	Transaction *tossTransaction     `json:"transaction"`
	Payment     *tossConfirmResponse `json:"payment,omitempty"`
}

// persistTossTransactionMismatch durably dead-letters one authenticated row
// before its page checkpoint advances. The event key hashes the stable MID
// source, transactionKey, paymentKey, and the full evidence snapshot, so exact
// reruns are idempotent while a genuinely changed provider state remains a
// distinct operator-visible incident.
func persistTossTransactionMismatch(
	ctx context.Context,
	source tossTransactionCredentialSource,
	transaction *tossTransaction,
	mismatch *tossTransactionPermanentMismatch,
) error {
	if transaction == nil || mismatch == nil || strings.TrimSpace(mismatch.reason) == "" {
		return errors.New("invalid Toss transaction mismatch evidence")
	}
	evidence := tossTransactionMismatchEvidence{
		SourceKey:   strings.TrimSpace(source.SourceKey),
		Reason:      strings.TrimSpace(mismatch.reason),
		Transaction: transaction,
		Payment:     mismatch.payment,
	}
	payload, err := common.Marshal(evidence)
	if err != nil {
		return err
	}
	paymentKey := strings.TrimSpace(transaction.PaymentKey)
	if !isValidTossPaymentKey(paymentKey) {
		// Keep malformed provider evidence in the bounded payload and its event-key
		// hash, not in a varchar(200) identity column.
		paymentKey = ""
	}
	transactionKey := transaction.TransactionKey
	if !isValidTossTransactionKey(transactionKey) {
		// Malformed provider input remains available only inside the bounded JSON
		// evidence. Never copy whitespace/control data into indexed columns or logs.
		transactionKey = ""
	}
	status := strings.ToUpper(strings.TrimSpace(transaction.Status))
	originalAmount := transaction.Amount
	balanceAmount := int64(0)
	if mismatch.payment != nil {
		status = strings.ToUpper(strings.TrimSpace(mismatch.payment.Status))
		originalAmount = mismatch.payment.TotalAmount
		balanceAmount = mismatch.payment.BalanceAmount
	}
	created, err := model.RecordTossPaymentEventWithContext(ctx, &model.TossPaymentEvent{
		EventKey:             "toss_tx_mismatch_" + common.Sha1(payload),
		EventType:            model.TossPaymentEventTypeFinancialMismatch,
		OrderId:              strings.TrimSpace(transaction.OrderID),
		PaymentKey:           paymentKey,
		Status:               status,
		TransactionKey:       transactionKey,
		BalanceAmount:        balanceAmount,
		OriginalAmount:       originalAmount,
		ProviderPayload:      string(payload),
		ReconciliationStatus: model.TossReconciliationStatusRequired,
		ResolutionNote:       evidence.Reason,
	})
	if err != nil {
		return err
	}
	if created {
		logger.LogError(ctx, fmt.Sprintf(
			"TOSS RECONCILIATION REQUIRED: transaction mismatch dead-lettered order_id=%s transaction_key=%s source=%s reason=%s",
			transaction.OrderID,
			transactionKey,
			evidence.SourceKey,
			evidence.Reason,
		))
	}
	return nil
}

func reconcileTossTransactionRow(
	ctx context.Context,
	source tossTransactionCredentialSource,
	transaction *tossTransaction,
	processedDonePayments map[string]struct{},
) error {
	transactionStatus := strings.ToUpper(strings.TrimSpace(transaction.Status))
	switch transactionStatus {
	case "DONE", "CANCELED", "PARTIAL_CANCELED":
		// Financial states are verified below against the authoritative Payment.
	case "READY", "IN_PROGRESS", "WAITING_FOR_DEPOSIT", "ABORTED", "EXPIRED":
		return nil
	default:
		// The documented enum is closed for this API version. A future financial
		// state must stop the checkpoint until the reconciler is upgraded rather
		// than being silently mistaken for an irrelevant transaction.
		return errors.New("Toss transaction lookup returned an unsupported status")
	}
	if !isValidTossOrderId(transaction.OrderID) {
		// A MID can be shared with another application or Toss product whose
		// order-id rules differ. Local orders are generated with this stricter
		// format, so an invalid foreign identity must not poison the time cursor.
		return nil
	}
	_, local, localOrderExists, err := lookupLocalTossTransactionOrderStatus(ctx, source, transaction.OrderID)
	if err != nil {
		return err
	}
	if !local {
		if !localOrderExists && isReservedTossProjectOrderID(transaction.OrderID) &&
			isValidTossPaymentKey(transaction.PaymentKey) && isValidTossTransactionKey(transaction.TransactionKey) {
			// The Transaction belongs to a provider namespace this project can
			// generate, but its local financial row is gone (for example after an
			// incomplete database restore). Never auto-credit from Transaction-only
			// evidence and do not spend a Payment GET on an unscoped identity. Keep
			// the exact MID transaction in the operator queue before checkpointing.
			return newTossTransactionPermanentMismatch(
				"missing_local_order",
				nil,
				errors.New("Toss transaction has no local project order"),
			)
		}
		// Transaction lookup is MID-wide. Avoid one authoritative Payment GET
		// or operator event for unrelated orders sharing the merchant namespace,
		// and for a local order that belongs to another proven MID source.
		return nil
	}
	if !isValidTossPaymentKey(transaction.PaymentKey) {
		return newTossTransactionPermanentMismatch(
			"transaction_payment_key_invalid",
			nil,
			errors.New("Toss transaction lookup returned an invalid payment identity"),
		)
	}
	if transactionStatus == "DONE" {
		if _, done := processedDonePayments[transaction.PaymentKey]; done {
			return nil
		}
	}
	auth, err := getTossPaymentForTransactionReconciliation(ctx, transaction.PaymentKey, source.SecretKey)
	if err != nil {
		return err
	}
	if auth.PaymentKey != transaction.PaymentKey {
		return newTossTransactionPermanentMismatch(
			"authoritative_payment_key_mismatch",
			auth,
			errors.New("Toss reconciliation lookup returned a different paymentKey"),
		)
	}
	if auth.OrderId != transaction.OrderID {
		return newTossTransactionPermanentMismatch(
			"authoritative_order_mismatch",
			auth,
			errors.New("Toss transaction is not visible in the authoritative Payment object"),
		)
	}
	authoritativeStatus := strings.ToUpper(strings.TrimSpace(auth.Status))
	switch {
	case isTossCancelStatus(authoritativeStatus):
		return applyTossTransactionCancellation(ctx, source, auth)
	case transactionStatus == "DONE" && authoritativeStatus == "DONE":
		if err := applyTossTransactionFulfillment(ctx, source, auth); err != nil {
			return err
		}
		processedDonePayments[transaction.PaymentKey] = struct{}{}
		return nil
	case transactionStatus == "DONE" && authoritativeStatus == "WAITING_FOR_DEPOSIT" &&
		strings.TrimSpace(auth.Method) == "가상계좌":
		// Toss documents a non-monotonic virtual-account transition: a bank can
		// first publish a DONE transaction and later revert the Payment resource
		// to WAITING_FOR_DEPOSIT after an actual deposit error. Retrying this
		// immutable historical transaction pins the source cursor forever, while
		// treating it as fulfilled could credit money that never arrived. Persist
		// the authenticated rollback for operator reconciliation, then let later
		// transaction rows and future redeposit attempts continue.
		return newTossTransactionPermanentMismatch(
			"virtual_account_deposit_reverted_to_waiting",
			auth,
			errors.New("Toss virtual-account DONE transaction reverted to WAITING_FOR_DEPOSIT"),
		)
	default:
		// Transaction listing and the Payment resource can become visible at
		// slightly different times. In particular, a cancellation row followed by
		// a still-DONE Payment must be retried rather than dead-lettered as a
		// permanent mismatch, otherwise a later-visible refund could be skipped.
		return errors.New("Toss transaction state is not visible in the authoritative Payment object")
	}
}

func reconcileTossTransactionWindowPagesFrom(
	ctx context.Context,
	source tossTransactionCredentialSource,
	start, end time.Time,
	pageLimit, maxPages int,
	startingAfter string,
	advancePage tossTransactionPageAdvance,
) error {
	if startingAfter != "" && !isValidTossTransactionKey(startingAfter) {
		return errors.New("invalid Toss transaction pagination cursor")
	}
	processedDonePayments := make(map[string]struct{})
	for page := 0; page < maxPages; page++ {
		pageStartingAfter := startingAfter
		var transactions []tossTransaction
		for {
			var err error
			transactions, err = getTossTransactionsWithLimit(ctx, source.SecretKey, start, end, startingAfter, pageLimit)
			if !errors.Is(err, errTossAPIResponseTooLarge) {
				if err != nil {
					return err
				}
				break
			}
			if pageLimit <= 1 {
				return fmt.Errorf("%w: Toss transaction response exceeds limit at one row", errTossAPIResponseTooLarge)
			}
			// A valid page containing rich transaction objects can exceed the
			// shared HTTP response cap. Retry the exact cursor with a smaller page;
			// only successful pages count against maxPages or advance pagination.
			pageLimit = (pageLimit + 1) / 2
		}
		for i := range transactions {
			transaction := &transactions[i]
			transactionKey := transaction.TransactionKey
			if !isValidTossTransactionKey(transactionKey) || transactionKey == startingAfter {
				return errors.New("Toss transaction lookup returned an invalid payment identity")
			}
			if !isSafeTossAPIEnum(transaction.Status) {
				// status is required on every documented Transaction object. Silently
				// treating a missing/lowercase/corrupt value as an irrelevant state can
				// checkpoint what was actually an approval or cancellation row.
				return errors.New("Toss transaction lookup returned an invalid status")
			}
			if err := reconcileTossTransactionRow(ctx, source, transaction, processedDonePayments); err != nil {
				var permanent *tossTransactionPermanentMismatch
				if !errors.As(err, &permanent) {
					return err
				}
				if err := persistTossTransactionMismatch(ctx, source, transaction, permanent); err != nil {
					// A durable operator record is the precondition for skipping an
					// authenticated mismatch. DB failures therefore keep the row pinned.
					return err
				}
			}
			if advancePage != nil {
				if err := advancePage(startingAfter, transactionKey); err != nil {
					return err
				}
				startingAfter = transactionKey
			}
		}
		if len(transactions) < pageLimit {
			return nil
		}
		lastKey := strings.TrimSpace(transactions[len(transactions)-1].TransactionKey)
		if lastKey == "" || lastKey == pageStartingAfter {
			return errors.New("Toss transaction pagination cursor did not advance")
		}
		if advancePage == nil {
			startingAfter = lastKey
		} else if startingAfter != lastKey {
			return errors.New("Toss transaction page checkpoint did not reach the last row")
		}
	}
	return errTossTransactionPageLimit
}

// reconcileTossTransactionWindowDurable resumes a busy provider interval from
// its last fully applied page. The page checkpoint is removed before the caller
// advances its higher-level time cursor, so a crash can cause a harmless replay
// but can never skip a transaction.
func reconcileTossTransactionWindowDurable(
	ctx context.Context,
	source tossTransactionCredentialSource,
	lane string,
	cursorTime int64,
	start, end time.Time,
	pageLimit, maxPages int,
) (int64, error) {
	return reconcileTossTransactionWindowDurableInternal(
		ctx, source, lane, "", cursorTime, start, end, pageLimit, maxPages,
	)
}

func reconcileTossTransactionWindowDurableLeased(
	ctx context.Context,
	source tossTransactionCredentialSource,
	lane, leaseOwner string,
	cursorTime int64,
	start, end time.Time,
	pageLimit, maxPages int,
) (int64, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" {
		return 0, errors.New("missing Toss transaction reconciliation lease owner")
	}
	return reconcileTossTransactionWindowDurableInternal(
		ctx, source, lane, leaseOwner, cursorTime, start, end, pageLimit, maxPages,
	)
}

func reconcileTossTransactionWindowDurableInternal(
	ctx context.Context,
	source tossTransactionCredentialSource,
	lane, leaseOwner string,
	cursorTime int64,
	start, end time.Time,
	pageLimit, maxPages int,
) (int64, error) {
	if lane != model.TossTransactionPageLaneRecent && lane != model.TossTransactionPageLaneHistorical {
		return 0, errors.New("invalid Toss transaction reconciliation lane")
	}
	if !end.After(start) || cursorTime <= 0 || pageLimit <= 0 || maxPages <= 0 {
		return 0, errors.New("invalid durable Toss transaction reconciliation window")
	}
	startUnix := start.Unix()
	endUnix := end.Unix()
	if startUnix <= 0 || endUnix <= startUnix {
		return 0, errors.New("invalid durable Toss transaction reconciliation timestamps")
	}
	var checkpoint *model.TossTransactionReconciliationPageCursor
	var err error
	if leaseOwner == "" {
		checkpoint, err = model.GetOrCreateTossTransactionReconciliationPageCursorWithContext(
			ctx, source.SourceKey, lane, cursorTime, startUnix, endUnix,
		)
	} else {
		checkpoint, err = model.GetOrCreateTossTransactionReconciliationPageCursorLeasedWithContext(
			ctx, source.SourceKey, lane, leaseOwner, cursorTime, startUnix, endUnix,
		)
	}
	if err != nil {
		return 0, err
	}
	startingAfter := strings.TrimSpace(checkpoint.StartingAfter)
	err = reconcileTossTransactionWindowPagesFrom(
		ctx,
		source,
		time.Unix(checkpoint.StartTime, 0),
		time.Unix(checkpoint.EndTime, 0),
		pageLimit,
		maxPages,
		startingAfter,
		func(expectedStartingAfter, nextStartingAfter string) error {
			var advanceErr error
			if leaseOwner == "" {
				advanceErr = model.AdvanceTossTransactionReconciliationPageCursorWithContext(
					ctx, source.SourceKey, lane, checkpoint.CursorTime, checkpoint.StartTime,
					checkpoint.EndTime, expectedStartingAfter, nextStartingAfter,
				)
			} else {
				advanceErr = model.AdvanceTossTransactionReconciliationPageCursorLeasedWithContext(
					ctx, source.SourceKey, lane, leaseOwner, checkpoint.CursorTime, checkpoint.StartTime,
					checkpoint.EndTime, expectedStartingAfter, nextStartingAfter,
				)
			}
			if advanceErr != nil {
				return advanceErr
			}
			startingAfter = nextStartingAfter
			return nil
		},
	)
	if err != nil {
		return 0, err
	}
	var completeErr error
	if leaseOwner == "" {
		completeErr = model.CompleteTossTransactionReconciliationPageCursorWithContext(
			ctx, source.SourceKey, lane, checkpoint.CursorTime, checkpoint.StartTime, checkpoint.EndTime, startingAfter,
		)
	} else {
		completeErr = model.CompleteTossTransactionReconciliationPageCursorLeasedWithContext(
			ctx, source.SourceKey, lane, leaseOwner, checkpoint.CursorTime, checkpoint.StartTime, checkpoint.EndTime, startingAfter,
		)
	}
	if completeErr != nil {
		return 0, completeErr
	}
	return checkpoint.EndTime, nil
}

func reconcileTossTransactionSource(ctx context.Context, source tossTransactionCredentialSource, now time.Time) error {
	initial := now.Add(-tossTransactionReconciliationInitialLookback).Unix()
	if source.InitialCursor > 0 && source.InitialCursor < initial {
		initial = source.InitialCursor - int64(tossTransactionReconciliationOverlap/time.Second)
		if initial <= 0 {
			initial = source.InitialCursor
		}
	}
	testEnvironmentFloor := int64(0)
	if strings.HasPrefix(strings.TrimSpace(source.SecretKey), "test_sk_") {
		testEnvironmentFloor = now.Add(-tossTransactionTestEnvironmentLookback).
			Add(tossTransactionReconciliationOverlap + tossTransactionTestEnvironmentSafetyMargin).
			Unix()
		if initial < testEnvironmentFloor {
			initial = testEnvironmentFloor
		}
	}
	cursor, activeAliases, err := model.GetOrCreateTossTransactionReconciliationCursorWithAliasesWithContext(ctx, source.SourceKey, initial, source.CursorAliases)
	if err != nil {
		return err
	}
	leaseTokenBytes := make([]byte, 16)
	if _, err := rand.Read(leaseTokenBytes); err != nil {
		return fmt.Errorf("generate Toss transaction reconciliation lease owner: %w", err)
	}
	leaseOwner := hex.EncodeToString(leaseTokenBytes)
	acquired, err := model.AcquireTossTransactionReconciliationLeaseWithContext(
		ctx,
		source.SourceKey,
		leaseOwner,
		int64(tossTransactionReconciliationLeaseDuration/time.Second),
	)
	if err != nil {
		return err
	}
	if !acquired {
		return model.ErrTossTransactionLeaseHeld
	}
	defer func() {
		// The source context is commonly canceled on provider timeout. Use a
		// small independent cleanup context; if the process dies or the database
		// remains unavailable, lease expiry is the authoritative recovery path.
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer releaseCancel()
		_ = model.ReleaseTossTransactionReconciliationLeaseWithContext(releaseCtx, source.SourceKey, leaseOwner)
	}()
	// Existing installations may already have a 30-day cursor for this test MID.
	// Advance the target and every rolling-upgrade alias atomically before any
	// provider call; otherwise an out-of-range startDate can pin the source on
	// every hourly run. Test transactions older than this floor are unavailable
	// from Toss by contract, so no recoverable provider interval is skipped.
	if testEnvironmentFloor > 0 && cursor.CursorTime < testEnvironmentFloor {
		if err := model.AdvanceTossTransactionReconciliationCursorWithAliasesLeasedWithContext(
			ctx,
			source.SourceKey,
			activeAliases,
			leaseOwner,
			cursor.CursorTime,
			testEnvironmentFloor,
		); err != nil {
			return err
		}
		cursor.CursorTime = testEnvironmentFloor
	}
	if testEnvironmentFloor > 0 {
		providerFloor := testEnvironmentFloor - int64(tossTransactionReconciliationOverlap/time.Second)
		if err := model.DeleteTossTransactionReconciliationPageCursorsBeforeWithContext(ctx, source.SourceKey, providerFloor); err != nil {
			return err
		}
	}
	// A fresh deployment can have a historical cursor backlog. Reconcile the
	// newest seven days first so a refund whose finite webhook retries expired is
	// not delayed while older history catches up. The recent lane has its own
	// durable cursor: without it, every hourly run would re-read all seven days
	// until a years-old backlog reached the present; with a one-time boolean,
	// newly arriving cancellations would instead be missed during that catch-up.
	// Keep the lane active until the historical cursor joins it.
	nowUnix := now.Unix()
	recentCutoff := now.Add(-tossTransactionReconciliationRecentDays * 24 * time.Hour).Unix()
	recentLaneActive := cursor.CursorTime < recentCutoff ||
		(cursor.RecentCursorTime > 0 && cursor.CursorTime < cursor.RecentCursorTime)
	if recentLaneActive {
		if cursor.RecentCursorTime < 0 || cursor.RecentCursorTime > nowUnix {
			return errors.New("invalid Toss transaction reconciliation recent cursor")
		}
		if cursor.RecentCursorTime == 0 {
			// Seed the oldest edge before scanning. Every subsequently completed
			// window is checkpointed, so a shared task timeout cannot force a busy
			// MID to restart the newest seven days forever.
			if err := model.AdvanceTossTransactionReconciliationRecentCursorWithAliasesLeasedWithContext(
				ctx,
				source.SourceKey,
				activeAliases,
				leaseOwner,
				cursor.CursorTime,
				0,
				recentCutoff,
			); err != nil {
				return err
			}
			cursor.RecentCursorTime = recentCutoff
		}
		// Normally this is one hourly window. Bound each provider request to a
		// day after a scheduler outage and checkpoint each complete range with an
		// exact recent-cursor CAS. The overlap is re-read on resume by design.
		scanCursor := cursor.RecentCursorTime
		for scanCursor < nowUnix {
			endUnix := scanCursor + int64(tossTransactionReconciliationWindow/time.Second)
			if endUnix > nowUnix {
				endUnix = nowUnix
			}
			startUnix := scanCursor - int64(tossTransactionReconciliationOverlap/time.Second)
			if startUnix <= 0 {
				startUnix = scanCursor
			}
			completedEndUnix, err := reconcileTossTransactionWindowDurableLeased(
				ctx,
				source,
				model.TossTransactionPageLaneRecent,
				leaseOwner,
				scanCursor,
				time.Unix(startUnix, 0),
				time.Unix(endUnix, 0),
				tossTransactionReconciliationPageLimit,
				tossTransactionReconciliationMaxPages,
			)
			if err != nil {
				return err
			}
			if completedEndUnix <= scanCursor || completedEndUnix > endUnix {
				return errors.New("invalid completed Toss recent transaction window")
			}
			if err := model.AdvanceTossTransactionReconciliationRecentCursorWithAliasesLeasedWithContext(
				ctx,
				source.SourceKey,
				activeAliases,
				leaseOwner,
				cursor.CursorTime,
				scanCursor,
				completedEndUnix,
			); err != nil {
				return err
			}
			scanCursor = completedEndUnix
			cursor.RecentCursorTime = completedEndUnix
		}
	}
	for window := 0; window < tossTransactionReconciliationMaxWindows && cursor.CursorTime < nowUnix; window++ {
		startUnix := cursor.CursorTime - int64(tossTransactionReconciliationOverlap/time.Second)
		if startUnix <= 0 {
			startUnix = cursor.CursorTime
		}
		endUnix := cursor.CursorTime + int64(tossTransactionReconciliationWindow/time.Second)
		if endUnix > nowUnix {
			endUnix = nowUnix
		}
		if endUnix <= cursor.CursorTime {
			break
		}
		completedEndUnix, err := reconcileTossTransactionWindowDurableLeased(
			ctx,
			source,
			model.TossTransactionPageLaneHistorical,
			leaseOwner,
			cursor.CursorTime,
			time.Unix(startUnix, 0),
			time.Unix(endUnix, 0),
			tossTransactionReconciliationPageLimit,
			tossTransactionReconciliationMaxPages,
		)
		if err != nil {
			return err
		}
		if completedEndUnix <= cursor.CursorTime || completedEndUnix > endUnix {
			return errors.New("invalid completed Toss historical transaction window")
		}
		if err := model.AdvanceTossTransactionReconciliationCursorWithAliasesLeasedWithContext(ctx, source.SourceKey, activeAliases, leaseOwner, cursor.CursorTime, completedEndUnix); err != nil {
			return err
		}
		cursor.CursorTime = completedEndUnix
	}
	return nil
}

func runTossTransactionReconciliationOnce() {
	if !tossTransactionReconciliationRunning.CompareAndSwap(false, true) {
		return
	}
	defer tossTransactionReconciliationRunning.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	sources, err := discoverTossTransactionCredentialSources(ctx)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss transaction reconciliation credential discovery failed: %v", err))
		return
	}
	// The cursor is shared across master failover. Use the same database clock as
	// order/cursor timestamps so a fast application host cannot advance the
	// durable provider window beyond a successor node's notion of "now".
	now := time.Unix(model.GetDBTimestamp(), 0)
	sources = orderTossTransactionSourcesForRun(sources, now)
	deadline, hasDeadline := ctx.Deadline()
	for i, source := range sources {
		sourceCtx := ctx
		cancelSource := func() {}
		if hasDeadline {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			budget, ok := tossTransactionSourceBudget(remaining, len(sources)-i)
			if !ok {
				break
			}
			sourceCtx, cancelSource = context.WithTimeout(ctx, budget)
		}
		err := reconcileTossTransactionSource(sourceCtx, source, now)
		cancelSource()
		if err != nil && !errors.Is(err, model.ErrTossTransactionLeaseHeld) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss transaction reconciliation failed source=%s error=%v", source.SourceKey, err))
		}
	}
}

// tossTransactionSourceBudget preserves fair sharing when there is ample run
// time, but never starts a source with a deadline too short for one compliant
// transaction-list lookup followed by one authoritative payment lookup. With
// more MIDs than fit in one run, hourly source rotation makes the deferred
// suffix the next run's prefix instead of giving every MID an unusable slice.
func tossTransactionSourceBudget(remaining time.Duration, remainingSources int) (time.Duration, bool) {
	if remainingSources <= 0 || remaining < tossTransactionMinimumSourceBudget {
		return 0, false
	}
	budget := remaining / time.Duration(remainingSources)
	if budget < tossTransactionMinimumSourceBudget {
		budget = tossTransactionMinimumSourceBudget
	}
	if budget > remaining {
		budget = remaining
	}
	return budget, true
}

func runTossTransactionReconciliationIteration(run func()) {
	if run == nil {
		return
	}
	defer func() {
		if recover() != nil {
			// gopool recovery would keep the process alive but terminate this
			// ticker goroutine. Recover at the iteration boundary so reconciliation
			// resumes on the next tick without logging payment data or panic values.
			logger.LogError(context.Background(), "Toss transaction reconciliation iteration panic recovered; later ticks will continue")
		}
	}()
	run()
}

// StartTossTransactionReconciliationTask reconciles missed approval and
// cancellation transactions after browser callbacks or Toss's finite webhook
// retry schedule can no longer complete local settlement.
func StartTossTransactionReconciliationTask() {
	tossTransactionReconciliationOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(tossTransactionReconciliationInterval)
			defer ticker.Stop()
			runTossTransactionReconciliationIteration(runTossTransactionReconciliationOnce)
			for range ticker.C {
				runTossTransactionReconciliationIteration(runTossTransactionReconciliationOnce)
			}
		})
	})
}
