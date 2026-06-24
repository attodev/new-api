package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// BodyStorageCleanup
// /
func BodyStorageCleanup() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		common.CleanupBodyStorage(c)

		// URL
		service.CleanupFileSources(c)
	}
}
