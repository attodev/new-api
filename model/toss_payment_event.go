package model

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func PreservePaidTossSubscriptionOrder(tradeNo string, billingKeyId int, providerPayload string) error {
	return PreservePaidTossSubscriptionOrderWithContext(context.Background(), tradeNo, billingKeyId, providerPayload)
}

func PreservePaidTossSubscriptionOrderWithContext(ctx context.Context, tradeNo string, billingKeyId int, providerPayload string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" || billingKeyId <= 0 {
		return errors.New("invalid paid Toss subscription order")
	}
	updates := map[string]interface{}{"billing_key_id": billingKeyId}
	if strings.TrimSpace(providerPayload) != "" {
		updates["provider_payload"] = providerPayload
	}
	db := dbWithContext(ctx)
	result := db.Model(&SubscriptionOrder{}).
		Where("trade_no = ? AND payment_provider = ? AND status = ?", tradeNo, PaymentProviderToss, common.TopUpStatusPending).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var order SubscriptionOrder
	if err := db.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return err
	}
	if order.PaymentProvider == PaymentProviderToss && order.Status == common.TopUpStatusSuccess {
		return nil
	}
	return ErrSubscriptionOrderStatusInvalid
}

const (
	TossPaymentEventTypeCancellation      = "cancellation"
	TossPaymentEventTypeFulfillment       = "paid_pending_fulfillment"
	TossPaymentEventTypeFinancialMismatch = "financial_mismatch"
	// RefundRequired is a provider-write fence for an authenticated payment
	// that must never be fulfilled locally (for example, a browser-mutated tax
	// or payment-method contract, or an unrepresentable quota credit). It stays
	// required until a full provider cancellation is durably observed.
	TossPaymentEventTypeRefundRequired = "refund_required"

	TossReconciliationStatusRequired = "required"
	TossReconciliationStatusResolved = "resolved"
)

func TossTopUpRefundRequiredEventKey(orderID, paymentKey string) string {
	seed := strings.Join([]string{strings.TrimSpace(orderID), strings.TrimSpace(paymentKey)}, "\x00")
	return "toss_refund_required_" + common.Sha1([]byte(seed))
}

// TossFinancialMismatchEventKey is shared by every browser, webhook, stale
// recovery, and recurring-payment writer. Including the provider paymentKey
// and monetary snapshot keeps distinct identities and successive partial
// cancellation states separate while making an exact redelivery idempotent
// across entry points.
func TossFinancialMismatchEventKey(orderID, paymentKey, status string, originalAmount, balanceAmount int64) string {
	seed := strings.Join([]string{
		strings.TrimSpace(orderID),
		strings.TrimSpace(paymentKey),
		strings.ToUpper(strings.TrimSpace(status)),
		strconv.FormatInt(originalAmount, 10),
		strconv.FormatInt(balanceAmount, 10),
	}, "\x00")
	return "toss_fin_mismatch_" + common.Sha1([]byte(seed))
}

var (
	ErrTossPaymentEventNotFound            = errors.New("toss payment event not found")
	ErrTossPaymentEventStatusInvalid       = errors.New("toss payment event status invalid")
	ErrTossPaymentEventKeyConflict         = errors.New("toss payment event key conflicts with different provider evidence")
	ErrTossCancellationPrecedesFulfillment = errors.New("an authoritative Toss cancellation precedes local fulfillment")
	ErrTossRefundOperationStateInvalid     = errors.New("toss refund operation state is invalid")
	ErrTossRefundBalanceIncreased          = errors.New("toss refund remaining balance increased")
)

func sameTossPaymentEventEvidence(left, right *TossPaymentEvent) bool {
	if left == nil || right == nil {
		return false
	}
	if strings.TrimSpace(left.EventType) != strings.TrimSpace(right.EventType) ||
		strings.TrimSpace(left.OrderId) != strings.TrimSpace(right.OrderId) ||
		strings.TrimSpace(left.PaymentKey) != strings.TrimSpace(right.PaymentKey) {
		return false
	}
	// An exact cancel transaction keeps the same transactionKey and amount as
	// the payment later advances from PARTIAL_CANCELED to CANCELED. The enclosing
	// payment status/balance are therefore snapshot fields, not part of that
	// transaction's immutable identity. Cumulative legacy events include those
	// fields in their own key and continue through the strict comparison below.
	if left.EventType == TossPaymentEventTypeCancellation &&
		!left.CancelAmountCumulative && !right.CancelAmountCumulative {
		return strings.TrimSpace(left.TransactionKey) == strings.TrimSpace(right.TransactionKey) &&
			left.CancelAmount == right.CancelAmount &&
			left.OriginalAmount == right.OriginalAmount
	}
	if left.EventType == TossPaymentEventTypeRefundRequired {
		// One stable refund fence follows a payment through WAITING, DONE, and
		// cancellation. Those are mutable provider snapshots; order/payment and
		// original amount are the immutable refund identity.
		return left.OriginalAmount == right.OriginalAmount
	}
	return strings.ToUpper(strings.TrimSpace(left.Status)) == strings.ToUpper(strings.TrimSpace(right.Status)) &&
		strings.TrimSpace(left.TransactionKey) == strings.TrimSpace(right.TransactionKey) &&
		left.CancelAmount == right.CancelAmount &&
		left.CancelAmountCumulative == right.CancelAmountCumulative &&
		left.BalanceAmount == right.BalanceAmount &&
		left.OriginalAmount == right.OriginalAmount
}

