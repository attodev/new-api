package common

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateWalletQuota(t *testing.T) {
	assert.NoError(t, ValidateWalletQuota(MaxWalletQuota))
	assert.NoError(t, ValidateWalletQuota(0))
	// Billing can temporarily overdraw a wallet, so only the upper bound applies.
	assert.NoError(t, ValidateWalletQuota(-MaxWalletQuota-1))
	err := ValidateWalletQuota(MaxWalletQuota + 1)
	require.Error(t, err)
	assert.ErrorContains(t, err, fmt.Sprintf("%d", MaxWalletQuota))
}
