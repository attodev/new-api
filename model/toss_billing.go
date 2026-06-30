package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// UserBillingKey stores a Toss billing key (암호화) for recurring charges.
type UserBillingKey struct {
	Id               int    `json:"id"`
	UserId           int    `json:"user_id" gorm:"index"`
	CustomerKey      string `json:"customer_key" gorm:"type:varchar(64);index"`
	EncryptedKey     string `json:"-" gorm:"type:text"` // AES-GCM(billingKey)
	CardCompany      string `json:"card_company" gorm:"type:varchar(32)"`
	CardNumberMasked string `json:"card_number_masked" gorm:"type:varchar(32)"`
	Status           string `json:"status" gorm:"type:varchar(16);default:'active'"` // active | revoked
	CreateTime       int64  `json:"create_time"`
}

const (
	BillingKeyStatusActive  = "active"
	BillingKeyStatusRevoked = "revoked"
)

// StoreTossBillingKey encrypts and persists a billing key, returning its row id.
func StoreTossBillingKey(userId int, customerKey, billingKey, cardCompany, cardMasked string) (int, error) {
	if billingKey == "" {
		return 0, errors.New("empty billing key")
	}
	enc, err := common.EncryptString(billingKey)
	if err != nil {
		return 0, err
	}
	row := &UserBillingKey{
		UserId:           userId,
		CustomerKey:      customerKey,
		EncryptedKey:     enc,
		CardCompany:      cardCompany,
		CardNumberMasked: cardMasked,
		Status:           BillingKeyStatusActive,
		CreateTime:       common.GetTimestamp(),
	}
	if err := DB.Create(row).Error; err != nil {
		return 0, err
	}
	return row.Id, nil
}

// GetTossBillingKeyPlain returns the decrypted billing key for an active row.
func GetTossBillingKeyPlain(id int) (key string, customerKey string, err error) {
	var row UserBillingKey
	if err = DB.Where("id = ?", id).First(&row).Error; err != nil {
		return "", "", err
	}
	if row.Status != BillingKeyStatusActive {
		return "", "", errors.New("billing key revoked")
	}
	plain, err := common.DecryptString(row.EncryptedKey)
	if err != nil {
		return "", "", err
	}
	return plain, row.CustomerKey, nil
}

// RevokeTossBillingKey marks a billing key revoked (best-effort).
func RevokeTossBillingKey(tx *gorm.DB, id int) error {
	db := DB
	if tx != nil {
		db = tx
	}
	return db.Model(&UserBillingKey{}).Where("id = ?", id).Update("status", BillingKeyStatusRevoked).Error
}