func hasAuthoritativeTossCancellationForOrderTx(tx *gorm.DB, orderID string) (bool, error) {
	if tx == nil || strings.TrimSpace(orderID) == "" {
		return false, nil
	}
	if !tx.Migrator().HasTable(&TossPaymentEvent{}) {
		return false, nil
	}
	var count int64
	if err := tx.Model(&TossPaymentEvent{}).
		Where("order_id = ? AND event_type = ? AND status IN ?", orderID, TossPaymentEventTypeCancellation, []string{"CANCELED", "PARTIAL_CANCELED"}).
		Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func hasRequiredTossTopUpRefundForOrderTx(tx *gorm.DB, orderID string) (bool, error) {
	if tx == nil || strings.TrimSpace(orderID) == "" || !tx.Migrator().HasTable(&TossPaymentEvent{}) {
		return false, nil
	}
	var count int64
	if err := tx.Model(&TossPaymentEvent{}).
		Where("order_id = ? AND event_type = ? AND reconciliation_status = ?", orderID, TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired).
		Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func HasRequiredTossTopUpRefundWithContext(ctx context.Context, orderID, paymentKey string) (bool, error) {
	orderID = strings.TrimSpace(orderID)
	paymentKey = strings.TrimSpace(paymentKey)
	if orderID == "" || paymentKey == "" {
		return false, errors.New("toss refund identity is required")
	}
	var count int64
	err := dbWithContext(ctx).Model(&TossPaymentEvent{}).
		Where("order_id = ? AND payment_key = ? AND event_type = ? AND reconciliation_status = ?",
			orderID, paymentKey, TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired).
		Limit(1).Count(&count).Error
	return count > 0, err
}

type TossTopUpRefundOperation struct {
	IdempotencyKey   string
	FirstAttemptTime int64
	BalanceAmount    int64
	Rotated          bool
}

func isValidTossTopUpRefundOperationKey(value string) bool {
	const prefix = "toss_refund_"
	if value == "" || value != strings.TrimSpace(value) || value != strings.ToLower(value) || !strings.HasPrefix(value, prefix) {
		return false
	}
	digest := strings.TrimPrefix(value, prefix)
	if len(digest) != 40 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == 20
}

// PrepareTossTopUpRefundOperationWithContext durably pins one cancellation
// operation to the authoritative remaining balance before the provider POST.
// Every retry of that balance reuses the same key throughout Toss's documented
// 15-day retention period. Only a lower provider balance (proof that an earlier
// cancellation completed) or expiry of that full retention period may create a
// new operation. The TopUp-first lock order matches settlement/finalization so
// concurrent workers cannot rotate the key independently.
func PrepareTossTopUpRefundOperationWithContext(ctx context.Context, orderID, paymentKey string, balanceAmount int64) (operation TossTopUpRefundOperation, err error) {
	orderID = strings.TrimSpace(orderID)
	paymentKey = strings.TrimSpace(paymentKey)
	if orderID == "" || paymentKey == "" || balanceAmount <= 0 {
		return operation, ErrTossRefundOperationStateInvalid
	}
	candidateKey := "toss_refund_" + common.Sha1([]byte(strings.Join([]string{
		orderID,
		paymentKey,
		strconv.FormatInt(balanceAmount, 10),
		common.GetUUID(),
	}, "\x00")))

	err = dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", orderID).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		if topUp.ProviderOrderId != paymentKey || topUp.Amount < balanceAmount {
			return ErrTossRefundOperationStateInvalid
		}
		switch topUp.Status {
		case common.TopUpStatusPending, TossTopUpStatusRefundPending, common.TopUpStatusFailed, common.TopUpStatusExpired:
		default:
			return ErrTossRefundOperationStateInvalid
		}

		var event TossPaymentEvent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_id = ? AND payment_key = ? AND event_type = ? AND reconciliation_status = ?",
				orderID, paymentKey, TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired).
			First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTossPaymentEventNotFound
			}
			return err
		}
		if event.OriginalAmount != topUp.Amount {
			return ErrTossRefundOperationStateInvalid
		}

		hasAnyState := event.RefundOperationKey != "" || event.RefundOperationTime != 0 || event.RefundOperationBalance != 0
		hasCompleteState := isValidTossTopUpRefundOperationKey(event.RefundOperationKey) &&
			event.RefundOperationTime > 0 && event.RefundOperationBalance > 0
		if hasAnyState && !hasCompleteState {
			return ErrTossRefundOperationStateInvalid
		}

		now := getDBTimestampTx(tx)
		if hasCompleteState {
			if event.RefundOperationTime > now || event.RefundOperationBalance > event.OriginalAmount ||
				event.RefundOperationBalance > topUp.Amount {
				return ErrTossRefundOperationStateInvalid
			}
			if balanceAmount > event.RefundOperationBalance {
				// Cancellation is monotonic. A higher balance is a stale or corrupt
				// observation and must not authorize another provider operation.
				return ErrTossRefundBalanceIncreased
			}
			operationAge := now - event.RefundOperationTime
			if balanceAmount == event.RefundOperationBalance && operationAge < TossProviderIdempotencyRetentionSeconds {
				operation = TossTopUpRefundOperation{
					IdempotencyKey:   event.RefundOperationKey,
					FirstAttemptTime: event.RefundOperationTime,
					BalanceAmount:    event.RefundOperationBalance,
				}
				return nil
			}
		}

		result := tx.Model(&TossPaymentEvent{}).
			Where("id = ? AND reconciliation_status = ?", event.Id, TossReconciliationStatusRequired).
			Updates(map[string]interface{}{
				"refund_operation_key":     candidateKey,
				"refund_operation_time":    now,
				"refund_operation_balance": balanceAmount,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossPaymentEventStatusInvalid
		}
		operation = TossTopUpRefundOperation{
			IdempotencyKey:   candidateKey,
			FirstAttemptTime: now,
			BalanceAmount:    balanceAmount,
			Rotated:          hasCompleteState,
		}
		return nil
	})
	return operation, err
}

