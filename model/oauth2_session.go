package model

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	oauth2CodePrefix         = "oauth2:code:"
	oauth2SessionTokenPrefix = "oauth2:session_token:"
	oauth2TokenTTL           = 24 * time.Hour
	oauth2CodeTTL            = 120 * time.Second
)

var ErrOAuth2CodeInvalid = errors.New("oauth2 code invalid or expired")

// StoreAuthCode mints a single-use, short-lived authorization code for
// userId and stores it in Redis. The code is opaque and URL-safe.
func StoreAuthCode(userId int) (string, error) {
	code, err := common.GenerateKey()
	if err != nil {
		return "", err
	}
	if err := common.RedisSet(oauth2CodePrefix+code, strconv.Itoa(userId), oauth2CodeTTL); err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeAuthCode atomically reads and deletes the code, so a second call
// with the same code always fails — never a partial success.
func ConsumeAuthCode(code string) (int, error) {
	val, err := common.RedisGetDel(oauth2CodePrefix + code)
	if err != nil {
		return 0, ErrOAuth2CodeInvalid
	}
	userId, err := strconv.Atoi(val)
	if err != nil {
		return 0, ErrOAuth2CodeInvalid
	}
	return userId, nil
}

// IssueOAuth2Token mints a Token for userId scoped to group, caches it
// directly in Redis (the same format the existing token cache uses, so
// the unmodified TokenAuth()/GetTokenByKey() path resolves it), and never
// creates a DB row. Any token previously issued for this user via this
// path is invalidated first, so at most one stays live at a time.
func IssueOAuth2Token(userId int, group string, clientId string) (*Token, error) {
	_ = RevokeOAuth2Token(userId)

	key, err := common.GenerateKey()
	if err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	token := Token{
		Id:             -1, // never a real DB row; sentinel so it can't collide with a real Token.Id
		UserId:         userId,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		Name:           fmt.Sprintf("oauth2-%s", clientId),
		CreatedTime:    now,
		AccessedTime:   now,
		ExpiredTime:    now + int64(oauth2TokenTTL.Seconds()),
		UnlimitedQuota: true,
		Group:          group,
	}

	hmacKey := common.GenerateHMAC(token.Key)
	rawKey := token.Key
	cacheToken := token
	cacheToken.Clean() // zero out Key before storing, matching cacheSetToken's convention
	if err := common.RedisHSetObj(fmt.Sprintf("token:%s", hmacKey), &cacheToken, oauth2TokenTTL); err != nil {
		return nil, err
	}
	if err := common.RedisSet(oauth2SessionTokenPrefix+strconv.Itoa(userId), rawKey, oauth2TokenTTL); err != nil {
		return nil, err
	}

	return &token, nil
}

// RevokeOAuth2Token invalidates the user's currently-live OAuth2 token, if
// any. Not finding one is not an error.
func RevokeOAuth2Token(userId int) error {
	rawKey, err := common.RedisGet(oauth2SessionTokenPrefix + strconv.Itoa(userId))
	if err != nil {
		return nil
	}
	if err := cacheDeleteToken(rawKey); err != nil {
		return err
	}
	return common.RedisDel(oauth2SessionTokenPrefix + strconv.Itoa(userId))
}
