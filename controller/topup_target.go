package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	topUpTargetTypeContextKey = "topup_target_type"
	topUpTargetIdContextKey   = "topup_target_id"
)

func setTopUpTarget(c *gin.Context, targetType string, targetId int) {
	c.Set(topUpTargetTypeContextKey, targetType)
	c.Set(topUpTargetIdContextKey, targetId)
}

func getTopUpTargetType(c *gin.Context) string {
	if targetType, ok := c.Get(topUpTargetTypeContextKey); ok {
		if value, ok := targetType.(string); ok && value != "" {
			return value
		}
	}
	return model.TopUpTargetTypeUser
}

func getTopUpTargetId(c *gin.Context) int {
	if targetId, ok := c.Get(topUpTargetIdContextKey); ok {
		if value, ok := targetId.(int); ok && value > 0 {
			return value
		}
	}
	return c.GetInt("id")
}

func getOrganizationWalletActor(c *gin.Context) (*model.User, *model.Organization, error) {
	actor, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		return nil, nil, err
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationOwnerRole(actor.OrganizationRole) {
		return nil, nil, common.NewMessage("organization owner permission required")
	}

	org := &model.Organization{}
	if err := model.DB.First(org, actor.OrganizationId).Error; err != nil {
		return nil, nil, err
	}
	return actor, org, nil
}

func prepareOrganizationTopUpTarget(c *gin.Context) (*model.User, *model.Organization, bool) {
	actor, org, err := getOrganizationWalletActor(c)
	if err != nil {
		common.ApiError(c, err)
		return nil, nil, false
	}
	setTopUpTarget(c, model.TopUpTargetTypeOrganization, org.Id)
	return actor, org, true
}

// prepareOrganizationPaymentTarget is prepareOrganizationTopUpTarget for the
// pay/amount wrappers: a rejection uses the payment flows' error shape
// (paymentError), whose data field the pay-flow frontend reads.
func prepareOrganizationPaymentTarget(c *gin.Context) bool {
	_, org, err := getOrganizationWalletActor(c)
	if err != nil {
		paymentError(c, err)
		return false
	}
	setTopUpTarget(c, model.TopUpTargetTypeOrganization, org.Id)
	return true
}

// validateTopUpTargetCapacity checks the wallet this order settles into.
func validateTopUpTargetCapacity(c *gin.Context, userId int, creditedQuota int) error {
	targetType := getTopUpTargetType(c)
	targetId := userId
	if targetType == model.TopUpTargetTypeOrganization {
		targetId = getTopUpTargetId(c)
	}
	return model.ValidateTopUpTargetCapacity(targetType, targetId, creditedQuota)
}