// RecordTossCancellationEventsForOrderWithContext serializes cancellation
// evidence with every local fulfillment path by locking the authoritative
// order row before inserting the event batch. Settlement locks the same row
// before checking these events, so either fulfillment wins first (and the
// cancellation remains a post-credit reconciliation) or cancellation wins
// first and fulfillment is blocked.
func RecordTossCancellationEventsForOrderWithContext(ctx context.Context, orderID string, events []*TossPaymentEvent) (int, int64, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" || len(events) == 0 {
		return 0, 0, errors.New("Toss cancellation event batch is invalid")
	}
	walletPolicyID := 0
	if policy, walletOrder, err := GetWalletAutoRechargeByChargeTradeNoWithContext(ctx, orderID); err != nil {
		return 0, 0, err
	} else if walletOrder {
		walletPolicyID = policy.Id
	}
	createdCount := 0
	createdAmount := int64(0)
	err := dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		var walletPolicy *WalletAutoRecharge
		if walletPolicyID > 0 {
			lockedPolicy, err := lockWalletAutoRechargeFinancialMutationTx(tx, walletPolicyID)
			if err != nil {
				return err
			}
			walletPolicy = lockedPolicy
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", orderID).First(&topUp).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) || !strings.HasPrefix(orderID, "wallet_auto_"+strconv.Itoa(walletPolicy.Id)+"_") {
					return err
				}
			} else {
				belongs, err := walletAutoRechargeTopUpBelongsToPolicy(&topUp, walletPolicy)
				if err != nil {
					return err
				}
				if !belongs {
					return ErrWalletAutoRechargeAttemptAssociationConflict
				}
			}
		} else {
			topUpErr := gorm.ErrRecordNotFound
			if tx.Migrator().HasTable(&TopUp{}) {
				topUpErr = tx.Select("id").Where("trade_no = ?", orderID).First(&topUp).Error
			}
			if topUpErr != nil && !errors.Is(topUpErr, gorm.ErrRecordNotFound) {
				return topUpErr
			}
			if errors.Is(topUpErr, gorm.ErrRecordNotFound) {
				if !tx.Migrator().HasTable(&SubscriptionOrder{}) {
					return gorm.ErrRecordNotFound
				}
				var order SubscriptionOrder
				if err := tx.Select("id").Where("trade_no = ?", orderID).First(&order).Error; err != nil {
					return err
				}
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", order.Id).First(&order).Error; err != nil {
					return err
				}
			} else if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", topUp.Id).First(&topUp).Error; err != nil {
				return err
			}
		}

		// Some older/partial Payment responses omit transactionKey or
		// cancelStatus, so the controller has to persist a cumulative refund
		// snapshot. A later retry can contain the complete Cancel objects. If we
		// inserted those transaction rows as a new lineage, the same provider
		// refund would appear twice in the operator queue and newlyCanceled would
		// be counted again. Once a payment has entered cumulative mode, keep it in
		// that mode. The newest provider payload still accompanies any genuinely
		// larger snapshot, while an unchanged snapshot is an idempotent no-op.
		//
		// This decision is made after locking the local order, at the same
		// serialization point as event insertion. An outside preflight query would
		// leave a race between a legacy webhook and a detailed redelivery.
		allExact := true
		paymentKey := ""
		for i := range events {
			if events[i] == nil || events[i].CancelAmountCumulative {
				allExact = false
				break
			}
			candidateKey := strings.TrimSpace(events[i].PaymentKey)
			if paymentKey == "" {
				paymentKey = candidateKey
			} else if candidateKey != paymentKey {
				return errors.New("Toss cancellation event batch spans multiple payments")
			}
		}
		if allExact && paymentKey != "" {
			var cumulativeCount int64
			if err := tx.Model(&TossPaymentEvent{}).
				Where("payment_key = ? AND event_type = ? AND cancel_amount_cumulative = ?", paymentKey, TossPaymentEventTypeCancellation, true).
				Count(&cumulativeCount).Error; err != nil {
				return err
			}
			if cumulativeCount > 0 {
				first := *events[0]
				expectedCanceled := first.OriginalAmount - first.BalanceAmount
				if first.OriginalAmount <= 0 || first.BalanceAmount < 0 || first.BalanceAmount > first.OriginalAmount || expectedCanceled <= 0 {
					return errors.New("invalid cumulative Toss cancellation snapshot")
				}
				seed := strings.Join([]string{
					paymentKey,
					strconv.FormatInt(first.OriginalAmount, 10),
					strconv.FormatInt(first.BalanceAmount, 10),
					strings.TrimSpace(first.Status),
				}, "\x00")
				first.EventKey = "toss_cancel_cumulative_" + common.Sha1([]byte(seed))
				first.TransactionKey = ""
				first.CancelAmount = expectedCanceled
				first.CancelAmountCumulative = true
				events = []*TossPaymentEvent{&first}
			}
		}

		for _, event := range events {
			if event == nil || strings.TrimSpace(event.EventKey) == "" || event.EventType != TossPaymentEventTypeCancellation || strings.TrimSpace(event.OrderId) != orderID {
				return errors.New("invalid Toss cancellation event")
			}
			if strings.TrimSpace(event.ReconciliationStatus) == "" {
				event.ReconciliationStatus = TossReconciliationStatusRequired
			}
			if event.CreateTime <= 0 {
				event.CreateTime = common.GetTimestamp()
			}
			createdDelta := event.CancelAmount
			if event.CancelAmountCumulative {
				expectedCanceled := event.OriginalAmount - event.BalanceAmount
				if event.OriginalAmount <= 0 || event.BalanceAmount < 0 || event.BalanceAmount > event.OriginalAmount || expectedCanceled <= 0 || event.CancelAmount != expectedCanceled {
					return errors.New("invalid cumulative Toss cancellation snapshot")
				}
				var snapshots []struct {
					OriginalAmount int64
					BalanceAmount  int64
				}
				if err := tx.Model(&TossPaymentEvent{}).
					Select("original_amount", "balance_amount").
					Where("payment_key = ? AND event_type = ?", event.PaymentKey, TossPaymentEventTypeCancellation).
					Find(&snapshots).Error; err != nil {
					return err
				}
				previousCanceled := int64(0)
				for i := range snapshots {
					if snapshots[i].OriginalAmount <= 0 || snapshots[i].BalanceAmount < 0 || snapshots[i].BalanceAmount > snapshots[i].OriginalAmount {
						continue
					}
					canceled := snapshots[i].OriginalAmount - snapshots[i].BalanceAmount
					if canceled > previousCanceled {
						previousCanceled = canceled
					}
				}
				if previousCanceled > expectedCanceled {
					return errors.New("Toss cumulative canceled amount moved backwards")
				}
				createdDelta = expectedCanceled - previousCanceled
				if createdDelta == 0 {
					continue
				}
			}
			event.CreateToken = common.GetUUID()
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "event_key"}},
				DoNothing: true,
			}).Create(event).Error; err != nil {
				return err
			}
			var stored TossPaymentEvent
			if err := tx.Where("event_key = ?", event.EventKey).First(&stored).Error; err != nil {
				return err
			}
			if stored.CreateToken != "" && stored.CreateToken == event.CreateToken {
				createdCount++
				createdAmount += createdDelta
			} else if !sameTossPaymentEventEvidence(&stored, event) {
				return ErrTossPaymentEventKeyConflict
			}
		}
		if walletPolicy != nil {
			if _, err := haltWalletAutoRechargeForRecordedCancellationTx(tx, walletPolicy, false); err != nil {
				return err
			}
		}
		return nil
	})
	return createdCount, createdAmount, err
}

