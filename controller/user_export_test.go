package controller

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// importWorkbookBody builds a multipart body carrying an xlsx with the given rows.
func importWorkbookBody(t *testing.T, rows [][]any) (*bytes.Buffer, string) {
	t.Helper()
	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	sheet := f.GetSheetName(0)
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		require.NoError(t, err)
		require.NoError(t, f.SetSheetRow(sheet, cell, &row))
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "users.xlsx")
	require.NoError(t, err)
	require.NoError(t, f.Write(part))
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

func TestImportUsersRecordsOneAuditEntry(t *testing.T) {
	db := setupManageUserTestDB(t)
	previousQuota := common.QuotaForNewUser
	common.QuotaForNewUser = 1000
	t.Cleanup(func() { common.QuotaForNewUser = previousQuota })
	operator := createQuotaTestOperator(t, db, common.RoleRootUser)
	_, jwt := createAccessTokenTestSession(t, operator.Id, "user-import-audit-session")
	existing := model.User{Username: "import_existing", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "import-existing-aff"}
	require.NoError(t, db.Create(&existing).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RequestId())
	router.POST("/api/user/import", middleware.AdminAuth(), ImportUsers)

	body, contentType := importWorkbookBody(t, [][]any{
		{"username", "initial_password", "group", "quota"},
		{"import_alpha", "password123", "vip", "250"},
		{"import_beta", "password123", "default", "0"},
		{"import_gamma", "password123", "default", ""},
		{"import_existing", "password123", "default", "5"},
		{"import_nopass", "", "default", "5"},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/user/import", body)
	request.Header.Set("Authorization", "Bearer "+jwt)
	request.Header.Set("Content-Type", contentType)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"granted_quota":1250`)

	requestID := recorder.Header().Get(common.RequestIdKey)
	require.NotEmpty(t, requestID)
	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("request_id = ? AND category = ?", requestID, model.AuditCategoryOperation).Find(&audits).Error)
	require.Len(t, audits, 1, "one import audit, without the AdminAuth fallback entry")
	audit := audits[0]
	assert.Equal(t, "user.import", audit.Action)
	assert.True(t, audit.Success)
	assert.Equal(t, operator.Id, audit.UserId)
	assert.Equal(t, "/api/user/import", audit.Route)
	assert.Equal(t, "Imported 3 users from Excel (skipped 1, errors 1, granted quota 1250)", audit.Content)
	require.NotNil(t, audit.Other.Op)
	params, err := common.Marshal(audit.Other.Op.Params)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"created": 3,
		"skipped": 1,
		"error_count": 1,
		"created_usernames": ["import_alpha", "import_beta", "import_gamma"],
		"created_usernames_total": 3,
		"granted_quota": 1250
	}`, string(params))
}

func TestImportUsersAuditCapsUsernameList(t *testing.T) {
	db := setupManageUserTestDB(t)
	previousQuota := common.QuotaForNewUser
	common.QuotaForNewUser = 0
	t.Cleanup(func() { common.QuotaForNewUser = previousQuota })
	createQuotaTestOperator(t, db, common.RoleRootUser)

	rows := [][]any{{"username", "initial_password"}}
	for i := 0; i < importAuditUsernameLimit+5; i++ {
		rows = append(rows, []any{fmt.Sprintf("bulk_%03d", i), "password123"})
	}
	body, contentType := importWorkbookBody(t, rows)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/import", body)
	c.Request.Header.Set("Content-Type", contentType)
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root-operator")
	c.Set(common.RequestIdKey, "user-import-cap-request")
	ImportUsers(c)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("request_id = ?", "user-import-cap-request").Find(&audits).Error)
	require.Len(t, audits, 1)
	require.NotNil(t, audits[0].Other.Op)
	params, err := common.Marshal(audits[0].Other.Op.Params)
	require.NoError(t, err)
	var decoded struct {
		Created   int      `json:"created"`
		Usernames []string `json:"created_usernames"`
		Total     int      `json:"created_usernames_total"`
	}
	require.NoError(t, common.Unmarshal(params, &decoded))
	assert.Equal(t, importAuditUsernameLimit+5, decoded.Created)
	assert.Len(t, decoded.Usernames, importAuditUsernameLimit)
	assert.Equal(t, importAuditUsernameLimit+5, decoded.Total)
	assert.Equal(t, "bulk_000", decoded.Usernames[0])
}
