package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupQuietTokenAuthDB gives TokenAuth an empty token table so an unknown key
// is rejected with 401. model.InitLogDB (no LOG_SQL_DSN, non-master) is used
// only for its initCol side effect, so token lookups use a valid key column.
// GORM and SysLog output are discarded: the "record not found" lookup is the
// expected path here, not a failure worth printing.
func setupQuietTokenAuthDB(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	common.LogWriterMu.Lock()
	previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = io.Discard, io.Discard
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetMainDatabaseType(previousType)
		common.SetLogDatabaseType(previousLogType)
		common.RedisEnabled, common.IsMasterNode = previousRedis, previousMaster
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = previousWriter, previousErrorWriter
		common.LogWriterMu.Unlock()
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	t.Setenv("LOG_SQL_DSN", "")
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled, common.IsMasterNode = false, false
	require.NoError(t, model.InitLogDB())
}

// Ported from team 2d1169e84. Team tests an isGeminiQueryKeyPath helper that
// develop does not have; develop inlines the path check in TokenAuth, so this
// drives TokenAuth directly and asserts the "?key=" -> Authorization promotion.
// The key is unknown, so every request is then rejected with 401.
func TestTokenAuthPromotesGeminiQueryKeyOnModelsPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupQuietTokenAuthDB(t)

	cases := []struct {
		target string
		want   string
	}{
		{"/v1/models?key=sk-x", "Bearer sk-x"},
		{"/v1/models/gemini-3-flash-preview?key=sk-x", "Bearer sk-x"},
		{"/v1beta/models?key=sk-x", "Bearer sk-x"},
		{"/v1beta/models/x?key=sk-x", "Bearer sk-x"},
		{"/v1beta/openai/models?key=sk-x", "Bearer sk-x"},
		{"/v1/chat/completions?key=sk-x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, tc.target, nil)

			TokenAuth()(c)

			assert.Equal(t, tc.want, c.Request.Header.Get("Authorization"))
			assert.True(t, c.IsAborted())
			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}
