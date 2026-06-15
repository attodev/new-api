package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type OrganizationSubscriptionPlan struct {
	Id int `json:"id"`

	OrganizationId int    `json:"organization_id" gorm:"index;not null"`
	Title          string `json:"title" gorm:"type:varchar(128);not null"`
	Subtitle       string `json:"subtitle" gorm:"type:varchar(255);default:''"`

	DurationUnit  string `json:"duration_unit" gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue int    `json:"duration_value" gorm:"type:int;not null;default:1"`
	CustomSeconds int64  `json:"custom_seconds" gorm:"type:bigint;not null;default:0"`

	Enabled   bool `json:"enabled" gorm:"default:true"`
	SortOrder int  `json:"sort_order" gorm:"type:int;default:0"`

	UpgradeGroup string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	TotalAmount  int64  `json:"total_amount" gorm:"type:bigint;not null;default:0"`

	QuotaResetPeriod        string `json:"quota_reset_period" gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64  `json:"quota_reset_custom_seconds" gorm:"type:bigint;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (p *OrganizationSubscriptionPlan) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (p *OrganizationSubscriptionPlan) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

type OrganizationUserSubscription struct {
	Id             int `json:"id"`
	OrganizationId int `json:"organization_id" gorm:"index;index:idx_org_user_sub_active,priority:1"`
	UserId         int `json:"user_id" gorm:"index;index:idx_org_user_sub_active,priority:2"`
	PlanId         int `json:"plan_id" gorm:"index"`

	AmountTotal int64 `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed  int64 `json:"amount_used" gorm:"type:bigint;not null;default:0"`

	StartTime int64  `json:"start_time" gorm:"bigint"`
	EndTime   int64  `json:"end_time" gorm:"bigint;index;index:idx_org_user_sub_active,priority:4"`
	Status    string `json:"status" gorm:"type:varchar(32);index;index:idx_org_user_sub_active,priority:3"`

	LastResetTime int64 `json:"last_reset_time" gorm:"type:bigint;default:0"`
	NextResetTime int64 `json:"next_reset_time" gorm:"type:bigint;default:0;index"`

	UpgradeGroup     string `json:"upgrade_group" gorm:"type:varchar(64);default:''"`
	PrevUserGroup    string `json:"prev_user_group" gorm:"type:varchar(64);default:''"`
	AssignedByUserId int    `json:"assigned_by_user_id" gorm:"index;default:0"`

	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (s *OrganizationUserSubscription) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *OrganizationUserSubscription) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

type OrganizationSubscriptionPreConsumeRecord struct {
	Id                             int    `json:"id"`
	RequestId                      string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	OrganizationId                 int    `json:"organization_id" gorm:"index"`
	UserId                         int    `json:"user_id" gorm:"index"`
	OrganizationUserSubscriptionId int    `json:"organization_user_subscription_id" gorm:"index"`
	PreConsumed                    int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	Status                         string `json:"status" gorm:"type:varchar(32);index"`
	CreatedAt                      int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt                      int64  `json:"updated_at" gorm:"bigint;index"`
}

func (r *OrganizationSubscriptionPreConsumeRecord) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *OrganizationSubscriptionPreConsumeRecord) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

type OrganizationSubscriptionPreConsumeResult struct {
	OrganizationUserSubscriptionId int
	PreConsumed                    int64
	AmountTotal                    int64
	AmountUsedBefore               int64
	AmountUsedAfter                int64
	PlanId                         int
	PlanTitle                      string
}

type OrganizationSubscriptionPlanInfo struct {
	PlanId    int
	PlanTitle string
}

type OrganizationUserSubscriptionSummary struct {
	Subscription *OrganizationUserSubscription `json:"subscription"`
	User         *User                         `json:"user"`
	Plan         *OrganizationSubscriptionPlan `json:"plan"`
}

