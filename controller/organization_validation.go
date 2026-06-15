package controller

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	MaxOrganizationNameLength                     = 64
	MaxOrganizationDescriptionLength              = 255
	MaxOrganizationRemarkLength                   = 255
	MaxOrganizationQuota                          = 1_000_000_000
	MaxOrganizationReferenceId                    = 1_000_000_000
	MaxOrganizationSubscriptionPlanTitleLength    = 128
	MaxOrganizationSubscriptionPlanSubtitleLength = 255
	MaxOrganizationSubscriptionDurationValue      = 1200
	MaxOrganizationSubscriptionCustomSeconds      = 31_536_000
	MaxOrganizationSubscriptionSortOrder          = 1_000_000

	OrganizationStatusAllowedError       = "status must be one of: 1, 2"
	OrganizationDurationValueRangeError  = "duration_value must be between 0 and 1200"
	OrganizationDurationUnitAllowedError = "duration_unit must be one of: year, month, day, hour, custom"
)

func trimAndValidateText(field string, value string, maxLength int, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(trimmed) > maxLength {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxLength)
	}
	return trimmed, nil
}

func validateIntRange(field string, value int, min int, max int) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d", field, min, max)
	}
	return nil
}

func validateInt64Range(field string, value int64, min int64, max int64) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d", field, min, max)
	}
	return nil
}

func validatePositiveReferenceId(field string, value int) error {
	if value <= 0 {
		return fmt.Errorf("%s must be greater than 0", field)
	}
	if value > MaxOrganizationReferenceId {
		return fmt.Errorf("%s must be at most %d", field, MaxOrganizationReferenceId)
	}
	return nil
}

func validateOrganizationStatus(status int) error {
	if status != model.OrganizationStatusEnabled && status != model.OrganizationStatusDisabled {
		return errors.New(OrganizationStatusAllowedError)
	}
	return nil
}

func validateUserStatus(status int) error {
	if status != common.UserStatusEnabled && status != common.UserStatusDisabled {
		return errors.New(OrganizationStatusAllowedError)
	}
	return nil
}

func validateOrganizationDurationUnit(durationUnit string) error {
	switch durationUnit {
	case model.SubscriptionDurationYear,
		model.SubscriptionDurationMonth,
		model.SubscriptionDurationDay,
		model.SubscriptionDurationHour,
		model.SubscriptionDurationCustom:
		return nil
	default:
		return errors.New(OrganizationDurationUnitAllowedError)
	}
}

func rejectTopUpAmountTooLarge(amount int64) error {
	if amount > int64(MaxOrganizationQuota) {
		return fmt.Errorf("amount cannot be greater than %d", MaxOrganizationQuota)
	}
	return nil
}
