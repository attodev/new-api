package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const subscriptionOrderPlanSnapshotVersion = 1

var (
	ErrSubscriptionPlanSnapshotInvalid  = errors.New("subscription plan snapshot is invalid")
	ErrSubscriptionPlanSnapshotMismatch = errors.New("subscription plan snapshot does not match order")
	ErrTossRenewalContractUnavailable   = errors.New("Toss renewal contract snapshot is unavailable")
)

type TossRenewalContract struct {
	Plan             *SubscriptionPlan
	Money            float64
	ProviderAmount   int64
	ProviderCurrency string
}

// subscriptionOrderPlanSnapshot freezes both the purchased plan terms and the
// provider-denominated amount. Binding it to TradeNo prevents a valid snapshot
// from one order being copied onto another order with the same plan and price.
type subscriptionOrderPlanSnapshot struct {
	Version          int     `json:"version"`
	TradeNo          string  `json:"trade_no"`
	PlanId           int     `json:"plan_id"`
	Title            string  `json:"title"`
	PriceAmount      float64 `json:"price_amount"`
	Currency         string  `json:"currency"`
	DurationUnit     string  `json:"duration_unit"`
	DurationValue    int     `json:"duration_value"`
	CustomSeconds    int64   `json:"custom_seconds"`
	MaxPurchaseCount int     `json:"max_purchase_per_user"`
	UpgradeGroup     string  `json:"upgrade_group"`
	TotalAmount      int64   `json:"total_amount"`
	ResetPeriod      string  `json:"quota_reset_period"`
	ResetSeconds     int64   `json:"quota_reset_custom_seconds"`
	ProviderAmount   int64   `json:"provider_amount"`
	ProviderCurrency string  `json:"provider_currency"`
}

func subscriptionPricesEqual(left, right float64) bool {
	if math.IsNaN(left) || math.IsInf(left, 0) || math.IsNaN(right) || math.IsInf(right, 0) {
		return false
	}
	return decimal.NewFromFloat(left).Equal(decimal.NewFromFloat(right))
}

func validateSubscriptionSnapshotPlan(plan *SubscriptionPlan) error {
	if plan == nil || plan.Id <= 0 {
		return fmt.Errorf("%w: invalid plan id", ErrSubscriptionPlanSnapshotInvalid)
	}
	if !subscriptionPricesEqual(plan.PriceAmount, plan.PriceAmount) || plan.PriceAmount <= 0 {
		return fmt.Errorf("%w: invalid price", ErrSubscriptionPlanSnapshotInvalid)
	}
	if strings.TrimSpace(plan.Currency) == "" {
		return fmt.Errorf("%w: currency is empty", ErrSubscriptionPlanSnapshotInvalid)
	}
	if plan.MaxPurchasePerUser < 0 || plan.TotalAmount < 0 {
		return fmt.Errorf("%w: invalid quota or purchase limit", ErrSubscriptionPlanSnapshotInvalid)
	}
	if err := ValidateSubscriptionPlanTiming(plan); err != nil {
		return fmt.Errorf("%w: %v", ErrSubscriptionPlanSnapshotInvalid, err)
	}
	resetPeriod := strings.TrimSpace(plan.QuotaResetPeriod)
	if resetPeriod != "" && resetPeriod != SubscriptionResetNever && resetPeriod != SubscriptionResetDaily &&
		resetPeriod != SubscriptionResetWeekly && resetPeriod != SubscriptionResetMonthly && resetPeriod != SubscriptionResetCustom {
		return fmt.Errorf("%w: invalid reset period", ErrSubscriptionPlanSnapshotInvalid)
	}
	return nil
}

func planFromSubscriptionOrderSnapshot(snapshot *subscriptionOrderPlanSnapshot) *SubscriptionPlan {
	if snapshot == nil {
		return nil
	}
	return &SubscriptionPlan{
		Id:                      snapshot.PlanId,
		Title:                   snapshot.Title,
		PriceAmount:             snapshot.PriceAmount,
		Currency:                snapshot.Currency,
		DurationUnit:            snapshot.DurationUnit,
		DurationValue:           snapshot.DurationValue,
		CustomSeconds:           snapshot.CustomSeconds,
		MaxPurchasePerUser:      snapshot.MaxPurchaseCount,
		UpgradeGroup:            snapshot.UpgradeGroup,
		TotalAmount:             snapshot.TotalAmount,
		QuotaResetPeriod:        snapshot.ResetPeriod,
		QuotaResetCustomSeconds: snapshot.ResetSeconds,
	}
}

