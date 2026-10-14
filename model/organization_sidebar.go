package model

// fork: O3 — the organization sidebar section. Hooks: controller/user.go
// generateDefaultSidebarConfig and model/user.go generateDefaultSidebarConfigForRole.
// The keys match the web DEFAULT_SIDEBAR_MODULES.organization block; the
// sidebar still shows the section only to root and organization admins.

// AddOrganizationSidebarDefaults adds the organization section to a default
// per-user sidebar config.
func AddOrganizationSidebarDefaults(config map[string]any) {
	config["organization"] = map[string]any{
		"enabled":      true,
		"users":        true,
		"dashboard":    true,
		"subscription": true,
		"log":          true,
		"task":         true,
	}
}
