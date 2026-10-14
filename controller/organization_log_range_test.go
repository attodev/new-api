package controller

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 — organization admins are ordinary users: their log searches get
// upstream's non-admin log range limit (limitLogTimeRange), like GetUserLogs.

func seedOrganizationLogRangeTest(t *testing.T) (model.User, model.User) {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}, &model.Task{}))
	admin := model.User{Id: 1, Username: "admin", Password: "x", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "admin-range"}
	root := model.User{Id: 9, Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root-range"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	for _, row := range []any{&admin, &root, &org} {
		require.NoError(t, model.DB.Create(row).Error)
	}
	return admin, root
}

func TestOrganizationLogsApplyTheLogRangeLimit(t *testing.T) {
	admin, root := seedOrganizationLogRangeTest(t)
	now := time.Now().Unix()
	old := now - 400*24*60*60
	logs := []model.Log{
		{UserId: admin.Id, Username: admin.Username, Type: model.LogTypeConsume, CreatedAt: now, ModelName: "recent-model", Other: "{}"},
		{UserId: admin.Id, Username: admin.Username, Type: model.LogTypeConsume, CreatedAt: old, ModelName: "old-model", Other: "{}"},
	}
	require.NoError(t, model.LOG_DB.Create(&logs).Error)
	wantErr := fmt.Sprintf("The time range cannot exceed %d days", 365)

	res := performOrganizationRequest(GetOrganizationLogs, admin, http.MethodGet,
		fmt.Sprintf("/api/organization/logs?start_timestamp=%d&end_timestamp=%d", old-1, now), "")
	requireOrganizationApiError(t, res, wantErr)

	// A missing start reaches back the whole range only.
	res = performOrganizationRequest(GetOrganizationLogs, admin, http.MethodGet, "/api/organization/logs", "")
	require.Contains(t, res.Body.String(), "recent-model")
	require.NotContains(t, res.Body.String(), "old-model")

	// Root (platform admin) is not limited, as in upstream's log endpoints.
	res = performOrganizationRequest(GetOrganizationLogs, root, http.MethodGet,
		fmt.Sprintf("/api/organization/logs?organization_id=1&start_timestamp=%d&end_timestamp=%d", old-1, now), "")
	require.Contains(t, res.Body.String(), "old-model")

	res = performOrganizationRequest(GetOrganizationTask, admin, http.MethodGet,
		fmt.Sprintf("/api/organization/task?start_timestamp=%d&end_timestamp=%d", old-1, now), "")
	requireOrganizationApiError(t, res, wantErr)

	// Midjourney timestamps are milliseconds.
	res = performOrganizationRequest(GetOrganizationMidjourney, admin, http.MethodGet,
		fmt.Sprintf("/api/organization/mj?start_timestamp=%d&end_timestamp=%d", (old-1)*1000, now*1000), "")
	requireOrganizationApiError(t, res, wantErr)
}

func TestOrganizationMidjourneyLogsClampAMissingStart(t *testing.T) {
	admin, _ := seedOrganizationLogRangeTest(t)
	nowMs := time.Now().UnixMilli()
	tasks := []model.Midjourney{
		{UserId: admin.Id, MjId: "mj-recent", SubmitTime: nowMs},
		{UserId: admin.Id, MjId: "mj-old", SubmitTime: nowMs - 400*24*60*60*1000},
	}
	require.NoError(t, model.DB.Create(&tasks).Error)

	res := performOrganizationRequest(GetOrganizationMidjourney, admin, http.MethodGet, "/api/organization/mj", "", gin.Param{})
	require.Contains(t, res.Body.String(), "mj-recent")
	require.NotContains(t, res.Body.String(), "mj-old")
}
