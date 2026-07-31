package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupOrganizationSubscriptionControllerTestDB(t *testing.T) {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.OrganizationSubscriptionPlan{},
		&model.OrganizationUserSubscription{},
		&model.OrganizationSubscriptionPreConsumeRecord{},
	))
}

func TestOrganizationOwnerCanManageSubscriptionPlan(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-sub-plan"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	createRes := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		owner,
		http.MethodPost,
		"/api/organization/subscription/plans",
		`{"title":"Team Basic","subtitle":"Starter","duration_unit":"month","duration_value":1,"total_amount":500}`,
	)
	require.Equal(t, http.StatusOK, createRes.Code)
	require.Contains(t, createRes.Body.String(), `"success":true`)

	var plan model.OrganizationSubscriptionPlan
	require.NoError(t, model.DB.First(&plan, "organization_id = ? AND title = ?", 1, "Team Basic").Error)
	require.Equal(t, int64(500), plan.TotalAmount)
	require.Equal(t, model.SubscriptionResetNever, plan.QuotaResetPeriod)

	updateRes := performOrganizationRequest(
		UpdateOrganizationSubscriptionPlan,
		owner,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/subscription/plans/%d", plan.Id),
		`{"title":"Team Pro","duration_unit":"day","duration_value":7,"total_amount":900}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", plan.Id)},
	)
	require.Equal(t, http.StatusOK, updateRes.Code)
	require.Contains(t, updateRes.Body.String(), `"success":true`)

	require.NoError(t, model.DB.First(&plan, plan.Id).Error)
	require.Equal(t, "Team Pro", plan.Title)
	require.Equal(t, int64(900), plan.TotalAmount)
	require.Equal(t, model.SubscriptionDurationDay, plan.DurationUnit)

	deleteRes := performOrganizationRequest(
		DeleteOrganizationSubscriptionPlan,
		owner,
		http.MethodDelete,
		fmt.Sprintf("/api/organization/subscription/plans/%d", plan.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", plan.Id)},
	)
	require.Equal(t, http.StatusOK, deleteRes.Code)
	require.Contains(t, deleteRes.Body.String(), `"success":true`)

	require.NoError(t, model.DB.First(&plan, plan.Id).Error)
	require.False(t, plan.Enabled)
}

func TestOrganizationSubscriptionPlanAcceptsBoundaryInput(t *testing.T) {
	for _, tc := range []struct {
		name              string
		body              string
		wantTitle         string
		wantSubtitle      string
		wantDurationUnit  string
		wantDurationValue int
		wantCustomSeconds int64
		wantTotalAmount   int64
		wantSortOrder     int
	}{
		{
			name:              "minimum numeric values and default duration value",
			body:              `{"title":"A","duration_unit":"month","duration_value":0,"total_amount":0,"sort_order":0}`,
			wantTitle:         "A",
			wantDurationUnit:  model.SubscriptionDurationMonth,
			wantDurationValue: 1,
			wantTotalAmount:   0,
			wantSortOrder:     0,
		},
		{
			name:              "maximum lengths amount duration and positive sort order",
			body:              fmt.Sprintf(`{"title":%q,"subtitle":%q,"duration_unit":"year","duration_value":%d,"total_amount":%d,"sort_order":%d}`, strings.Repeat("가", MaxOrganizationSubscriptionPlanTitleLength), strings.Repeat("나", MaxOrganizationSubscriptionPlanSubtitleLength), MaxOrganizationSubscriptionDurationValue, MaxOrganizationQuota, MaxOrganizationSubscriptionSortOrder),
			wantTitle:         strings.Repeat("가", MaxOrganizationSubscriptionPlanTitleLength),
			wantSubtitle:      strings.Repeat("나", MaxOrganizationSubscriptionPlanSubtitleLength),
			wantDurationUnit:  model.SubscriptionDurationYear,
			wantDurationValue: MaxOrganizationSubscriptionDurationValue,
			wantTotalAmount:   int64(MaxOrganizationQuota),
			wantSortOrder:     MaxOrganizationSubscriptionSortOrder,
		},
		{
			name:              "minimum custom seconds",
			body:              `{"title":"Custom Min","duration_unit":"custom","custom_seconds":1,"total_amount":1}`,
			wantTitle:         "Custom Min",
			wantDurationUnit:  model.SubscriptionDurationCustom,
			wantDurationValue: 1,
			wantCustomSeconds: 1,
			wantTotalAmount:   1,
		},
		{
			name:              "maximum custom seconds and negative sort order",
			body:              fmt.Sprintf(`{"title":"Custom Max","duration_unit":"custom","custom_seconds":%d,"total_amount":%d,"sort_order":%d}`, MaxOrganizationSubscriptionCustomSeconds, MaxOrganizationQuota, -MaxOrganizationSubscriptionSortOrder),
			wantTitle:         "Custom Max",
			wantDurationUnit:  model.SubscriptionDurationCustom,
			wantDurationValue: 1,
			wantCustomSeconds: MaxOrganizationSubscriptionCustomSeconds,
			wantTotalAmount:   int64(MaxOrganizationQuota),
			wantSortOrder:     -MaxOrganizationSubscriptionSortOrder,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationSubscriptionControllerTestDB(t)
			owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-plan-boundary-" + tc.name}
			org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
			require.NoError(t, model.DB.Create(&owner).Error)
			require.NoError(t, model.DB.Create(&org).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				owner,
				http.MethodPost,
				"/api/organization/subscription/plans",
				tc.body,
			)

			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":true`)

			var plan model.OrganizationSubscriptionPlan
			require.NoError(t, model.DB.First(&plan, "organization_id = ? AND title = ?", 1, tc.wantTitle).Error)
			require.Equal(t, tc.wantTitle, plan.Title)
			require.Equal(t, tc.wantSubtitle, plan.Subtitle)
			require.Equal(t, tc.wantDurationUnit, plan.DurationUnit)
			require.Equal(t, tc.wantDurationValue, plan.DurationValue)
			require.Equal(t, tc.wantCustomSeconds, plan.CustomSeconds)
			require.Equal(t, tc.wantTotalAmount, plan.TotalAmount)
			require.Equal(t, tc.wantSortOrder, plan.SortOrder)
		})
	}
}

func TestOrganizationSubscriptionPlanRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "title too long",
			body:    fmt.Sprintf(`{"title":%q,"duration_unit":"month","duration_value":1,"total_amount":500}`, strings.Repeat("a", MaxOrganizationSubscriptionPlanTitleLength+1)),
			message: "title must be at most 128 characters",
		},
		{
			name:    "title empty",
			body:    `{"title":"   ","duration_unit":"month","duration_value":1,"total_amount":500}`,
			message: "title is required",
		},
		{
			name:    "subtitle too long",
			body:    fmt.Sprintf(`{"title":"Team","subtitle":%q,"duration_unit":"month","duration_value":1,"total_amount":500}`, strings.Repeat("a", MaxOrganizationSubscriptionPlanSubtitleLength+1)),
			message: "subtitle must be at most 255 characters",
		},
		{
			name:    "invalid duration unit",
			body:    `{"title":"Team","duration_unit":"quarter","duration_value":1,"total_amount":500}`,
			message: "duration_unit must be one of: year, month, day, hour, custom",
		},
		{
			name:    "duration value below minimum",
			body:    `{"title":"Team","duration_unit":"month","duration_value":-1,"total_amount":500}`,
			message: "duration_value must be between 0 and 1200",
		},
		{
			name:    "duration value too large",
			body:    fmt.Sprintf(`{"title":"Team","duration_unit":"month","duration_value":%d,"total_amount":500}`, MaxOrganizationSubscriptionDurationValue+1),
			message: "duration_value must be between 0 and 1200",
		},
		{
			name:    "custom seconds below minimum",
			body:    `{"title":"Team","duration_unit":"custom","custom_seconds":0,"total_amount":500}`,
			message: "custom_seconds must be between 1 and 31536000",
		},
		{
			name:    "custom seconds too large",
			body:    fmt.Sprintf(`{"title":"Team","duration_unit":"custom","custom_seconds":%d,"total_amount":500}`, MaxOrganizationSubscriptionCustomSeconds+1),
			message: "custom_seconds must be between 1 and 31536000",
		},
		{
			name:    "non custom custom seconds below minimum",
			body:    `{"title":"Team","duration_unit":"month","duration_value":1,"custom_seconds":-1,"total_amount":500}`,
			message: "custom_seconds must be between 0 and 31536000",
		},
		{
			name:    "total amount below minimum",
			body:    `{"title":"Team","duration_unit":"month","duration_value":1,"total_amount":-1}`,
			message: fmt.Sprintf("total_amount must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "total amount too large",
			body:    fmt.Sprintf(`{"title":"Team","duration_unit":"month","duration_value":1,"total_amount":%d}`, MaxOrganizationQuota+1),
			message: fmt.Sprintf("total_amount must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "sort order too small",
			body:    fmt.Sprintf(`{"title":"Team","duration_unit":"month","duration_value":1,"total_amount":500,"sort_order":%d}`, -MaxOrganizationSubscriptionSortOrder-1),
			message: "sort_order must be between -1000000 and 1000000",
		},
		{
			name:    "sort order too large",
			body:    fmt.Sprintf(`{"title":"Team","duration_unit":"month","duration_value":1,"total_amount":500,"sort_order":%d}`, MaxOrganizationSubscriptionSortOrder+1),
			message: "sort_order must be between -1000000 and 1000000",
		},
		{
			name:    "unsupported field",
			body:    `{"title":"Team","duration_unit":"month","duration_value":1,"total_amount":500,"quota_reset_period":"day"}`,
			message: "unsupported field: quota_reset_period",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationSubscriptionControllerTestDB(t)
			owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-invalid-plan-" + tc.name}
			org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
			require.NoError(t, model.DB.Create(&owner).Error)
			require.NoError(t, model.DB.Create(&org).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				owner,
				http.MethodPost,
				"/api/organization/subscription/plans",
				tc.body,
			)

			requireOrganizationApiError(t, res, tc.message)
		})
	}
}

