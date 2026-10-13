package service

import (
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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
		wallet      int64
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
