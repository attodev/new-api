package service

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func insertTokenForPreConsume(t *testing.T, remainQuota int, unlimited bool) *model.Token {
	t.Helper()
	tok := &model.Token{
		UserId:         1,
		Key:            t.Name(),
		Status:         common.TokenStatusEnabled,
		RemainQuota:    remainQuota,
		UnlimitedQuota: unlimited,
	}
	require.NoError(t, model.DB.Create(tok).Error)
	return tok
}

func TestPreConsumeTokenQuota_RejectsWhenInsufficient(t *testing.T) {
	truncate(t)
	tok := insertTokenForPreConsume(t, 10, false)

	info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key}
	err := PreConsumeTokenQuota(info, 40)
	require.Error(t, err)

	var reloaded model.Token
	require.NoError(t, model.DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 10, reloaded.RemainQuota, "a rejected pre-consume must not touch the balance")
}

func TestPreConsumeTokenQuota_SucceedsWhenEnough(t *testing.T) {
	truncate(t)
	tok := insertTokenForPreConsume(t, 100, false)

	info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key}
	err := PreConsumeTokenQuota(info, 40)
	require.NoError(t, err)

	var reloaded model.Token
	require.NoError(t, model.DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 60, reloaded.RemainQuota)
}

// TestPreConsumeTokenQuota_ConcurrentRequestsCannotOverspend is the
// regression test for the race this fix closes: previously PreConsumeTokenQuota
// read the balance, checked it, and only then decremented - three separate
// steps a concurrent request could interleave with. Only as many concurrent
// pre-consumes as the balance actually covers must succeed.
func TestPreConsumeTokenQuota_ConcurrentRequestsCannotOverspend(t *testing.T) {
	truncate(t)
	tok := insertTokenForPreConsume(t, 100, false)

	const cost = 30
	const attempts = 10 // only floor(100/30) = 3 may succeed

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key}
			if err := PreConsumeTokenQuota(info, cost); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 3, successes)

	var reloaded model.Token
	require.NoError(t, model.DB.First(&reloaded, tok.Id).Error)
	require.GreaterOrEqual(t, reloaded.RemainQuota, 0)
}

func TestPreConsumeTokenQuota_UnlimitedTokenAlwaysSucceeds(t *testing.T) {
	truncate(t)
	tok := insertTokenForPreConsume(t, 5, true)

	info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key, TokenUnlimited: true}
	err := PreConsumeTokenQuota(info, 500)
	require.NoError(t, err)
}
