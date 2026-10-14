package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// fork: O3 — the controller-side default sidebar config carries the
// organization section too (model.AddOrganizationSidebarDefaults).
func TestGenerateDefaultSidebarConfigIncludesOrganizationSection(t *testing.T) {
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		var config map[string]map[string]bool
		if err := common.UnmarshalJsonStr(generateDefaultSidebarConfig(role), &config); err != nil {
			t.Fatalf("role %d: %v", role, err)
		}
		for _, key := range []string{"enabled", "users", "dashboard", "subscription", "log", "task"} {
			if !config["organization"][key] {
				t.Errorf("role %d: organization.%s = false, want true", role, key)
			}
		}
	}
}
