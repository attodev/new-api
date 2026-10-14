package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestDefaultSidebarConfigIncludesOrganizationSection(t *testing.T) {
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		var config map[string]map[string]bool
		if err := common.UnmarshalJsonStr(generateDefaultSidebarConfigForRole(role), &config); err != nil {
			t.Fatalf("role %d: %v", role, err)
		}
		section := config["organization"]
		for _, key := range []string{"enabled", "users", "dashboard", "subscription", "log", "task"} {
			if !section[key] {
				t.Errorf("role %d: organization.%s = false, want true", role, key)
			}
		}
	}
}