func TestOrganizationMemberCannotManageSubscriptionPlan(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-sub-plan"}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		member,
		http.MethodPost,
		"/api/organization/subscription/plans",
		`{"title":"Team Basic","duration_unit":"month","duration_value":1,"total_amount":500}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "organization admin permission required")
}

func TestOrganizationRootCanManageSelectedOrganizationSubscription(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-sub-plan"}
	org := model.Organization{Id: 7, Name: "Selected", OwnerUserId: 2, Quota: 1000, Status: model.OrganizationStatusEnabled}
	member := model.User{Id: 2, Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 7, OrganizationRole: model.OrganizationRoleMember, AffCode: "selected-member"}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	createRes := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		root,
		http.MethodPost,
		"/api/organization/subscription/plans?organization_id=7",
		`{"title":"Root Plan","duration_unit":"month","duration_value":1,"total_amount":300}`,
	)
	require.Equal(t, http.StatusOK, createRes.Code)
	require.Contains(t, createRes.Body.String(), `"success":true`)

	var plan model.OrganizationSubscriptionPlan
	require.NoError(t, model.DB.First(&plan, "organization_id = ? AND title = ?", 7, "Root Plan").Error)

	assignRes := performOrganizationRequest(
		AssignOrganizationUserSubscription,
		root,
		http.MethodPut,
		fmt.Sprintf("/api/organization/subscription/users/%d?organization_id=7", member.Id),
		fmt.Sprintf(`{"plan_id":%d}`, plan.Id),
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", member.Id)},
	)
	require.Equal(t, http.StatusOK, assignRes.Code)
	require.Contains(t, assignRes.Body.String(), `"success":true`)

	var sub model.OrganizationUserSubscription
	require.NoError(t, model.DB.First(&sub, "organization_id = ? AND user_id = ? AND status = ?", 7, member.Id, "active").Error)
	require.Equal(t, plan.Id, sub.PlanId)
}

func TestOrganizationRootSubscriptionPlanRequiresOrganizationId(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-no-org-id"}
	require.NoError(t, model.DB.Create(&root).Error)

	res := performOrganizationRequest(
		ListOrganizationSubscriptionPlans,
		root,
		http.MethodGet,
		"/api/organization/subscription/plans",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "organization_id is required")
}

func TestOrganizationAdminCannotAssignSubscriptionOutsideOrganization(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-sub"}
	outside := model.User{Username: "outside", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, AffCode: "outside-sub"}
	plan := model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Team Basic", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 500, Enabled: true}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&outside).Error)
	require.NoError(t, model.DB.Create(&plan).Error)

	res := performOrganizationRequest(
		AssignOrganizationUserSubscription,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/subscription/users/%d", outside.Id),
		fmt.Sprintf(`{"plan_id":%d}`, plan.Id),
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", outside.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "organization target permission denied")
}

func TestOrganizationSubscriptionAssignmentAcceptsBoundaryPlanId(t *testing.T) {
	for _, tc := range []struct {
		name   string
		planId int
	}{
		{name: "minimum plan id", planId: 1},
		{name: "maximum plan id", planId: MaxOrganizationReferenceId},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationSubscriptionControllerTestDB(t)
			admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-plan-id-boundary-" + tc.name}
			member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-plan-id-boundary-" + tc.name}
			plan := model.OrganizationSubscriptionPlan{Id: tc.planId, OrganizationId: 1, Title: "Boundary Plan", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 500, Enabled: true}
			require.NoError(t, model.DB.Create(&admin).Error)
			require.NoError(t, model.DB.Create(&member).Error)
			require.NoError(t, model.DB.Create(&plan).Error)

			res := performOrganizationRequest(
				AssignOrganizationUserSubscription,
				admin,
				http.MethodPut,
				fmt.Sprintf("/api/organization/subscription/users/%d", member.Id),
				fmt.Sprintf(`{"plan_id":%d}`, tc.planId),
				gin.Param{Key: "id", Value: fmt.Sprintf("%d", member.Id)},
			)

			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":true`)

			var sub model.OrganizationUserSubscription
			require.NoError(t, model.DB.First(&sub, "organization_id = ? AND user_id = ? AND plan_id = ? AND status = ?", 1, member.Id, tc.planId, "active").Error)
			require.Equal(t, int64(500), sub.AmountTotal)
		})
	}
}

