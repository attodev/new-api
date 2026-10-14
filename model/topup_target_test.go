package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func cleanupTopUpTargetTest(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM organizations")
	})
}

func TestCreditTopUpTargetCreditsOrganizationNotPayer(t *testing.T) {
	cleanupTopUpTargetTest(t)
	owner := &User{Id: 701, Username: "topup_target_owner", Status: common.UserStatusEnabled, Quota: 10}
	require.NoError(t, DB.Create(owner).Error)
	org := &Organization{Name: "topup-target-org", OwnerUserId: owner.Id, Quota: 100, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(org).Error)
	topUp := &TopUp{UserId: owner.Id, TargetType: TopUpTargetTypeOrganization, TargetId: org.Id, TradeNo: "tt-org-1", Status: common.TopUpStatusPending}

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 50, nil) }))

	var gotOrg Organization
	require.NoError(t, DB.First(&gotOrg, org.Id).Error)
	require.EqualValues(t, 150, gotOrg.Quota)
	var gotOwner User
	require.NoError(t, DB.First(&gotOwner, owner.Id).Error)
	require.Equal(t, 10, gotOwner.Quota, "payer wallet must not change for an organization target")
}

func TestCreditTopUpTargetOrganizationStopsAtWalletCeiling(t *testing.T) {
	cleanupTopUpTargetTest(t)
	org := &Organization{Name: "topup-ceiling-org", OwnerUserId: 1, Quota: int64(common.MaxWalletQuota) - 40, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(org).Error)
	topUp := &TopUp{UserId: 1, TargetType: TopUpTargetTypeOrganization, TargetId: org.Id}

	// First credit fits exactly, the second would overflow the ceiling.
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 40, nil) }))
	err := DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 1, nil) })
	require.True(t, errors.Is(err, ErrTopUpQuotaLimitExceeded), "got %v", err)
	var gotOrg Organization
	require.NoError(t, DB.First(&gotOrg, org.Id).Error)
	require.EqualValues(t, int64(common.MaxWalletQuota), gotOrg.Quota)

	require.True(t, errors.Is(ValidateOrganizationWalletCapacity(org.Id, 1), ErrTopUpQuotaLimitExceeded))
	require.Error(t, ValidateTopUpTargetCapacity(TopUpTargetTypeOrganization, org.Id, 0), "a zero credit is invalid")
}

func TestCreditTopUpTargetUserPathUnchanged(t *testing.T) {
	cleanupTopUpTargetTest(t)
	user := &User{Id: 702, Username: "topup_target_user", Status: common.UserStatusEnabled, Quota: 5}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{UserId: user.Id} // no target fields: legacy rows
	require.Equal(t, TopUpTargetTypeUser, topUp.EffectiveTargetType())
	require.Equal(t, user.Id, topUp.EffectiveTargetId())
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 7, nil) }))
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, 12, got.Quota)
}

func TestOrganizationTopUpsListOnlyOrganizationOrders(t *testing.T) {
	cleanupTopUpTargetTest(t)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TargetType: TopUpTargetTypeOrganization, TargetId: 9, TradeNo: "org-9-a", Status: common.TopUpStatusSuccess, CreateTime: 100}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TargetType: TopUpTargetTypeOrganization, TargetId: 10, TradeNo: "org-10-a", Status: common.TopUpStatusSuccess, CreateTime: 100}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "user-1-a", Status: common.TopUpStatusSuccess, CreateTime: 100}).Error)
	topups, total, err := GetOrganizationTopUps(9, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "org-9-a", topups[0].TradeNo)
	found, total, err := SearchOrganizationTopUps(9, "%org-9%", &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, found, 1)
}
