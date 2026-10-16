package service

import (
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPreConsumeTokenTest uses the package TestMain SQLite database and pins
// the database-only reservation path (no Redis cache, no batch updater).
func setupPreConsumeTokenTest(t *testing.T) {
	t.Helper()
	truncate(t)
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
	})
}

func insertTokenForPreConsume(t *testing.T, remainQuota int, unlimited bool) *model.Token {
	t.Helper()
	tok := &model.Token{
		UserId:         1,
		Key:            t.Name(),
		Name:           "preconsume_token",
		Status:         common.TokenStatusEnabled,
		RemainQuota:    remainQuota,
		UnlimitedQuota: unlimited,
	}
	require.NoError(t, model.DB.Create(tok).Error)
	return tok
}

func reloadTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var reloaded model.Token
	require.NoError(t, model.DB.First(&reloaded, id).Error)
	return reloaded.RemainQuota
}

func TestPreConsumeTokenQuotaRejectsWhenInsufficient(t *testing.T) {
	setupPreConsumeTokenTest(t)
	tok := insertTokenForPreConsume(t, 10, false)

	info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key}
	require.Error(t, PreConsumeTokenQuota(info, 40))
	require.Equal(t, 10, reloadTokenRemainQuota(t, tok.Id), "a rejected pre-consume must not touch the balance")
}

func TestPreConsumeTokenQuotaSucceedsWhenEnough(t *testing.T) {
	setupPreConsumeTokenTest(t)
	tok := insertTokenForPreConsume(t, 100, false)

	info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key}
	require.NoError(t, PreConsumeTokenQuota(info, 40))
	require.Equal(t, 60, reloadTokenRemainQuota(t, tok.Id))
}

func TestPreConsumeTokenQuotaRejectsNegativeQuota(t *testing.T) {
	setupPreConsumeTokenTest(t)
	tok := insertTokenForPreConsume(t, 100, false)

	info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key}
	require.Error(t, PreConsumeTokenQuota(info, -1))
	require.Equal(t, 100, reloadTokenRemainQuota(t, tok.Id), "a negative pre-consume must not credit the token")
}

// TestPreConsumeTokenQuotaConcurrentRequestsCannotOverspend: the check and the
// deduction are a single conditional update, so concurrent pre-consumes can
// only succeed as far as the balance covers them.
func TestPreConsumeTokenQuotaConcurrentRequestsCannotOverspend(t *testing.T) {
	setupPreConsumeTokenTest(t)
	tok := insertTokenForPreConsume(t, 100, false)

	const cost = 30
	const attempts = 10 // only floor(100/30) = 3 may succeed

	start := make(chan struct{})
	var ready, done sync.WaitGroup
	var successes, failures atomic.Int32
	for i := 0; i < attempts; i++ {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key}
			ready.Done()
			<-start
			if err := PreConsumeTokenQuota(info, cost); err == nil {
				successes.Add(1)
			} else {
				failures.Add(1)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()

	require.Equal(t, int32(3), successes.Load())
	require.Equal(t, int32(attempts-3), failures.Load())
	require.Equal(t, 10, reloadTokenRemainQuota(t, tok.Id))
}

func TestPreConsumeTokenQuotaUnlimitedTokenAlwaysSucceeds(t *testing.T) {
	setupPreConsumeTokenTest(t)
	tok := insertTokenForPreConsume(t, 5, true)

	info := &relaycommon.RelayInfo{TokenId: tok.Id, TokenKey: tok.Key, TokenUnlimited: true}
	require.NoError(t, PreConsumeTokenQuota(info, 500))
}

// TestBillingSessionShouldTrustUsesTrustQuotaSetting pins the wallet trust
// bypass to quota_setting.trust_quota_usd: both the wallet and any limited
// token must strictly exceed the threshold, 0 (or an invalid runtime value)
// disables the bypass, and organization wallets / subscriptions never bypass.
func TestBillingSessionShouldTrustUsesTrustQuotaSetting(t *testing.T) {
	previousTrust, previousUnit := operation_setting.GetQuotaSetting().TrustQuotaUSD, common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		operation_setting.GetQuotaSetting().TrustQuotaUSD = previousTrust
		common.QuotaPerUnit = previousUnit
	})

	cases := []struct {
		name        string
		trustUSD    float64
		wallet      int
		token       int
		unlimited   bool
		force       bool
		funding     FundingSource
		wantTrusted bool
	}{
		{"default 10 USD above threshold", 10, 5500000, 5500000, false, false, &WalletFunding{userId: 1}, true},
		{"custom threshold", 20, 5500000, 5500000, false, false, &WalletFunding{userId: 1}, false},
		{"zero disables bypass", 0, 5500000, 5500000, false, false, &WalletFunding{userId: 1}, false},
		{"zero disables bypass for unlimited token", 0, 5500000, 0, true, false, &WalletFunding{userId: 1}, false},
		{"wallet equals threshold", 10, 5000000, 5500000, false, false, &WalletFunding{userId: 1}, false},
		{"token equals threshold", 10, 5500000, 5000000, false, false, &WalletFunding{userId: 1}, false},
		{"fractional threshold exceeded", 10.5, 5250001, 5250001, false, false, &WalletFunding{userId: 1}, true},
		{"fractional threshold equal", 10.5, 5250000, 5250001, false, false, &WalletFunding{userId: 1}, false},
		{"unlimited token", 10, 5500000, 0, true, false, &WalletFunding{userId: 1}, true},
		{"forced reservation", 10, 5500000, 5500000, false, true, &WalletFunding{userId: 1}, false},
		{"NaN threshold", math.NaN(), 5500000, 5500000, false, false, &WalletFunding{userId: 1}, false},
		{"Inf threshold", math.Inf(1), 5500000, 5500000, false, false, &WalletFunding{userId: 1}, false},
		{"negative threshold", -1, 5500000, 5500000, false, false, &WalletFunding{userId: 1}, false},
		{"organization wallet never bypasses", 10, 5500000, 5500000, false, false, &OrganizationWalletFunding{memberId: 1, organizationId: 1}, false},
		{"subscription never bypasses", 10, 5500000, 5500000, false, false, &SubscriptionFunding{userId: 1, amount: 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetQuotaSetting().TrustQuotaUSD = tc.trustUSD
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("token_quota", tc.token)
			session := &BillingSession{
				relayInfo: &relaycommon.RelayInfo{UserId: 1, UserQuota: tc.wallet, TokenUnlimited: tc.unlimited, ForcePreConsume: tc.force},
				funding:   tc.funding,
			}
			assert.Equal(t, tc.wantTrusted, session.shouldTrust(c))
		})
	}
}