func GetOrganizationSubscriptionPlanById(organizationId int, planId int) (*OrganizationSubscriptionPlan, error) {
	return getOrganizationSubscriptionPlanByIdTx(nil, organizationId, planId)
}

func getOrganizationSubscriptionPlanByIdTx(tx *gorm.DB, organizationId int, planId int) (*OrganizationSubscriptionPlan, error) {
	if organizationId <= 0 || planId <= 0 {
		return nil, errors.New("invalid organization subscription plan")
	}
	query := DB
	if tx != nil {
		query = tx
	}
	var plan OrganizationSubscriptionPlan
	if err := query.Where("id = ? AND organization_id = ?", planId, organizationId).First(&plan).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

func ListOrganizationSubscriptionPlans(organizationId int) ([]OrganizationSubscriptionPlan, error) {
	if organizationId <= 0 {
		return nil, errors.New("invalid organization id")
	}
	var plans []OrganizationSubscriptionPlan
	if err := DB.Where("organization_id = ?", organizationId).
		Order("sort_order asc, id asc").
		Find(&plans).Error; err != nil {
		return nil, err
	}
	return plans, nil
}

func ListOrganizationUserSubscriptions(organizationId int) ([]OrganizationUserSubscriptionSummary, error) {
	if organizationId <= 0 {
		return nil, errors.New("invalid organization id")
	}
	var subs []OrganizationUserSubscription
	if err := DB.Where("organization_id = ?", organizationId).
		Order("status asc, end_time desc, id desc").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return []OrganizationUserSubscriptionSummary{}, nil
	}
	userIds := make([]int, 0, len(subs))
	planIds := make([]int, 0, len(subs))
	for _, sub := range subs {
		userIds = append(userIds, sub.UserId)
		planIds = append(planIds, sub.PlanId)
	}
	var users []User
	if err := DB.Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return nil, err
	}
	var plans []OrganizationSubscriptionPlan
	if err := DB.Where("organization_id = ? AND id IN ?", organizationId, planIds).Find(&plans).Error; err != nil {
		return nil, err
	}
	userById := make(map[int]User, len(users))
	for _, user := range users {
		userById[user.Id] = user
	}
	planById := make(map[int]OrganizationSubscriptionPlan, len(plans))
	for _, plan := range plans {
		planById[plan.Id] = plan
	}
	result := make([]OrganizationUserSubscriptionSummary, 0, len(subs))
	for _, sub := range subs {
		subCopy := sub
		summary := OrganizationUserSubscriptionSummary{Subscription: &subCopy}
		if user, ok := userById[sub.UserId]; ok {
			userCopy := user
			summary.User = &userCopy
		}
		if plan, ok := planById[sub.PlanId]; ok {
			planCopy := plan
			summary.Plan = &planCopy
		}
		result = append(result, summary)
	}
	return result, nil
}

