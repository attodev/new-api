package controller

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// fork: O3 — one operation audit entry per organization mutation. Content is
// English (upstream's auditContentTemplates is not edited); action names are
// "organization.<verb>". markAuditLogged suppresses the generic RootAuth entry
// for /api/organizations and is harmless on UserAuth routes.
func recordOrganizationAudit(c *gin.Context, action string, content string, params map[string]any) {
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params, auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
}

// formatOrganizationAuditFields renders changed fields as "key=value, ..." in
// key order, for the audit content sentence.
func formatOrganizationAuditFields(updates map[string]interface{}) string {
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, updates[key]))
	}
	return strings.Join(parts, ", ")
}

// organizationAuditParams merges the changed fields into the audit params.
func organizationAuditParams(base map[string]any, updates map[string]interface{}) map[string]any {
	params := make(map[string]any, len(base)+len(updates))
	for key, value := range updates {
		params[key] = value
	}
	for key, value := range base {
		params[key] = value
	}
	return params
}
