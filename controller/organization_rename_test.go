package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 (review M1) — root can rename an organization through
// PATCH /api/organizations/:id with the same validation as create.
func TestOrganizationRootCanRenameOrganization(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Id: 9, Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root-rename"}
	first := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Status: model.OrganizationStatusEnabled}
	second := model.Organization{Id: 2, Name: "Globex", OwnerUserId: 2, Status: model.OrganizationStatusEnabled}
	for _, row := range []any{&root, &first, &second} {
		require.NoError(t, model.DB.Create(row).Error)
	}
	patch := func(body string) string {
		return performOrganizationRequest(UpdateOrganization, root, http.MethodPatch, "/api/organizations/1", body,
			gin.Param{Key: "id", Value: "1"}).Body.String()
	}

	require.Contains(t, patch(`{"name":"  Acme Labs  ","description":"renamed"}`), `"success":true`)
	var reloaded model.Organization
	require.NoError(t, model.DB.First(&reloaded, 1).Error)
	require.Equal(t, "Acme Labs", reloaded.Name)
	require.Equal(t, "renamed", reloaded.Description)
	var audits int64
	require.NoError(t, model.DB.Model(&model.AuditLog{}).Where("action = ? AND content LIKE ?", "organization.update", "%name=Acme Labs%").Count(&audits).Error)
	require.Equal(t, int64(1), audits)

	// Keeping the current name is not a conflict.
	require.Contains(t, patch(`{"name":"Acme Labs"}`), `"success":true`)

	for _, tc := range []struct{ body, message string }{
		{`{"name":"Globex"}`, "organization name already exists"},
		{`{"name":"   "}`, "name is required"},
		{fmt.Sprintf(`{"name":%q}`, strings.Repeat("a", MaxOrganizationNameLength+1)), "name must be at most 64 characters"},
	} {
		body := patch(tc.body)
		require.Contains(t, body, `"success":false`, tc.body)
		require.Contains(t, body, tc.message, tc.body)
	}
	require.NoError(t, model.DB.First(&reloaded, 1).Error)
	require.Equal(t, "Acme Labs", reloaded.Name)
}
