package tokenkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWarmIsIdempotentAndLeavesCountWorking(t *testing.T) {
	Warm()
	Warm()
	assert.Greater(t, Count("gpt-4o", "hello"), 0)
	assert.Greater(t, Count("gpt-4", "hello"), 0)
}
