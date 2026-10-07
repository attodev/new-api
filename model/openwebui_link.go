package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	OpenWebUILinkedByAuto  = "auto"
	OpenWebUILinkedByAdmin = "admin"
)

// ErrOpenWebUILinkConflict is returned when the sub or the user is already
// linked. One Open WebUI account bills exactly one new-api user and vice versa.
var ErrOpenWebUILinkConflict = errors.New("openwebui link conflict: sub or user already linked")

// OpenWebUILink maps an Open WebUI user id (the JWT "sub" claim) to a new-api
// user. Email is the JWT email at link time and is kept for auditing only —
// lookups after the first request go through Sub exclusively.
type OpenWebUILink struct {
	Id        int    `json:"id"`
	Sub       string `json:"sub" gorm:"type:varchar(191);uniqueIndex"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex"`
	Email     string `json:"email" gorm:"type:varchar(191)"`
	LinkedBy  string `json:"linked_by" gorm:"type:varchar(16)"`
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime"`
}

func (OpenWebUILink) TableName() string {
	return "openwebui_user_links"
}

func GetOpenWebUILinkBySub(sub string) (*OpenWebUILink, error) {
	if sub == "" {
		return nil, errors.New("sub is empty")
	}
	var link OpenWebUILink
	if err := DB.Where("sub = ?", sub).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func GetOpenWebUILinkByUserId(userId int) (*OpenWebUILink, error) {
	if userId == 0 {
		return nil, errors.New("user id is empty")
	}
	var link OpenWebUILink
	if err := DB.Where("user_id = ?", userId).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func GetOpenWebUILinkById(id int) (*OpenWebUILink, error) {
	if id == 0 {
		return nil, errors.New("id is empty")
	}
	var link OpenWebUILink
	if err := DB.Where("id = ?", id).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func countOpenWebUILinkConflicts(sub string, userId int) (int64, error) {
	var count int64
	err := DB.Model(&OpenWebUILink{}).Where("sub = ? OR user_id = ?", sub, userId).Count(&count).Error
	return count, err
}

// CreateOpenWebUILink inserts a link, reporting ErrOpenWebUILinkConflict when
// either side is already taken. The pre-check keeps the common case free of
// driver-specific duplicate-key errors; the post-insert re-check covers the
// race where two first requests for the same person arrive together.
func CreateOpenWebUILink(link *OpenWebUILink) error {
	if link == nil || link.Sub == "" || link.UserId == 0 {
		return errors.New("sub and user_id are required")
	}
	if link.LinkedBy == "" {
		link.LinkedBy = OpenWebUILinkedByAuto
	}
	count, err := countOpenWebUILinkConflicts(link.Sub, link.UserId)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrOpenWebUILinkConflict
	}
	link.CreatedAt = common.GetTimestamp()
	if err := DB.Create(link).Error; err != nil {
		if again, checkErr := countOpenWebUILinkConflicts(link.Sub, link.UserId); checkErr == nil && again > 0 {
			return ErrOpenWebUILinkConflict
		}
		return err
	}
	return nil
}

func DeleteOpenWebUILink(id int) error {
	if id == 0 {
		return errors.New("id is empty")
	}
	result := DB.Where("id = ?", id).Delete(&OpenWebUILink{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// FindUsersByEmailFold returns the non-deleted users whose email matches
// case-insensitively. Open WebUI stores emails lower-cased, so this is the
// comparison the auto-link uses. LOWER() exists on SQLite, MySQL and PostgreSQL.
func FindUsersByEmailFold(email string) ([]*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, nil
	}
	var users []*User
	err := DB.Omit("password").Where("LOWER(email) = ?", email).Find(&users).Error
	return users, err
}
