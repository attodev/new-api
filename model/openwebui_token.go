package model

// fork: O13 — per-user token the Open WebUI relay bills to, and identity-only
// API key validation for POST /oauth2/token-from-key.

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// FindOldestUserTokenByName returns the user's lowest-id, non-deleted token
// with that name. When a race created two rows, every caller converges on
// the same one.
func FindOldestUserTokenByName(userId int, name string) (*Token, error) {
	if userId == 0 || name == "" {
		return nil, errors.New("user id and name are required")
	}
	var token Token
	err := DB.Where("user_id = ? AND name = ?", userId, name).Order("id ASC").First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// CreateOpenWebUIToken creates the per-user token the Open WebUI relay bills
// to: unlimited token quota (so the user's wallet is charged), no expiry, no
// group pin (follows the user's group), no model limits. It deliberately
// bypasses the per-user token count limit because it is system-provisioned.
func CreateOpenWebUIToken(userId int, name string) (*Token, error) {
	if userId == 0 || name == "" {
		return nil, errors.New("user id and name are required")
	}
	key, err := common.GenerateKey()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	token := &Token{
		UserId:         userId,
		Name:           name,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    now,
		AccessedTime:   now,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}
	if err := token.Insert(); err != nil {
		return nil, err
	}
	return token, nil
}

// ValidateTokenForIdentity checks key for identity-only use (OIDC
// token-from-key): the key must exist, be enabled and unexpired. Remaining
// quota is deliberately ignored, so a key marked exhausted still identifies
// its owner. Unknown, deleted, disabled and expired keys all yield
// ErrTokenInvalid; lookup failures wrap ErrDatabase. Unlike ValidateUserToken
// it never writes the token's status back.
func ValidateTokenForIdentity(key string) (*Token, error) {
	if key == "" {
		return nil, ErrTokenInvalid
	}
	token, err := GetTokenByKey(key, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTokenInvalid
		}
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if token.Status != common.TokenStatusEnabled && token.Status != common.TokenStatusExhausted {
		return nil, ErrTokenInvalid
	}
	if token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp() {
		return nil, ErrTokenInvalid
	}
	return token, nil
}