func validateSubscriptionOrderPlanSnapshot(order *SubscriptionOrder, snapshot *subscriptionOrderPlanSnapshot) (*SubscriptionPlan, error) {
	if order == nil || snapshot == nil {
		return nil, fmt.Errorf("%w: missing order or snapshot", ErrSubscriptionPlanSnapshotInvalid)
	}
	if snapshot.Version != subscriptionOrderPlanSnapshotVersion {
		return nil, fmt.Errorf("%w: unsupported version", ErrSubscriptionPlanSnapshotInvalid)
	}
	if strings.TrimSpace(order.TradeNo) == "" || snapshot.TradeNo != order.TradeNo {
		return nil, fmt.Errorf("%w: trade number", ErrSubscriptionPlanSnapshotMismatch)
	}
	if order.PlanId <= 0 || snapshot.PlanId != order.PlanId {
		return nil, fmt.Errorf("%w: plan id", ErrSubscriptionPlanSnapshotMismatch)
	}
	if !subscriptionPricesEqual(snapshot.PriceAmount, order.Money) {
		return nil, fmt.Errorf("%w: order price", ErrSubscriptionPlanSnapshotMismatch)
	}
	if snapshot.ProviderAmount <= 0 || order.ProviderAmount != snapshot.ProviderAmount {
		return nil, fmt.Errorf("%w: provider amount", ErrSubscriptionPlanSnapshotMismatch)
	}
	if !strings.EqualFold(strings.TrimSpace(snapshot.ProviderCurrency), "KRW") ||
		!strings.EqualFold(strings.TrimSpace(order.ProviderCurrency), snapshot.ProviderCurrency) {
		return nil, fmt.Errorf("%w: provider currency", ErrSubscriptionPlanSnapshotMismatch)
	}
	plan := planFromSubscriptionOrderSnapshot(snapshot)
	if err := validateSubscriptionSnapshotPlan(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// SetTossSubscriptionOrderPlanSnapshot writes a snapshot only when the order
// does not already have one. Existing snapshots are validated, never replaced.
func SetTossSubscriptionOrderPlanSnapshot(order *SubscriptionOrder, plan *SubscriptionPlan) error {
	if order == nil {
		return fmt.Errorf("%w: order is nil", ErrSubscriptionPlanSnapshotInvalid)
	}
	if strings.TrimSpace(order.PlanSnapshot) != "" {
		_, err := ResolveTossSubscriptionOrderPlan(order)
		return err
	}
	if err := validateSubscriptionSnapshotPlan(plan); err != nil {
		return err
	}
	if plan.Id != order.PlanId || !subscriptionPricesEqual(plan.PriceAmount, order.Money) {
		return fmt.Errorf("%w: plan id or price", ErrSubscriptionPlanSnapshotMismatch)
	}
	if strings.TrimSpace(order.TradeNo) == "" || order.ProviderAmount <= 0 ||
		!strings.EqualFold(strings.TrimSpace(order.ProviderCurrency), "KRW") {
		return fmt.Errorf("%w: incomplete Toss order", ErrSubscriptionPlanSnapshotInvalid)
	}
	snapshot := subscriptionOrderPlanSnapshot{
		Version:          subscriptionOrderPlanSnapshotVersion,
		TradeNo:          order.TradeNo,
		PlanId:           plan.Id,
		Title:            plan.Title,
		PriceAmount:      plan.PriceAmount,
		Currency:         plan.Currency,
		DurationUnit:     plan.DurationUnit,
		DurationValue:    plan.DurationValue,
		CustomSeconds:    plan.CustomSeconds,
		MaxPurchaseCount: plan.MaxPurchasePerUser,
		UpgradeGroup:     plan.UpgradeGroup,
		TotalAmount:      plan.TotalAmount,
		ResetPeriod:      plan.QuotaResetPeriod,
		ResetSeconds:     plan.QuotaResetCustomSeconds,
		ProviderAmount:   order.ProviderAmount,
		ProviderCurrency: strings.ToUpper(strings.TrimSpace(order.ProviderCurrency)),
	}
	payload, err := common.Marshal(&snapshot)
	if err != nil {
		return fmt.Errorf("%w: encode: %v", ErrSubscriptionPlanSnapshotInvalid, err)
	}
	order.PlanSnapshot = string(payload)
	return nil
}

// ResolveTossSubscriptionOrderPlan returns immutable terms for snapshotted
// orders. Rows created before snapshots were introduced fall back to the
// current plan only when its identity and price still match the order.
func ResolveTossSubscriptionOrderPlan(order *SubscriptionOrder) (*SubscriptionPlan, error) {
	return resolveSubscriptionOrderPlanTx(nil, order)
}

func ResolveTossSubscriptionOrderPlanWithContext(ctx context.Context, order *SubscriptionOrder) (*SubscriptionPlan, error) {
	return resolveSubscriptionOrderPlanTx(dbWithContext(ctx), order)
}

func resolveSubscriptionOrderPlanTx(tx *gorm.DB, order *SubscriptionOrder) (*SubscriptionPlan, error) {
	if order == nil {
		return nil, fmt.Errorf("%w: order is nil", ErrSubscriptionPlanSnapshotInvalid)
	}
	if strings.TrimSpace(order.PlanSnapshot) != "" {
		var snapshot subscriptionOrderPlanSnapshot
		if err := common.Unmarshal([]byte(order.PlanSnapshot), &snapshot); err != nil {
			return nil, fmt.Errorf("%w: decode: %v", ErrSubscriptionPlanSnapshotInvalid, err)
		}
		return validateSubscriptionOrderPlanSnapshot(order, &snapshot)
	}

	plan, err := getSubscriptionPlanByIdTx(tx, order.PlanId)
	if err != nil {
		return nil, err
	}
	if err := validateSubscriptionSnapshotPlan(plan); err != nil {
		return nil, err
	}
	if !subscriptionPricesEqual(plan.PriceAmount, order.Money) {
		return nil, fmt.Errorf("%w: legacy order price", ErrSubscriptionPlanSnapshotMismatch)
	}
	return plan, nil
}

func decodeTossRenewalContractSnapshot(sub *UserSubscription) (*subscriptionOrderPlanSnapshot, error) {
	if sub == nil || sub.Id <= 0 || sub.PlanId <= 0 || strings.TrimSpace(sub.TossRenewalContractSnapshot) == "" {
		return nil, ErrTossRenewalContractUnavailable
	}
	var snapshot subscriptionOrderPlanSnapshot
	if err := common.Unmarshal([]byte(sub.TossRenewalContractSnapshot), &snapshot); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrSubscriptionPlanSnapshotInvalid, err)
	}
	if snapshot.Version != subscriptionOrderPlanSnapshotVersion || snapshot.PlanId != sub.PlanId || strings.TrimSpace(snapshot.TradeNo) == "" {
		return nil, fmt.Errorf("%w: renewal contract identity", ErrSubscriptionPlanSnapshotMismatch)
	}
	if snapshot.ProviderAmount <= 0 || !IsTossCardAmountPayableKRW(snapshot.ProviderAmount) ||
		!strings.EqualFold(strings.TrimSpace(snapshot.ProviderCurrency), "KRW") {
		return nil, fmt.Errorf("%w: renewal provider amount", ErrSubscriptionPlanSnapshotInvalid)
	}
	plan := planFromSubscriptionOrderSnapshot(&snapshot)
	if err := validateSubscriptionSnapshotPlan(plan); err != nil {
		return nil, err
	}
	if !subscriptionPricesEqual(snapshot.PriceAmount, snapshot.PriceAmount) || snapshot.PriceAmount <= 0 {
		return nil, fmt.Errorf("%w: renewal price", ErrSubscriptionPlanSnapshotInvalid)
	}
	return &snapshot, nil
}

