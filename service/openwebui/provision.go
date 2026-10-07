package openwebui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	// TokenNameChat bills ordinary conversations, model listing, embeddings,
	// images and audio.
	TokenNameChat = "openwebui"
	// TokenNameTask bills Open WebUI background tasks (X-Task header set:
	// title_generation, tags_generation, ...), kept separate for auditing.
	TokenNameTask = "openwebui-task"
)

var provisionGroup singleflight.Group

// TokenNameForTask picks the per-user token name from the X-Task header value.
func TokenNameForTask(task string) string {
	if strings.TrimSpace(task) == "" {
		return TokenNameChat
	}
	return TokenNameTask
}

// GetOrCreateUserToken returns the user's token named name, creating it on
// first use. The (user,name)→key cache keeps the hot path off the token
// table; a cached key whose row has since been deleted (the user removed the
// token from their key list) falls through and provisions a fresh one.
// Concurrent first requests are collapsed with singleflight and converge on
// the oldest row.
func GetOrCreateUserToken(userId int, name string) (*model.Token, error) {
	if userId == 0 || name == "" {
		return nil, errors.New("openwebui: user id and token name are required")
	}
	cacheKey := fmt.Sprintf("%d:%s", userId, name)

	if key, found, err := tokenKeyCache().Get(cacheKey); err == nil && found && key != "" {
		token, err := model.GetTokenByKey(key, false)
		if err == nil {
			if token.Key == "" {
				token.Key = key
			}
			return token, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		_, _ = tokenKeyCache().DeleteMany([]string{cacheKey})
	}

	v, err, _ := provisionGroup.Do(cacheKey, func() (any, error) {
		token, err := model.FindOldestUserTokenByName(userId, name)
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if _, err := model.CreateOpenWebUIToken(userId, name); err != nil {
			return nil, err
		}
		// Re-read so a racing insert on another node resolves to one row.
		return model.FindOldestUserTokenByName(userId, name)
	})
	if err != nil {
		return nil, err
	}
	token := v.(*model.Token)
	_ = tokenKeyCache().SetWithTTL(cacheKey, token.Key, cacheTTL)
	return token, nil
}