// TossPaymentEvent is a durable, idempotent record of provider-side payment
// changes that require local reconciliation. In particular, quota or
// subscription reversal cannot safely happen inline for partial cancellations,
// so the webhook must persist the exact cancellation transaction before it is
// acknowledged.
type TossPaymentEvent struct {
	Id                     int    `json:"id"`
	EventKey               string `json:"event_key" gorm:"type:varchar(64);uniqueIndex"`
	EventType              string `json:"event_type" gorm:"type:varchar(32);index"`
	OrderId                string `json:"order_id" gorm:"type:varchar(64);index"`
	PaymentKey             string `json:"payment_key" gorm:"type:varchar(200)"`
	Status                 string `json:"status" gorm:"type:varchar(32);index"`
	TransactionKey         string `json:"transaction_key" gorm:"type:varchar(64);index"`
	CancelAmount           int64  `json:"cancel_amount"`
	CancelAmountCumulative bool   `json:"cancel_amount_cumulative" gorm:"default:false"`
	BalanceAmount          int64  `json:"balance_amount"`
	OriginalAmount         int64  `json:"original_amount"`
	// RefundOperation* pins an ambiguous provider cancellation to one
	// Idempotency-Key for the full Toss retention window. These fields are used
	// only by the stable refund_required fence row.
	RefundOperationKey     string `json:"-" gorm:"type:varchar(300);default:''"`
	RefundOperationTime    int64  `json:"-" gorm:"default:0;index"`
	RefundOperationBalance int64  `json:"-" gorm:"default:0"`
	// See TopUp.ProviderPayload. Cancellation audit records must retain the
	// complete provider response, including payloads larger than MySQL TEXT.
	ProviderPayload      string `json:"-"`
	ReconciliationStatus string `json:"reconciliation_status" gorm:"type:varchar(32);index"`
	CreateToken          string `json:"-" gorm:"type:varchar(64)"`
	CreateTime           int64  `json:"create_time" gorm:"index"`
	ResolutionNote       string `json:"resolution_note" gorm:"type:text"`
	ResolvedTime         int64  `json:"resolved_time" gorm:"default:0;index"`
	ResolvedBy           int    `json:"resolved_by" gorm:"default:0;index"`
}