func ResolveTossRenewalContract(sub *UserSubscription) (*TossRenewalContract, error) {
	snapshot, err := decodeTossRenewalContractSnapshot(sub)
	if err != nil {
		return nil, err
	}
	return &TossRenewalContract{
		Plan:             planFromSubscriptionOrderSnapshot(snapshot),
		Money:            snapshot.PriceAmount,
		ProviderAmount:   snapshot.ProviderAmount,
		ProviderCurrency: strings.ToUpper(strings.TrimSpace(snapshot.ProviderCurrency)),
	}, nil
}

// SetTossRenewalContractFromInitialOrder copies the already-validated initial
// order snapshot to the subscription exactly once. The original trade number
// remains useful provenance; renewal orders are rebound to their own tradeNo.
func SetTossRenewalContractFromInitialOrder(sub *UserSubscription, order *SubscriptionOrder) error {
	if sub == nil || order == nil || sub.PlanId != order.PlanId || sub.UserId != order.UserId {
		return fmt.Errorf("%w: renewal contract owner or plan", ErrSubscriptionPlanSnapshotMismatch)
	}
	_, _, _, isRenewalOrder, identityErr := ResolveTossRenewalOrderIdentity(order)
	if identityErr != nil {
		return identityErr
	}
	if isRenewalOrder || strings.HasPrefix(strings.TrimSpace(order.TradeNo), TossRenewalTradeNoPrefix) ||
		HasTossRenewalOpaqueOrderIDPrefix(order.TradeNo) ||
		order.PaymentProvider != PaymentProviderToss || order.Status != common.TopUpStatusSuccess ||
		order.BillingKeyId <= 0 || sub.BillingKeyId != order.BillingKeyId {
		return fmt.Errorf("%w: initial Toss payment provenance", ErrSubscriptionPlanSnapshotMismatch)
	}
	if _, err := ResolveTossSubscriptionOrderPlan(order); err != nil {
		return err
	}
	if strings.TrimSpace(sub.TossRenewalContractSnapshot) != "" {
		existing, err := decodeTossRenewalContractSnapshot(sub)
		if err != nil {
			return err
		}
		incoming := subscriptionOrderPlanSnapshot{}
		if err := common.Unmarshal([]byte(order.PlanSnapshot), &incoming); err != nil {
			return fmt.Errorf("%w: decode initial contract: %v", ErrSubscriptionPlanSnapshotInvalid, err)
		}
		if *existing != incoming {
			return fmt.Errorf("%w: renewal contract is immutable", ErrSubscriptionPlanSnapshotMismatch)
		}
		return nil
	}
	sub.TossRenewalContractSnapshot = order.PlanSnapshot
	_, err := decodeTossRenewalContractSnapshot(sub)
	return err
}