func TestOrganizationSubscriptionAssignmentRejectsInvalidPlanId(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "zero plan id",
			body:    `{"plan_id":0}`,
			message: "plan_id must be greater than 0",
		},
		{
			name:    "plan id too large",
			body:    fmt.Sprintf(`{"plan_id":%d}`, MaxOrganizationReferenceId+1),
			message: "plan_id must be at most 1000000000",
		},
		{
			name:    "unsupported field",
			body:    `{"plan_id":1,"quota":10}`,
			message: "unsupported field: quota",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationSubscriptionControllerTestDB(t)
			admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-invalid-sub-" + tc.name}
			member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-invalid-sub-" + tc.name}
			require.NoError(t, model.DB.Create(&admin).Error)
			require.NoError(t, model.DB.Create(&member).Error)

			res := performOrganizationRequest(
				AssignOrganizationUserSubscription,
				admin,
				http.MethodPut,
				fmt.Sprintf("/api/organization/subscription/users/%d", member.Id),
				tc.body,
				gin.Param{Key: "id", Value: fmt.Sprintf("%d", member.Id)},
			)

			requireOrganizationApiError(t, res, tc.message)
		})
	}
}

func TestOrganizationAdminCanCancelActiveSubscription(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-cancel"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-cancel"}
	plan := model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Team Basic", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 500, Enabled: true}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&plan).Error)
	sub := model.OrganizationUserSubscription{OrganizationId: 1, UserId: member.Id, PlanId: plan.Id, AmountTotal: 500, StartTime: time.Now().Unix(), EndTime: time.Now().Add(24 * time.Hour).Unix(), Status: "active"}
	require.NoError(t, model.DB.Create(&sub).Error)

	res := performOrganizationRequest(
		CancelOrganizationUserSubscription,
		admin,
		http.MethodDelete,
		fmt.Sprintf("/api/organization/subscription/users/%d", member.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", member.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.OrganizationUserSubscription
	require.NoError(t, model.DB.First(&reloaded, sub.Id).Error)
	require.Equal(t, "cancelled", reloaded.Status)
}

func TestOrganizationAdminCanCancelAndReassignSubscription(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-reassign"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-reassign"}
	plan := model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Team Basic", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 500, Enabled: true}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&plan).Error)
	previousSub := model.OrganizationUserSubscription{OrganizationId: 1, UserId: member.Id, PlanId: plan.Id, AmountTotal: 500, StartTime: time.Now().Unix(), EndTime: time.Now().Add(24 * time.Hour).Unix(), Status: "active"}
	require.NoError(t, model.DB.Create(&previousSub).Error)

	cancelRes := performOrganizationRequest(
		CancelOrganizationUserSubscription,
		admin,
		http.MethodDelete,
		fmt.Sprintf("/api/organization/subscription/users/%d", member.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", member.Id)},
	)
	require.Equal(t, http.StatusOK, cancelRes.Code)
	require.Contains(t, cancelRes.Body.String(), `"success":true`)

	reassignRes := performOrganizationRequest(
		AssignOrganizationUserSubscription,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/subscription/users/%d", member.Id),
		fmt.Sprintf(`{"plan_id":%d}`, plan.Id),
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", member.Id)},
	)
	require.Equal(t, http.StatusOK, reassignRes.Code)
	require.Contains(t, reassignRes.Body.String(), `"success":true`)

	var activeSubs []model.OrganizationUserSubscription
	require.NoError(t, model.DB.Where("organization_id = ? AND user_id = ? AND status = ?", 1, member.Id, "active").Find(&activeSubs).Error)
	require.Len(t, activeSubs, 1)
	require.NotEqual(t, previousSub.Id, activeSubs[0].Id)
	require.Equal(t, int64(500), activeSubs[0].AmountTotal)

	var cancelledSub model.OrganizationUserSubscription
	require.NoError(t, model.DB.First(&cancelledSub, previousSub.Id).Error)
	require.Equal(t, "cancelled", cancelledSub.Status)
}

// ---------------------------------------------------------------------------
// ListOrganizationSubscriptionPlans — success path with plans present
// ---------------------------------------------------------------------------

func TestOrganizationAdminCanListSubscriptionPlans(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-list-plans"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	plan1 := model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Starter", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 500, Enabled: true}
	plan2 := model.OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Pro", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&plan1).Error)
	require.NoError(t, model.DB.Create(&plan2).Error)

	res := performOrganizationRequest(
		ListOrganizationSubscriptionPlans,
		owner,
		http.MethodGet,
		"/api/organization/subscription/plans",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"success":true`)
	require.Contains(t, body, `"Starter"`)
	require.Contains(t, body, `"Pro"`)
}

// ---------------------------------------------------------------------------
// UpdateOrganization — missing branches
// ---------------------------------------------------------------------------

func TestOrganizationUpdateRejectsNoFields(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-update-no-fields"}
	org := model.Organization{Name: "Acme", OwnerUserId: 1, Quota: 100, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganization,
		root,
		http.MethodPatch,
		fmt.Sprintf("/api/organizations/%d", org.Id),
		`{}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", org.Id)},
	)

	requireOrganizationApiError(t, res, "no organization fields to update")
}

func TestOrganizationUpdateRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-update-bad-id"}
	require.NoError(t, model.DB.Create(&root).Error)

	res := performOrganizationRequest(
		UpdateOrganization,
		root,
		http.MethodPatch,
		"/api/organizations/not-a-number",
		`{"quota":100}`,
		gin.Param{Key: "id", Value: "not-a-number"},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

// ---------------------------------------------------------------------------
// UpdateOrganizationUser — missing branches
// ---------------------------------------------------------------------------

func TestOrganizationAdminUpdateUserRejectsNoFields(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-update-no-fields"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-update-no-fields"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", member.Id),
		`{}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", member.Id)},
	)

	requireOrganizationApiError(t, res, "no organization user fields to update")
}

func TestOrganizationAdminUpdateUserRejectsOutsideOrg(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-cross-org"}
	outsider := model.User{Username: "outsider", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, AffCode: "outsider-cross-org"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&outsider).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", outsider.Id),
		`{"quota":100}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", outsider.Id)},
	)

	requireOrganizationApiError(t, res, "organization target permission denied")
}

func TestOrganizationAdminUpdateUserRejectsNonExistentUser(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-ghost-user"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		"/api/organization/users/99999",
		`{"quota":100}`,
		gin.Param{Key: "id", Value: "99999"},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

// ---------------------------------------------------------------------------
// DeleteOrganization — invalid param
// ---------------------------------------------------------------------------

func TestOrganizationDeleteRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-delete-bad-id"}
	require.NoError(t, model.DB.Create(&root).Error)

	res := performOrganizationRequest(
		DeleteOrganization,
		root,
		http.MethodDelete,
		"/api/organizations/not-a-number",
		"",
		gin.Param{Key: "id", Value: "not-a-number"},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

// ---------------------------------------------------------------------------
// AssignOrganizationUser — non-existent target
// ---------------------------------------------------------------------------

func TestOrganizationOwnerAssignNonExistentUser(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-assign-ghost"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		"/api/organization/users/99999/membership",
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: "99999"},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

// ---------------------------------------------------------------------------
// UpdateOrganizationSubscriptionPlan — missing branches
// ---------------------------------------------------------------------------

func TestOrganizationSubscriptionPlanUpdateRejectsInvalidId(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-update-plan-bad-id"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganizationSubscriptionPlan,
		owner,
		http.MethodPatch,
		"/api/organization/subscription/plans/not-a-number",
		`{"title":"X","duration_unit":"month","duration_value":1,"total_amount":100}`,
		gin.Param{Key: "id", Value: "not-a-number"},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

// ---------------------------------------------------------------------------
// CancelOrganizationUserSubscription — non-admin path and invalid param
// ---------------------------------------------------------------------------

func TestOrganizationMemberCannotCancelSubscription(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-cancel-sub"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "target-cancel-sub"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		CancelOrganizationUserSubscription,
		member,
		http.MethodDelete,
		fmt.Sprintf("/api/organization/subscription/users/%d", target.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	requireOrganizationApiError(t, res, "organization admin permission required")
}

func TestOrganizationCancelSubscriptionRejectsInvalidId(t *testing.T) {
	setupOrganizationSubscriptionControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-cancel-bad-id"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		CancelOrganizationUserSubscription,
		admin,
		http.MethodDelete,
		"/api/organization/subscription/users/not-a-number",
		"",
		gin.Param{Key: "id", Value: "not-a-number"},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}
