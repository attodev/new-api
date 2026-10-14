package middleware

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// RequestTiming records the moment the gateway first sees the request and
// wraps the response writer so the moment the last byte is written to the
// client can be recovered later (see common.GetLastWriteTime). It must be
// registered before any other middleware so downstream auth/rate-limit/
// channel-selection time is included in the resulting e2e_ms metric.
func RequestTiming() gin.HandlerFunc {
	return func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, time.Now())
		c.Writer = &common.TimingResponseWriter{ResponseWriter: c.Writer}
		c.Next()
	}
}
