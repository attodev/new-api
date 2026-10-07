package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
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