// GetTossRecordedCanceledSnapshot returns the largest authoritative cumulative
// canceled amount already represented by events for a payment. It works for
// both exact transaction events and legacy cumulative snapshots because every
// event also stores the provider's original and remaining balance amounts.
func GetTossRecordedCanceledSnapshot(paymentKey string) (int64, error) {
	return GetTossRecordedCanceledSnapshotWithContext(context.Background(), paymentKey)
}

func GetTossRecordedCanceledSnapshotWithContext(ctx context.Context, paymentKey string) (int64, error) {
	paymentKey = strings.TrimSpace(paymentKey)
	if paymentKey == "" {
		return 0, errors.New("toss payment key is required")
	}
	var snapshots []struct {
		OriginalAmount int64
		BalanceAmount  int64
	}
	if err := dbWithContext(ctx).Model(&TossPaymentEvent{}).
		Select("original_amount", "balance_amount").
		Where("payment_key = ? AND event_type = ?", paymentKey, TossPaymentEventTypeCancellation).
		Find(&snapshots).Error; err != nil {
		return 0, err
	}
	var maximum int64
	for i := range snapshots {
		if snapshots[i].OriginalAmount <= 0 || snapshots[i].BalanceAmount < 0 || snapshots[i].BalanceAmount > snapshots[i].OriginalAmount {
			continue
		}
		canceled := snapshots[i].OriginalAmount - snapshots[i].BalanceAmount
		if canceled > maximum {
			maximum = canceled
		}
	}
	return maximum, nil
}

type TossPaymentEventFilter struct {
	ReconciliationStatus string
	EventType            string
	OrderId              string
}

func ListTossPaymentEvents(filter TossPaymentEventFilter, offset, limit int) ([]TossPaymentEvent, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = common.ItemsPerPage
	}
	if limit > 100 {
		limit = 100
	}
	query := DB.Model(&TossPaymentEvent{})
	if status := strings.TrimSpace(filter.ReconciliationStatus); status != "" {
		query = query.Where("reconciliation_status = ?", status)
	}
	if eventType := strings.TrimSpace(filter.EventType); eventType != "" {
		query = query.Where("event_type = ?", eventType)
	}
	if orderId := strings.TrimSpace(filter.OrderId); orderId != "" {
		query = query.Where("order_id = ?", orderId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []TossPaymentEvent
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

func GetTossPaymentEventById(id int) (*TossPaymentEvent, error) {
	if id <= 0 {
		return nil, ErrTossPaymentEventNotFound
	}
	var event TossPaymentEvent
	if err := DB.Where("id = ?", id).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTossPaymentEventNotFound
		}
		return nil, err
	}
	return &event, nil
}

