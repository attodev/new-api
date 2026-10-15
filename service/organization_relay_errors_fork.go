package service

import (
	"errors"

	"github.com/QuantumNous/new-api/i18n"
)

// fork: O3 — organization billing errors returned to AI clients (relay
// BillingSession and legacy Midjourney) are rendered through the backend
// catalogue (i18n/locales, keys in i18n/keys_fork.go) in the language the
// request stated. The sentinel behind the text (model.ErrOrganizationDisabled,
// model.ErrOrganizationMemberSuspended, ErrInsufficientOrganizationWalletQuota,
// ErrInsufficientWalletQuota) stays reachable through errors.Is.

// localizedError is a translated client message whose Unwrap returns the
// error it was rendered from.
type localizedError struct {
	message string
	cause   error
}

func (e *localizedError) Error() string { return e.message }
func (e *localizedError) Unwrap() error { return e.cause }

// organizationError renders key in lang ("" = no stated language, so
// common.DefaultLanguage). cause may be nil when no caller matches it.
func organizationError(lang string, cause error, key string, args ...map[string]any) error {
	message := i18n.Translate(lang, key, args...)
	if cause == nil {
		return errors.New(message)
	}
	return &localizedError{message: message, cause: cause}
}

// organizationWalletSettleError is the client text of a failed non-reserving
// organization wallet settle (reserveOrganizationFunding).
func organizationWalletSettleError(lang string, err error) error {
	return organizationError(lang, err, i18n.MsgOrganizationWalletInsufficient, map[string]any{"Error": err.Error()})
}