func SetTossRenewalOrderSnapshotFromContract(order *SubscriptionOrder, sub *UserSubscription) error {
	contract, err := ResolveTossRenewalContract(sub)
	if err != nil {
		return err
	}
	if order == nil || order.PlanId != sub.PlanId || !subscriptionPricesEqual(order.Money, contract.Money) ||
		order.ProviderAmount != contract.ProviderAmount || !strings.EqualFold(strings.TrimSpace(order.ProviderCurrency), contract.ProviderCurrency) {
		return fmt.Errorf("%w: renewal order terms", ErrSubscriptionPlanSnapshotMismatch)
	}
	return SetTossSubscriptionOrderPlanSnapshot(order, contract.Plan)
}

func ValidateTossRenewalOrderMatchesContract(order *SubscriptionOrder, sub *UserSubscription) error {
	if order == nil || sub == nil || order.PlanId != sub.PlanId || order.UserId != sub.UserId {
		return fmt.Errorf("%w: renewal owner or plan", ErrSubscriptionPlanSnapshotMismatch)
	}
	contractSnapshot, err := decodeTossRenewalContractSnapshot(sub)
	if err != nil {
		return err
	}
	if _, err := ResolveTossSubscriptionOrderPlan(order); err != nil {
		return err
	}
	var orderSnapshot subscriptionOrderPlanSnapshot
	if err := common.Unmarshal([]byte(order.PlanSnapshot), &orderSnapshot); err != nil {
		return fmt.Errorf("%w: decode renewal order: %v", ErrSubscriptionPlanSnapshotInvalid, err)
	}
	contractSnapshot.TradeNo = orderSnapshot.TradeNo
	if *contractSnapshot != orderSnapshot {
		return fmt.Errorf("%w: renewal order differs from contract", ErrSubscriptionPlanSnapshotMismatch)
	}
	return nil
}
