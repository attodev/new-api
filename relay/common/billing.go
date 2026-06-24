package common

import "github.com/gin-gonic/gin"

// BillingSettler
// service.BillingSession RelayInfo
type BillingSettler interface {
	// Settle delta = actualQuota - preConsumedQuota
	// /
	Settle(actualQuota int) error

	// Refund +
	// gopool
	Refund(c *gin.Context)

	// NeedsRefund
	NeedsRefund() bool

	// GetPreConsumedQuota 0
	GetPreConsumedQuota() int

	// Reserve
	Reserve(targetQuota int) error
}
