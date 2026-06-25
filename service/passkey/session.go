package passkey

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
)

func SaveSessionData(c *gin.Context, key string, data *webauthn.SessionData) error {
	session := sessions.Default(c)
	if data == nil {
		session.Delete(key)
		return session.Save()
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	session.Set(key, string(payload))
	return session.Save()
}

func PopSessionData(c *gin.Context, key string) (*webauthn.SessionData, error) {
	session := sessions.Default(c)
	raw := session.Get(key)
	if raw == nil {
		return nil, errors.New(common.TranslateMessage(c, i18n.MsgPasskeySessionExpired))
	}
	session.Delete(key)
	_ = session.Save()
	var data webauthn.SessionData
	switch value := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(value), &data); err != nil {
			return nil, err
		}
	case []byte:
		if err := json.Unmarshal(value, &data); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New(common.TranslateMessage(c, i18n.MsgPasskeySessionFormatInvalid))
	}
	return &data, nil
}
