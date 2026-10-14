package controller

import (
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	MaxOrganizationNameLength        = 64
	MaxOrganizationDescriptionLength = 255
	MaxOrganizationRemarkLength      = 255
	// Quota ceilings share the wallet bound with the model layer (fork: O3 —
	// common.MaxWalletQuota; the controller is the overflow guard).
	MaxOrganizationQuota                          int64 = int64(common.MaxWalletQuota)
	MaxOrganizationReferenceId                          = 1_000_000_000
	MaxOrganizationSubscriptionPlanTitleLength          = 128
	MaxOrganizationSubscriptionPlanSubtitleLength       = 255
	MaxOrganizationSubscriptionDurationValue            = 1200
	MaxOrganizationSubscriptionCustomSeconds            = 31_536_000
	MaxOrganizationSubscriptionSortOrder                = 1_000_000

	OrganizationStatusAllowedError       = "status must be one of: 1, 2"
	OrganizationMemberStatusAllowedError = "member_status must be one of: 1, 2"
	OrganizationDurationValueRangeError  = "duration_value must be between 0 and 1200"
	OrganizationDurationUnitAllowedError = "duration_unit must be one of: year, month, day, hour, custom"
)

func trimAndValidateText(field string, value string, maxLength int, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return "", common.NewMessage("{{field}} is required", map[string]any{"field": field})
	}
	if utf8.RuneCountInString(trimmed) > maxLength {
		return "", common.NewMessage("{{field}} must be at most {{max}} characters", map[string]any{"field": field, "max": maxLength})
	}
	return trimmed, nil
}

func validateIntRange(field string, value int, min int, max int) error {
	if value < min || value > max {
		return common.NewMessage("{{field}} must be between {{min}} and {{max}}", map[string]any{"field": field, "min": min, "max": max})
	}
	return nil
}

func validateInt64Range(field string, value int64, min int64, max int64) error {
	if value < min || value > max {
		return common.NewMessage("{{field}} must be between {{min}} and {{max}}", map[string]any{"field": field, "min": min, "max": max})
	}
	return nil
}

func validatePositiveReferenceId(field string, value int) error {
	if value <= 0 {
		return common.NewMessage("{{field}} must be greater than 0", map[string]any{"field": field})
	}
	if value > MaxOrganizationReferenceId {
		return common.NewMessage("{{field}} must be at most {{max}}", map[string]any{"field": field, "max": MaxOrganizationReferenceId})
	}
	return nil
}

func validateOrganizationStatus(status int) error {
	if status != model.OrganizationStatusEnabled && status != model.OrganizationStatusDisabled {
		return common.NewMessage(OrganizationStatusAllowedError)
	}
	return nil
}

func validateOrganizationMemberStatus(status int) error {
	if !model.IsValidOrganizationMemberStatus(status) {
		return common.NewMessage(OrganizationMemberStatusAllowedError)
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
		return common.NewMessage(OrganizationDurationUnitAllowedError)
	}
}