func HasActiveOrganizationUserSubscription(organizationId int, userId int) (bool, error) {
	if organizationId <= 0 || userId <= 0 {
		return false, errors.New("invalid organization subscription user")
	}
	now := GetDBTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, _, err := getActiveOrRenewOrganizationUserSubscriptionTx(tx, organizationId, userId, now)
		return err
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func GetActiveOrganizationUserSubscription(organizationId int, userId int) (*OrganizationUserSubscription, error) {
	if organizationId <= 0 || userId <= 0 {
		return nil, errors.New("invalid organization subscription user")
	}
	now := GetDBTimestamp()
	var sub *OrganizationUserSubscription
	err := DB.Transaction(func(tx *gorm.DB) error {
		renewed, _, err := getActiveOrRenewOrganizationUserSubscriptionTx(tx, organizationId, userId, now)
		if err != nil {
			return err
		}
		sub = renewed
		return nil
	})
	return sub, err
}

func getActiveOrRenewOrganizationUserSubscriptionTx(tx *gorm.DB, organizationId int, userId int, now int64) (*OrganizationUserSubscription, *OrganizationSubscriptionPlan, error) {
	if tx == nil {
		return nil, nil, errors.New("transaction is nil")
	}
	var sub OrganizationUserSubscription
	if err := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("organization_id = ? AND user_id = ? AND status = ? AND end_time > ?", organizationId, userId, "active", now).
		Order("end_time asc, id asc").
		First(&sub).Error; err == nil {
		plan, err := getOrganizationSubscriptionPlanByIdTx(tx, organizationId, sub.PlanId)
		if err != nil {
			return nil, nil, err
		}
		return &sub, plan, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}

	if err := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("organization_id = ? AND user_id = ? AND status = ? AND end_time <= ?", organizationId, userId, "active", now).
		Order("end_time desc, id desc").
		First(&sub).Error; err != nil {
		return nil, nil, err
	}
	plan, err := getOrganizationSubscriptionPlanByIdTx(tx, organizationId, sub.PlanId)
	if err != nil {
		return nil, nil, err
	}
	if err := renewOrganizationUserSubscriptionWithPlanTx(tx, &sub, plan, now); err != nil {
		return nil, nil, err
	}
	return &sub, plan, nil
}

func renewOrganizationUserSubscriptionWithPlanTx(tx *gorm.DB, sub *OrganizationUserSubscription, plan *OrganizationSubscriptionPlan, now int64) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid organization subscription renewal args")
	}
	periodStart := sub.EndTime
	if periodStart <= 0 {
		periodStart = now
	}
	nextEnd := sub.EndTime
	for i := 0; i < 10000 && nextEnd <= now; i++ {
		periodStart = nextEnd
		if periodStart <= 0 {
			periodStart = now
		}
		calculatedEnd, err := CalcSubscriptionPlanEndTime(time.Unix(periodStart, 0), plan.DurationUnit, plan.DurationValue, plan.CustomSeconds)
		if err != nil {
			return err
		}
		if calculatedEnd <= periodStart {
			return errors.New("organization subscription renewal did not advance end time")
		}
		nextEnd = calculatedEnd
	}
	if nextEnd <= now {
		return errors.New("organization subscription renewal exceeded maximum periods")
	}

	nextReset := CalcSubscriptionNextResetTime(time.Unix(periodStart, 0), plan.QuotaResetPeriod, plan.QuotaResetCustomSeconds, nextEnd)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = periodStart
	}
	sub.AmountTotal = plan.TotalAmount
	sub.AmountUsed = 0
	sub.EndTime = nextEnd
	sub.LastResetTime = lastReset
	sub.NextResetTime = nextReset
	sub.UpdatedAt = common.GetTimestamp()
	return tx.Save(sub).Error
}

