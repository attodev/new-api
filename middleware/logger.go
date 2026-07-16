package middleware

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const RouteTagKey = "route_tag"

var sensitiveQueryKeys = map[string]struct{}{
	"authkey":            {},
	"customerkey":        {},
	"message":            {},
	"order_id":           {},
	"orderid":            {},
	"paymentkey":         {},
	"toss_order_id":      {},
	"toss_error_message": {},
	"trade_no":           {},
}

func redactSensitivePath(path string) string {
	for _, prefix := range []string{
		"/api/subscription/toss/confirm/",
		"/api/subscription/toss/fail/",
	} {
		if strings.HasPrefix(path, prefix) {
			remainder := strings.TrimPrefix(path, prefix)
			if suffixIndex := strings.IndexByte(remainder, '/'); suffixIndex >= 0 {
				return prefix + "[REDACTED]" + remainder[suffixIndex:]
			}
			return prefix + "[REDACTED]"
		}
	}
	return path
}

func redactSensitiveQuery(path string) string {
	base, rawQuery, found := strings.Cut(path, "?")
	base = redactSensitivePath(base)
	if !found || rawQuery == "" {
		return base
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// A malformed query must not bypass redaction and reach access logs.
		return base + "?query=redacted"
	}
	for key := range values {
		if _, sensitive := sensitiveQueryKeys[strings.ToLower(key)]; sensitive {
			values.Set(key, "[REDACTED]")
		}
	}
	return base + "?" + values.Encode()
}

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID, _ = param.Keys[common.RequestIdKey].(string)
		}
		tag, _ := param.Keys[RouteTagKey].(string)
		if tag == "" {
			tag = "web"
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			tag,
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			redactSensitiveQuery(param.Path),
		)
	}))
}
