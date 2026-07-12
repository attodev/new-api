package controller

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

const tossPaymentRequestMaxBodyBytes int64 = 64 << 10

// decodeTossPaymentRequestJSON reads the complete authenticated payment request
// before decoding it. Reading to EOF is intentional: a valid JSON object followed
// by an oversized suffix must not bypass the body limit because the JSON decoder
// stopped after the first value.
func decodeTossPaymentRequestJSON(c *gin.Context, dst any) error {
	if c.Request.ContentLength > tossPaymentRequestMaxBodyBytes {
		return common.ErrRequestBodyTooLarge
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, tossPaymentRequestMaxBodyBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			return common.ErrRequestBodyTooLarge
		}
		return err
	}
	return common.Unmarshal(body, dst)
}

func respondTossPaymentRequestBodyTooLarge(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
		"success": false,
		"message": "request body exceeds 64 KiB",
	})
}
