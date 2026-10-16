package model

import (
	"errors"
	"fmt"
	"time"
)

// fork: O11 — personal subscription plan timing bounds (team 2c619a31e).
// The limits match the organization plan bounds in
// controller/organization_validation.go so neither path can build an end or
// reset time that overflows time.Duration or Unix seconds.
const (
	SubscriptionPlanMaxDurationValue       = 1200
	SubscriptionPlanMaxCustomSeconds int64 = 31_536_000
)

func ValidateSubscriptionPlanDuration(plan *SubscriptionPlan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	switch plan.DurationUnit {
	case SubscriptionDurationCustom:
		if plan.CustomSeconds <= 0 || plan.CustomSeconds > SubscriptionPlanMaxCustomSeconds {
			return fmt.Errorf("custom_seconds must be between 1 and %d", SubscriptionPlanMaxCustomSeconds)
		}
	case SubscriptionDurationYear, SubscriptionDurationMonth, SubscriptionDurationDay, SubscriptionDurationHour:
		if plan.DurationValue <= 0 || plan.DurationValue > SubscriptionPlanMaxDurationValue {
			return fmt.Errorf("duration_value must be between 1 and %d", SubscriptionPlanMaxDurationValue)
		}
		if plan.CustomSeconds < 0 || plan.CustomSeconds > SubscriptionPlanMaxCustomSeconds {
			return fmt.Errorf("custom_seconds must be between 0 and %d", SubscriptionPlanMaxCustomSeconds)
		}
	default:
		return fmt.Errorf("invalid duration_unit: %s", plan.DurationUnit)
	}
	return nil
}

func ValidateSubscriptionPlanReset(plan *SubscriptionPlan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	period := NormalizeResetPeriod(plan.QuotaResetPeriod)
	if period == SubscriptionResetCustom {
		if plan.QuotaResetCustomSeconds <= 0 || plan.QuotaResetCustomSeconds > SubscriptionPlanMaxCustomSeconds {
			return fmt.Errorf("quota_reset_custom_seconds must be between 1 and %d", SubscriptionPlanMaxCustomSeconds)
		}
		return nil
	}
	if plan.QuotaResetCustomSeconds < 0 || plan.QuotaResetCustomSeconds > SubscriptionPlanMaxCustomSeconds {
		return fmt.Errorf("quota_reset_custom_seconds must be between 0 and %d", SubscriptionPlanMaxCustomSeconds)
	}
	return nil
}

func ValidateSubscriptionPlanTiming(plan *SubscriptionPlan) error {
	if err := ValidateSubscriptionPlanDuration(plan); err != nil {
		return err
	}
	return ValidateSubscriptionPlanReset(plan)
}

// subscriptionEndAfterStart rejects an end time that did not move forward,
// which is how a Unix-seconds overflow near the time.Time limit shows up.
func subscriptionEndAfterStart(start, end time.Time) (int64, error) {
	if !end.After(start) || end.Unix() <= start.Unix() {
		return 0, errors.New("subscription plan end time overflowed")
	}
	return end.Unix(), nil
}

// subscriptionResetAfterBase returns 0 (no reset) when the next reset does
// not move forward or falls after the subscription end.
func subscriptionResetAfterBase(base, next time.Time, endUnix int64) int64 {
	if !next.After(base) || next.Unix() <= base.Unix() {
		return 0
	}
	if endUnix > 0 && next.Unix() > endUnix {
		return 0
	}
	return next.Unix()
}
