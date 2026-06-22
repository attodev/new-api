package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// TwoFA stores user 2FA settings
type TwoFA struct {
	Id             int            `json:"id" gorm:"primaryKey"`
	UserId         int            `json:"user_id" gorm:"unique;not null;index"`
	Secret         string         `json:"-" gorm:"type:varchar(255);not null"` // TOTP secret, not returned to frontend
	IsEnabled      bool           `json:"is_enabled"`
	FailedAttempts int            `json:"failed_attempts" gorm:"default:0"`
	LockedUntil    *time.Time     `json:"locked_until,omitempty"`
	LastUsedAt     *time.Time     `json:"last_used_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

// TwoFABackupCode stores backup code usage records
type TwoFABackupCode struct {
	Id        int            `json:"id" gorm:"primaryKey"`
	UserId    int            `json:"user_id" gorm:"not null;index"`
	CodeHash  string         `json:"-" gorm:"type:varchar(255);not null"` // backup code hash
	IsUsed    bool           `json:"is_used"`
	UsedAt    *time.Time     `json:"used_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// GetTwoFAByUserId retrieves 2FA settings by user ID
func GetTwoFAByUserId(userId int) (*TwoFA, error) {
	if userId == 0 {
		return nil, errors.New("user ID cannot be empty")
	}

	var twoFA TwoFA
	err := DB.Where("user_id = ?", userId).First(&twoFA).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // return nil to indicate 2FA is not configured
		}
		return nil, err
	}

	return &twoFA, nil
}

// IsTwoFAEnabled checks whether 2FA is enabled for the user
func IsTwoFAEnabled(userId int) bool {
	twoFA, err := GetTwoFAByUserId(userId)
	if err != nil || twoFA == nil {
		return false
	}
	return twoFA.IsEnabled
}

// CreateTwoFA creates 2FA settings
func (t *TwoFA) Create() error {
	// check if user already has 2FA configured
	existing, err := GetTwoFAByUserId(t.UserId)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New("user already has 2FA configured")
	}

	// verify user exists
	var user User
	if err := DB.First(&user, t.UserId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("user not found")
		}
		return err
	}

	return DB.Create(t).Error
}

// Update updates 2FA settings
func (t *TwoFA) Update() error {
	if t.Id == 0 {
		return errors.New("2FA record ID cannot be empty")
	}
	return DB.Save(t).Error
}

// Delete deletes 2FA settings
func (t *TwoFA) Delete() error {
	if t.Id == 0 {
		return errors.New("2FA record ID cannot be empty")
	}

	// use transaction to ensure atomicity
	return DB.Transaction(func(tx *gorm.DB) error {
		// also hard-delete related backup code records
		if err := tx.Unscoped().Where("user_id = ?", t.UserId).Delete(&TwoFABackupCode{}).Error; err != nil {
			return err
		}

		// hard-delete the 2FA record
		return tx.Unscoped().Delete(t).Error
	})
}

// ResetFailedAttempts resets the failed attempt count
func (t *TwoFA) ResetFailedAttempts() error {
	t.FailedAttempts = 0
	t.LockedUntil = nil
	return t.Update()
}

// IncrementFailedAttempts increments the failed attempt count
func (t *TwoFA) IncrementFailedAttempts() error {
	t.FailedAttempts++

	// check if lockout is needed
	if t.FailedAttempts >= common.MaxFailAttempts {
		lockUntil := time.Now().Add(time.Duration(common.LockoutDuration) * time.Second)
		t.LockedUntil = &lockUntil
	}

	return t.Update()
}

// IsLocked checks whether the account is locked
func (t *TwoFA) IsLocked() bool {
	if t.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*t.LockedUntil)
}