// ResolveTossPaymentEventByAdmin records a manual operational decision without
// attempting any automatic balance or subscription reversal. Repeated requests
// are idempotent and preserve the first resolver's audit metadata.
func ResolveTossPaymentEventByAdmin(id, adminId int, resolutionNote string) (*TossPaymentEvent, error) {
	resolutionNote = strings.TrimSpace(resolutionNote)
	if id <= 0 {
		return nil, ErrTossPaymentEventNotFound
	}
	if adminId <= 0 {
		return nil, errors.New("toss payment event resolver is required")
	}
	if resolutionNote == "" {
		return nil, errors.New("toss payment event resolution note is required")
	}
	var candidate TossPaymentEvent
	if err := DB.Select("id", "event_type", "order_id").Where("id = ?", id).First(&candidate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTossPaymentEventNotFound
		}
		return nil, err
	}
	if candidate.EventType == TossPaymentEventTypeRefundRequired {
		// This row is an active settlement fence, not a note-only work item.
		// It can be resolved only by FinalizeFullyRefundedTossTopUp after durable
		// full-cancellation evidence exists; clearing it manually could let a later
		// callback grant quota for the payment the operator intended to refund.
		return nil, ErrTossPaymentEventStatusInvalid
	}
	if (candidate.EventType == TossPaymentEventTypeCancellation || candidate.EventType == TossPaymentEventTypeFinancialMismatch) &&
		DB.Migrator().HasTable(&TopUp{}) && DB.Migrator().HasTable(&WalletAutoRecharge{}) {
		policy, walletEvent, err := GetWalletAutoRechargeByChargeTradeNo(candidate.OrderId)
		if err != nil {
			return nil, err
		}
		if walletEvent {
			return resolveWalletAutoRechargePaymentEventByAdmin(id, policy.Id, adminId, resolutionNote)
		}
	}
	result := DB.Model(&TossPaymentEvent{}).
		Where("id = ? AND reconciliation_status = ?", id, TossReconciliationStatusRequired).
		Updates(map[string]interface{}{
			"reconciliation_status": TossReconciliationStatusResolved,
			"resolution_note":       resolutionNote,
			"resolved_time":         common.GetTimestamp(),
			"resolved_by":           adminId,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	event, err := GetTossPaymentEventById(id)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected == 0 && event.ReconciliationStatus != TossReconciliationStatusResolved {
		return nil, ErrTossPaymentEventStatusInvalid
	}
	return event, nil
}

func ResolveTossPaymentEvents(orderId, eventType string) error {
	return ResolveTossPaymentEventsWithContext(context.Background(), orderId, eventType)
}

func ResolveTossPaymentEventsWithContext(ctx context.Context, orderId, eventType string) error {
	orderId = strings.TrimSpace(orderId)
	if orderId == "" {
		return errors.New("toss payment event order id is required")
	}
	// Only a successful local credit/entitlement settlement may automatically
	// close a paid_pending_fulfillment retry. Cancellation and financial
	// mismatch evidence always requires an explicit audited admin decision.
	if strings.TrimSpace(eventType) != TossPaymentEventTypeFulfillment {
		return errors.New("only Toss fulfillment events can be automatically resolved")
	}
	query := dbWithContext(ctx).Model(&TossPaymentEvent{}).
		Where("order_id = ? AND reconciliation_status = ? AND event_type = ?", orderId, TossReconciliationStatusRequired, TossPaymentEventTypeFulfillment)
	return query.Updates(map[string]interface{}{
		"reconciliation_status": TossReconciliationStatusResolved,
		"resolution_note":       "automatically reconciled",
		"resolved_time":         common.GetTimestamp(),
		"resolved_by":           0,
	}).Error
}

// RecordTossPaymentEvent inserts a provider event once. The event key must be
// stable across webhook redelivery; a duplicate is a successful no-op.
func RecordTossPaymentEvent(event *TossPaymentEvent) (created bool, err error) {
	return RecordTossPaymentEventWithContext(context.Background(), event)
}

func RecordTossPaymentEventWithContext(ctx context.Context, event *TossPaymentEvent) (created bool, err error) {
	db := dbWithContext(ctx)
	if event != nil && (event.EventType == TossPaymentEventTypeCancellation || event.EventType == TossPaymentEventTypeFinancialMismatch) &&
		strings.TrimSpace(event.OrderId) != "" && db.Migrator().HasTable(&TopUp{}) && db.Migrator().HasTable(&WalletAutoRecharge{}) {
		policy, walletOrder, mapErr := GetWalletAutoRechargeByChargeTradeNoWithContext(ctx, event.OrderId)
		if mapErr != nil {
			return false, mapErr
		}
		if walletOrder {
			if event.EventType == TossPaymentEventTypeCancellation {
				return recordWalletAutoRechargeCancellationEventWithContext(ctx, policy.Id, event)
			}
			return recordWalletAutoRechargeFinancialEventWithContext(ctx, policy.Id, event)
		}
	}
	return recordTossPaymentEventDB(db, event)
}

func recordTossPaymentEventDB(db *gorm.DB, event *TossPaymentEvent) (created bool, err error) {
	if db == nil {
		return false, errors.New("toss payment event database is unavailable")
	}
	if event == nil || strings.TrimSpace(event.EventKey) == "" {
		return false, errors.New("toss payment event key is required")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return false, errors.New("toss payment event type is required")
	}
	if strings.TrimSpace(event.ReconciliationStatus) == "" {
		event.ReconciliationStatus = TossReconciliationStatusRequired
	}
	if event.CreateTime <= 0 {
		event.CreateTime = common.GetTimestamp()
	}
	// A per-attempt marker makes the created/no-op result independent of
	// MySQL CLIENT_FOUND_ROWS behavior for ON DUPLICATE KEY UPDATE.
	event.CreateToken = common.GetUUID()
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_key"}},
		DoNothing: true,
	}).Create(event)
	if result.Error != nil {
		return false, result.Error
	}
	var stored TossPaymentEvent
	if err := db.Where("event_key = ?", event.EventKey).First(&stored).Error; err != nil {
		return false, err
	}
	if stored.CreateToken != "" && stored.CreateToken == event.CreateToken {
		return true, nil
	}
	if !sameTossPaymentEventEvidence(&stored, event) {
		return false, ErrTossPaymentEventKeyConflict
	}
	return false, nil
}

