package openwebui

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	ReasonNoUser            = "no_user"
	ReasonAmbiguousEmail    = "ambiguous_email"
	ReasonUserAlreadyLinked = "user_already_linked"
	ReasonUserMissing       = "user_missing"
)

// LinkError means the JWT was valid but could not be attributed to a new-api
// user. Reason is for server logs only; the client sees a generic 403.
type LinkError struct {
	Reason string
}

func (e *LinkError) Error() string {
	return "openwebui: link failed: " + e.Reason
}

// ResolveUserId maps a verified JWT to a new-api user id.
//
//  1. cached sub → user id
//  2. openwebui_user_links row by sub
//  3. first sight: exactly one non-deleted user with the same email
//     (case-insensitive) who is not already linked → create the link
//
// Any other outcome is a *LinkError. Non-link failures (DB errors) are
// returned as-is so the caller can answer 500 instead of 403.
func ResolveUserId(claims *UserClaims) (int, error) {
	if claims == nil || claims.Subject == "" {
		return 0, &LinkError{Reason: ReasonNoUser}
	}
	sub := claims.Subject

	if userId, found, err := subCache().Get(sub); err == nil && found && userId > 0 {
		return requireUserExists(userId)
	}

	link, err := model.GetOpenWebUILinkBySub(sub)
	if err == nil {
		_ = subCache().SetWithTTL(sub, link.UserId, cacheTTL)
		return requireUserExists(link.UserId)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	return autoLinkByEmail(claims)
}

func autoLinkByEmail(claims *UserClaims) (int, error) {
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		return 0, &LinkError{Reason: ReasonNoUser}
	}
	users, err := model.FindUsersByEmailFold(email)
	if err != nil {
		return 0, err
	}
	switch len(users) {
	case 0:
		return 0, &LinkError{Reason: ReasonNoUser}
	case 1:
	default:
		return 0, &LinkError{Reason: ReasonAmbiguousEmail}
	}
	user := users[0]

	link := &model.OpenWebUILink{
		Sub:      claims.Subject,
		UserId:   user.Id,
		Email:    email,
		LinkedBy: model.OpenWebUILinkedByAuto,
	}
	if err := model.CreateOpenWebUILink(link); err != nil {
		if !errors.Is(err, model.ErrOpenWebUILinkConflict) {
			return 0, err
		}
		// Either a concurrent first request just linked this sub (fine —
		// use it), or this user is already bound to a different sub
		// (Open WebUI account was recreated → admin must unlink first).
		if existing, lookupErr := model.GetOpenWebUILinkBySub(claims.Subject); lookupErr == nil {
			_ = subCache().SetWithTTL(claims.Subject, existing.UserId, cacheTTL)
			return requireUserExists(existing.UserId)
		}
		return 0, &LinkError{Reason: ReasonUserAlreadyLinked}
	}
	_ = subCache().SetWithTTL(claims.Subject, user.Id, cacheTTL)
	return user.Id, nil
}

// requireUserExists turns a dangling link (user deleted after linking) into
// a LinkError instead of letting TokenAuth surface a 500 later.
func requireUserExists(userId int) (int, error) {
	if _, err := model.GetUserCache(userId); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, &LinkError{Reason: ReasonUserMissing}
		}
		return 0, err
	}
	return userId, nil
}