func GetOrganizationUserSubscriptionById(id int) (*OrganizationUserSubscription, error) {
	if id <= 0 {
		return nil, errors.New("invalid organizationUserSubscriptionId")
	}
	var sub OrganizationUserSubscription
	if err := DB.Where("id = ?", id).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func CreateOrganizationUserSubscriptionFromPlan(organizationId int, userId int, planId int, assignedByUserId int) (*OrganizationUserSubscription, error) {
	if organizationId <= 0 || userId <= 0 || planId <= 0 {
		return nil, errors.New("invalid organization subscription assignment")
	}
	var created OrganizationUserSubscription
	cacheGroup := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", userId).First(&user).Error; err != nil {
			return err
		}
		if user.OrganizationId != organizationId {
			return errors.New("target user is outside organization")
		}
		if user.Role >= common.RoleAdminUser {
			return errors.New("target user cannot be system administrator")
		}
		plan, err := getOrganizationSubscriptionPlanByIdTx(tx, organizationId, planId)
		if err != nil {
			return err
		}
		if !plan.Enabled {
			return errors.New("organization subscription plan is disabled")
		}
		nowUnix := common.GetTimestamp()
		now := time.Unix(nowUnix, 0)
		endUnix, err := CalcSubscriptionPlanEndTime(now, plan.DurationUnit, plan.DurationValue, plan.CustomSeconds)
		if err != nil {
			return err
		}
		nextReset := CalcSubscriptionNextResetTime(now, plan.QuotaResetPeriod, plan.QuotaResetCustomSeconds, endUnix)
		lastReset := int64(0)
		if nextReset > 0 {
			lastReset = now.Unix()
		}
		if err := tx.Model(&OrganizationUserSubscription{}).
			Where("organization_id = ? AND user_id = ? AND status = ?", organizationId, userId, "active").
			Updates(map[string]interface{}{
				"status":     "cancelled",
				"end_time":   nowUnix,
				"updated_at": common.GetTimestamp(),
			}).Error; err != nil {
			return err
		}
		upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
		prevGroup := ""
		if upgradeGroup != "" && user.Group != upgradeGroup {
			prevGroup = user.Group
			if err := tx.Model(&User{}).Where("id = ?", userId).Update("group", upgradeGroup).Error; err != nil {
				return err
			}
			cacheGroup = upgradeGroup
		}
		created = OrganizationUserSubscription{
			OrganizationId:   organizationId,
			UserId:           userId,
			PlanId:           plan.Id,
			AmountTotal:      plan.TotalAmount,
			AmountUsed:       0,
			StartTime:        now.Unix(),
			EndTime:          endUnix,
			Status:           "active",
			LastResetTime:    lastReset,
			NextResetTime:    nextReset,
			UpgradeGroup:     upgradeGroup,
			PrevUserGroup:    prevGroup,
			AssignedByUserId: assignedByUserId,
		}
		return tx.Create(&created).Error
	})
	if err != nil {
		return nil, err
	}
	if cacheGroup != "" {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	return &created, nil
}

func downgradeUserGroupForOrganizationSubscriptionTx(tx *gorm.DB, sub *OrganizationUserSubscription, now int64) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid organization subscription downgrade args")
	}
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	if upgradeGroup == "" {
		return "", nil
	}
	currentGroup, err := getUserGroupByIdTx(tx, sub.UserId)
	if err != nil {
		return "", err
	}
	if currentGroup != upgradeGroup {
		return "", nil
	}
	var activeSub OrganizationUserSubscription
	activeQuery := tx.Where("organization_id = ? AND user_id = ? AND status = ? AND end_time > ? AND id <> ? AND upgrade_group <> ''",
		sub.OrganizationId, sub.UserId, "active", now, sub.Id).
		Order("end_time desc, id desc").
		Limit(1).
		Find(&activeSub)
	if activeQuery.Error == nil && activeQuery.RowsAffected > 0 {
		return "", nil
	}
	prevGroup := strings.TrimSpace(sub.PrevUserGroup)
	if prevGroup == "" || prevGroup == currentGroup {
		return "", nil
	}
	if err := tx.Model(&User{}).Where("id = ?", sub.UserId).Update("group", prevGroup).Error; err != nil {
		return "", err
	}
	return prevGroup, nil
}