// CreateBackupCodes creates backup codes
func CreateBackupCodes(userId int, codes []string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// delete existing backup codes first
		if err := tx.Where("user_id = ?", userId).Delete(&TwoFABackupCode{}).Error; err != nil {
			return err
		}

		// create new backup code records
		for _, code := range codes {
			hashedCode, err := common.HashBackupCode(code)
			if err != nil {
				return err
			}

			backupCode := TwoFABackupCode{
				UserId:   userId,
				CodeHash: hashedCode,
				IsUsed:   false,
			}

			if err := tx.Create(&backupCode).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// ValidateBackupCode validates and consumes a backup code
func ValidateBackupCode(userId int, code string) (bool, error) {
	if !common.ValidateBackupCode(code) {
		return false, errors.New("verification code or backup code is incorrect")
	}

	normalizedCode := common.NormalizeBackupCode(code)

	// find unused backup codes
	var backupCodes []TwoFABackupCode
	if err := DB.Where("user_id = ? AND is_used = false", userId).Find(&backupCodes).Error; err != nil {
		return false, err
	}

	// verify backup codes
	for _, bc := range backupCodes {
		if common.ValidatePasswordAndHash(normalizedCode, bc.CodeHash) {
			// mark as used
			now := time.Now()
			bc.IsUsed = true
			bc.UsedAt = &now

			if err := DB.Save(&bc).Error; err != nil {
				return false, err
			}

			return true, nil
		}
	}

	return false, nil
}

// GetUnusedBackupCodeCount returns the number of unused backup codes
func GetUnusedBackupCodeCount(userId int) (int, error) {
	var count int64
	err := DB.Model(&TwoFABackupCode{}).Where("user_id = ? AND is_used = false", userId).Count(&count).Error
	return int(count), err
}

// DisableTwoFA disables 2FA for the user
func DisableTwoFA(userId int) error {
	twoFA, err := GetTwoFAByUserId(userId)
	if err != nil {
		return err
	}
	if twoFA == nil {
		return ErrTwoFANotEnabled
	}

	// delete 2FA settings and backup codes
	return twoFA.Delete()
}

// EnableTwoFA enables 2FA
func (t *TwoFA) Enable() error {
	t.IsEnabled = true
	t.FailedAttempts = 0
	t.LockedUntil = nil
	return t.Update()
}

// ValidateTOTPAndUpdateUsage validates a TOTP code and updates the usage record
func (t *TwoFA) ValidateTOTPAndUpdateUsage(code string) (bool, error) {
	// check if locked
	if t.IsLocked() {
		return false, fmt.Errorf("account is locked, please retry after %v", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// validate TOTP code
	if !common.ValidateTOTPCode(t.Secret, code) {
		// increment failure count
		if err := t.IncrementFailedAttempts(); err != nil {
			common.SysLog("failed to update 2FA failure count: " + err.Error())
		}
		return false, nil
	}

	// validation succeeded, reset failure count and update last used time
	now := time.Now()
	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now

	if err := t.Update(); err != nil {
		common.SysLog("failed to update 2FA usage record: " + err.Error())
	}

	return true, nil
}

// ValidateBackupCodeAndUpdateUsage validates a backup code and updates the usage record
func (t *TwoFA) ValidateBackupCodeAndUpdateUsage(code string) (bool, error) {
	// check if locked
	if t.IsLocked() {
		return false, fmt.Errorf("account is locked, please retry after %v", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// validate backup code
	valid, err := ValidateBackupCode(t.UserId, code)
	if err != nil {
		return false, err
	}

	if !valid {
		// increment failure count
		if err := t.IncrementFailedAttempts(); err != nil {
			common.SysLog("failed to update 2FA failure count: " + err.Error())
		}
		return false, nil
	}

	// validation succeeded, reset failure count and update last used time
	now := time.Now()
	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now

	if err := t.Update(); err != nil {
		common.SysLog("failed to update 2FA usage record: " + err.Error())
	}

	return true, nil
}

// GetTwoFAStats returns 2FA statistics (admin use)
func GetTwoFAStats() (map[string]interface{}, error) {
	var totalUsers, enabledUsers int64

	// total users
	if err := DB.Model(&User{}).Count(&totalUsers).Error; err != nil {
		return nil, err
	}

	// users with 2FA enabled
	if err := DB.Model(&TwoFA{}).Where("is_enabled = true").Count(&enabledUsers).Error; err != nil {
		return nil, err
	}

	enabledRate := float64(0)
	if totalUsers > 0 {
		enabledRate = float64(enabledUsers) / float64(totalUsers) * 100
	}

	return map[string]interface{}{
		"total_users":   totalUsers,
		"enabled_users": enabledUsers,
		"enabled_rate":  fmt.Sprintf("%.1f%%", enabledRate),
	}, nil
}