// RecordTossTopUpRefundRequirementWithContext installs the durable refund
// fence while holding the same TopUp row lock used by settlement. Once this
// commits, no later callback can credit the order before the provider refund.
// The TopUp is moved out of pending while the active event remains the source
// of truth for the provider-reconciliation scheduler. This also keeps a rolling
// deployment safe from an older binary that does not know about this fence.
func RecordTossTopUpRefundRequirementWithContext(ctx context.Context, event *TossPaymentEvent) (created bool, err error) {
	if event == nil || event.EventType != TossPaymentEventTypeRefundRequired {
		return false, errors.New("invalid Toss top-up refund requirement")
	}
	event.OrderId = strings.TrimSpace(event.OrderId)
	event.PaymentKey = strings.TrimSpace(event.PaymentKey)
	if event.EventKey != TossTopUpRefundRequiredEventKey(event.OrderId, event.PaymentKey) ||
		event.OrderId == "" || event.PaymentKey == "" || event.OriginalAmount <= 0 ||
		event.BalanceAmount < 0 || event.BalanceAmount > event.OriginalAmount {
		return false, errors.New("invalid Toss top-up refund evidence")
	}
	err = dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", event.OrderId).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		if !strings.HasPrefix(topUp.TradeNo, "toss_") || topUp.WalletOrderIdVersion != 0 ||
			topUp.WalletAutoRechargeId != nil || topUp.WalletAutoRechargeCycleKey != nil || topUp.WalletAutoRechargeAttempt != nil {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusFailed &&
			topUp.Status != common.TopUpStatusExpired && topUp.Status != TossTopUpStatusRefundPending {
			return ErrTopUpStatusInvalid
		}
		if topUp.Amount != event.OriginalAmount {
			return errors.New("Toss refund amount does not match local top-up")
		}
		if topUp.ProviderOrderId != "" && topUp.ProviderOrderId != topUp.TradeNo && topUp.ProviderOrderId != event.PaymentKey {
			return ErrTossPaymentKeyConflict
		}
		created, err = recordTossPaymentEventDB(tx, event)
		if err != nil {
			return err
		}
		var stored TossPaymentEvent
		if err := tx.Select("reconciliation_status").Where("event_key = ?", event.EventKey).First(&stored).Error; err != nil {
			return err
		}
		if stored.ReconciliationStatus != TossReconciliationStatusRequired {
			// A stale authenticated DONE observation can race a completed refund.
			// Both paths lock this TopUp first, so a resolved fence paired with a
			// terminal row proves the refund finalizer already won. Never reopen it
			// after the durable fence has been cleared.
			if topUp.Status == common.TopUpStatusFailed || topUp.Status == common.TopUpStatusExpired {
				created = false
				return nil
			}
			return ErrTopUpStatusInvalid
		}
		needsUpdate := topUp.ProviderOrderId != event.PaymentKey ||
			topUp.Status != TossTopUpStatusRefundPending || topUp.ProviderRetryTime <= 0
		if needsUpdate {
			now := getDBTimestampTx(tx)
			updates := map[string]interface{}{
				"provider_order_id":    event.PaymentKey,
				"provider_retry_time":  now,
				"provider_claim_token": "",
				"provider_claim_time":  0,
			}
			if topUp.ProviderOrderId != event.PaymentKey {
				updates["provider_order_time"] = now
			}
			if topUp.Status != TossTopUpStatusRefundPending {
				updates["status"] = TossTopUpStatusRefundPending
			}
			if topUp.Status == common.TopUpStatusPending {
				updates["complete_time"] = now
			}
			result := tx.Model(&TopUp{}).
				Where("id = ? AND status = ?", topUp.Id, topUp.Status).
				Where("provider_order_id = '' OR provider_order_id IS NULL OR provider_order_id = trade_no OR provider_order_id = ?", event.PaymentKey).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTopUpStatusInvalid
			}
		}
		return nil
	})
	return created, err
}

// FinalizeUnfundedTossTopUpRefundRequirementWithContext closes a refund-fenced
// virtual-account order after an authenticated provider lookup proves that the
// payment expired or was aborted before any money moved. It is deliberately
// separate from the cancellation finalizer because there is no refund POST or
// cancellation transaction to persist in this state.
func FinalizeUnfundedTossTopUpRefundRequirementWithContext(ctx context.Context, orderID, paymentKey, providerStatus, providerPayload string) error {
	orderID = strings.TrimSpace(orderID)
	paymentKey = strings.TrimSpace(paymentKey)
	providerStatus = strings.ToUpper(strings.TrimSpace(providerStatus))
	if orderID == "" || paymentKey == "" || (providerStatus != "EXPIRED" && providerStatus != "ABORTED") {
		return errors.New("invalid unfunded Toss top-up terminal evidence")
	}
	targetStatus := common.TopUpStatusFailed
	if providerStatus == "EXPIRED" {
		targetStatus = common.TopUpStatusExpired
	}
	return dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", orderID).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		if !strings.HasPrefix(topUp.TradeNo, "toss_") || topUp.WalletOrderIdVersion != 0 ||
			topUp.WalletAutoRechargeId != nil || topUp.WalletAutoRechargeCycleKey != nil || topUp.WalletAutoRechargeAttempt != nil {
			return ErrPaymentMethodMismatch
		}
		if topUp.ProviderOrderId != paymentKey || topUp.Status == common.TopUpStatusSuccess {
			return ErrTopUpStatusInvalid
		}
		var refundRequirementCount int64
		if err := tx.Model(&TossPaymentEvent{}).
			Where("order_id = ? AND payment_key = ? AND event_type = ? AND original_amount = ? AND reconciliation_status = ?",
				orderID, paymentKey, TossPaymentEventTypeRefundRequired, topUp.Amount, TossReconciliationStatusRequired).
			Limit(1).Count(&refundRequirementCount).Error; err != nil {
			return err
		}
		if refundRequirementCount == 0 {
			return errors.New("Toss refund requirement evidence is unavailable")
		}
		if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusFailed &&
			topUp.Status != common.TopUpStatusExpired && topUp.Status != TossTopUpStatusRefundPending {
			return ErrTopUpStatusInvalid
		}
		updates := map[string]interface{}{
			"status":               targetStatus,
			"complete_time":        getDBTimestampTx(tx),
			"provider_attempted":   false,
			"provider_claim_token": "",
			"provider_claim_time":  0,
		}
		if strings.TrimSpace(providerPayload) != "" {
			updates["provider_payload"] = providerPayload
		}
		result := tx.Model(&TopUp{}).
			Where("id = ? AND status = ? AND provider_order_id = ?", topUp.Id, topUp.Status, paymentKey).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var current TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&current).Error; err != nil {
				return err
			}
			if current.Status != targetStatus || current.ProviderOrderId != paymentKey {
				return ErrTopUpStatusInvalid
			}
		}
		now := getDBTimestampTx(tx)
		return tx.Model(&TossPaymentEvent{}).
			Where("order_id = ? AND payment_key = ? AND event_type = ? AND reconciliation_status = ?",
				orderID, paymentKey, TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired).
			Updates(map[string]interface{}{
				"reconciliation_status": TossReconciliationStatusResolved,
				"resolution_note":       "authenticated payment expired before funds moved",
				"resolved_time":         now,
				"resolved_by":           0,
			}).Error
	})
}