func CancelActiveOrganizationUserSubscriptionForUser(organizationId int, userId int) (string, error) {
	if organizationId <= 0 || userId <= 0 {
		return "", errors.New("invalid organization subscription user")
	}
	now := common.GetTimestamp()
	cacheGroup := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		var subs []OrganizationUserSubscription
		if err := tx.Set("gorm:query_option", "FOR UPDATE").
			Where("organization_id = ? AND user_id = ? AND status = ?", organizationId, userId, "active").
			Order("end_time desc, id desc").
			Find(&subs).Error; err != nil {
			return err
		}
		if len(subs) == 0 {
			return gorm.ErrRecordNotFound
		}
		for _, sub := range subs {
			targetGroup, err := downgradeUserGroupForOrganizationSubscriptionTx(tx, &sub, now)
			if err != nil {
				return err
			}
			if targetGroup != "" {
				cacheGroup = targetGroup
			}
		}
		if err := tx.Model(&OrganizationUserSubscription{}).
			Where("organization_id = ? AND user_id = ? AND status = ?", organizationId, userId, "active").
			Updates(map[string]interface{}{
				"status":     "cancelled",
				"end_time":   now,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" {
		_ = UpdateUserGroupCache(userId, cacheGroup)
	}
	return cacheGroup, nil
}

func maybeResetOrganizationSubscriptionWithPlanTx(tx *gorm.DB, sub *OrganizationUserSubscription, plan *OrganizationSubscriptionPlan, now int64) error {
	if tx == nil || sub == nil || plan == nil {
		return errors.New("invalid organization subscription reset args")
	}
	if sub.NextResetTime > 0 && sub.NextResetTime > now {
		return nil
	}
	if NormalizeResetPeriod(plan.QuotaResetPeriod) == SubscriptionResetNever {
		return nil
	}
	baseUnix := sub.LastResetTime
	if baseUnix <= 0 {
		baseUnix = sub.StartTime
	}
	base := time.Unix(baseUnix, 0)
	next := CalcSubscriptionNextResetTime(base, plan.QuotaResetPeriod, plan.QuotaResetCustomSeconds, sub.EndTime)
	advanced := false
	for next > 0 && next <= now {
		advanced = true
		base = time.Unix(next, 0)
		next = CalcSubscriptionNextResetTime(base, plan.QuotaResetPeriod, plan.QuotaResetCustomSeconds, sub.EndTime)
	}
	if !advanced {
		if sub.NextResetTime == 0 && next > 0 {
			sub.NextResetTime = next
			sub.LastResetTime = base.Unix()
			return tx.Save(sub).Error
		}
		return nil
	}
	sub.AmountUsed = 0
	sub.LastResetTime = base.Unix()
	sub.NextResetTime = next
	return tx.Save(sub).Error
}

func PreConsumeOrganizationUserSubscription(requestId string, organizationId int, userId int, modelName string, amount int64) (*OrganizationSubscriptionPreConsumeResult, error) {
	if organizationId <= 0 || userId <= 0 {
		return nil, errors.New("invalid organization subscription user")
	}
	if strings.TrimSpace(requestId) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}
	now := GetDBTimestamp()
	returnValue := &OrganizationSubscriptionPreConsumeResult{}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var existing OrganizationSubscriptionPreConsumeRecord
		query := tx.Where("request_id = ?", requestId).Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			if existing.Status == "refunded" {
				return errors.New("organization subscription pre-consume already refunded")
			}
			var sub OrganizationUserSubscription
			if err := tx.Where("id = ?", existing.OrganizationUserSubscriptionId).First(&sub).Error; err != nil {
				return err
			}
			plan, err := getOrganizationSubscriptionPlanByIdTx(tx, sub.OrganizationId, sub.PlanId)
			if err != nil {
				return err
			}
			returnValue.OrganizationUserSubscriptionId = sub.Id
			returnValue.PreConsumed = existing.PreConsumed
			returnValue.AmountTotal = sub.AmountTotal
			returnValue.AmountUsedBefore = sub.AmountUsed
			returnValue.AmountUsedAfter = sub.AmountUsed
			returnValue.PlanId = plan.Id
			returnValue.PlanTitle = plan.Title
			return nil
		}

		sub, plan, err := getActiveOrRenewOrganizationUserSubscriptionTx(tx, organizationId, userId, now)
		if err != nil {
			return errors.New("no active organization subscription")
		}
		if err := maybeResetOrganizationSubscriptionWithPlanTx(tx, sub, plan, now); err != nil {
			return err
		}
		usedBefore := sub.AmountUsed
		if sub.AmountTotal > 0 {
			remain := sub.AmountTotal - usedBefore
			if remain < amount {
				return fmt.Errorf("organization subscription quota insufficient, need=%d", amount)
			}
		}
		record := &OrganizationSubscriptionPreConsumeRecord{
			RequestId:                      requestId,
			OrganizationId:                 organizationId,
			UserId:                         userId,
			OrganizationUserSubscriptionId: sub.Id,
			PreConsumed:                    amount,
			Status:                         "consumed",
		}
		if err := tx.Create(record).Error; err != nil {
			var dup OrganizationSubscriptionPreConsumeRecord
			if err2 := tx.Where("request_id = ?", requestId).First(&dup).Error; err2 == nil {
				if dup.Status == "refunded" {
					return errors.New("organization subscription pre-consume already refunded")
				}
				returnValue.OrganizationUserSubscriptionId = dup.OrganizationUserSubscriptionId
				returnValue.PreConsumed = dup.PreConsumed
				returnValue.AmountTotal = sub.AmountTotal
				returnValue.AmountUsedBefore = sub.AmountUsed
				returnValue.AmountUsedAfter = sub.AmountUsed
				returnValue.PlanId = plan.Id
				returnValue.PlanTitle = plan.Title
				return nil
			}
			return err
		}
		sub.AmountUsed += amount
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		returnValue.OrganizationUserSubscriptionId = sub.Id
		returnValue.PreConsumed = amount
		returnValue.AmountTotal = sub.AmountTotal
		returnValue.AmountUsedBefore = usedBefore
		returnValue.AmountUsedAfter = sub.AmountUsed
		returnValue.PlanId = plan.Id
		returnValue.PlanTitle = plan.Title
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = modelName
	return returnValue, nil
}