// FinalizeFullyRefundedTossTopUpWithContext closes an uncredited order only
// after a full authoritative cancellation event is already durable. It also
// resolves the related reconciliation records because no quota was granted.
func FinalizeFullyRefundedTossTopUpWithContext(ctx context.Context, orderID, paymentKey, providerPayload string) error {
	orderID = strings.TrimSpace(orderID)
	paymentKey = strings.TrimSpace(paymentKey)
	if orderID == "" || paymentKey == "" {
		return errors.New("Toss refunded top-up identity is required")
	}
	return dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("trade_no = ?", orderID).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderToss || topUp.PaymentMethod != PaymentMethodToss {
			return ErrPaymentMethodMismatch
		}
		if !strings.HasPrefix(topUp.TradeNo, "toss_") || topUp.WalletOrderIdVersion != 0 ||
			topUp.WalletAutoRechargeId != nil || topUp.WalletAutoRechargeCycleKey != nil || topUp.WalletAutoRechargeAttempt != nil {
			return ErrPaymentMethodMismatch
		}
		if topUp.ProviderOrderId != paymentKey {
			return ErrTossPaymentKeyConflict
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return ErrTopUpStatusInvalid
		}
		var refundRequirementCount int64
		if err := tx.Model(&TossPaymentEvent{}).
			Where("order_id = ? AND payment_key = ? AND event_type = ? AND reconciliation_status = ?",
				orderID, paymentKey, TossPaymentEventTypeRefundRequired, TossReconciliationStatusRequired).
			Limit(1).Count(&refundRequirementCount).Error; err != nil {
			return err
		}
		if refundRequirementCount == 0 {
			return errors.New("Toss refund requirement evidence is unavailable")
		}
		var fullCancellationCount int64
		if err := tx.Model(&TossPaymentEvent{}).
			Where("order_id = ? AND payment_key = ? AND event_type = ? AND status = ? AND original_amount = ? AND balance_amount = 0",
				orderID, paymentKey, TossPaymentEventTypeCancellation, "CANCELED", topUp.Amount).
			Limit(1).Count(&fullCancellationCount).Error; err != nil {
			return err
		}
		if fullCancellationCount == 0 {
			return errors.New("full Toss cancellation evidence is unavailable")
		}
		if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusFailed &&
			topUp.Status != common.TopUpStatusExpired && topUp.Status != TossTopUpStatusRefundPending {
			return ErrTopUpStatusInvalid
		}
		updates := map[string]interface{}{
			"status":               common.TopUpStatusFailed,
			"complete_time":        getDBTimestampTx(tx),
			"provider_claim_token": "",
			"provider_claim_time":  0,
			"provider_attempted":   false,
		}
		if strings.TrimSpace(providerPayload) != "" {
			updates["provider_payload"] = providerPayload
		}
		result := tx.Model(&TopUp{}).
			Where("id = ? AND status = ? AND provider_order_id = ?", topUp.Id, topUp.Status, paymentKey).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var current TopUp
			if err := tx.Where("id = ?", topUp.Id).First(&current).Error; err != nil {
				return err
			}
			if current.Status != common.TopUpStatusFailed || current.ProviderOrderId != paymentKey {
				return ErrTopUpStatusInvalid
			}
		}
		now := getDBTimestampTx(tx)
		return tx.Model(&TossPaymentEvent{}).
			Where("order_id = ? AND payment_key = ? AND reconciliation_status = ? AND event_type IN ?", orderID, paymentKey, TossReconciliationStatusRequired, []string{
				TossPaymentEventTypeRefundRequired,
				TossPaymentEventTypeFinancialMismatch,
				TossPaymentEventTypeFulfillment,
				TossPaymentEventTypeCancellation,
			}).
			Updates(map[string]interface{}{
				"reconciliation_status": TossReconciliationStatusResolved,
				"resolution_note":       "automatically fully refunded before local credit",
				"resolved_time":         now,
				"resolved_by":           0,
			}).Error
	})
}