func PostConsumeOrganizationUserSubscriptionDelta(organizationUserSubscriptionId int, delta int64) error {
	if organizationUserSubscriptionId <= 0 {
		return errors.New("invalid organizationUserSubscriptionId")
	}
	if delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return postConsumeOrganizationUserSubscriptionDeltaTx(tx, organizationUserSubscriptionId, delta)
	})
}

func postConsumeOrganizationUserSubscriptionDeltaTx(tx *gorm.DB, organizationUserSubscriptionId int, delta int64) error {
	var sub OrganizationUserSubscription
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", organizationUserSubscriptionId).First(&sub).Error; err != nil {
		return err
	}
	newUsed := sub.AmountUsed + delta
	if newUsed < 0 {
		newUsed = 0
	}
	if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
		return fmt.Errorf("organization subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
	}
	sub.AmountUsed = newUsed
	return tx.Save(&sub).Error
}

func RefundOrganizationSubscriptionPreConsume(requestId string) error {
	if strings.TrimSpace(requestId) == "" {
		return errors.New("requestId is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var record OrganizationSubscriptionPreConsumeRecord
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("request_id = ?", requestId).First(&record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" {
			return nil
		}
		if record.PreConsumed > 0 {
			if err := postConsumeOrganizationUserSubscriptionDeltaTx(tx, record.OrganizationUserSubscriptionId, -record.PreConsumed); err != nil {
				return err
			}
		}
		record.Status = "refunded"
		return tx.Save(&record).Error
	})
}

func GetOrganizationSubscriptionPlanInfoByUserSubscriptionId(organizationUserSubscriptionId int) (*OrganizationSubscriptionPlanInfo, error) {
	if organizationUserSubscriptionId <= 0 {
		return nil, errors.New("invalid organizationUserSubscriptionId")
	}
	var sub OrganizationUserSubscription
	if err := DB.Where("id = ?", organizationUserSubscriptionId).First(&sub).Error; err != nil {
		return nil, err
	}
	plan, err := getOrganizationSubscriptionPlanByIdTx(nil, sub.OrganizationId, sub.PlanId)
	if err != nil {
		return nil, err
	}
	return &OrganizationSubscriptionPlanInfo{
		PlanId:    plan.Id,
		PlanTitle: plan.Title,
	}, nil
}
