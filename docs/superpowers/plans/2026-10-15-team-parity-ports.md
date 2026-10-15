# team 재현 감사 이월 9건 포팅 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `docs/2026-10-15-team-parity-audit.md` §3에서 "옮김"으로 분류한 항목 중 relaykit에 걸리는 1번(UsageSemantic 덮어쓰기)을 뺀 9건을 `develop`에 포팅한다. 모두 team의 작은 수정이며 서로 독립이다.

**Architecture:** 백엔드 4건은 upstream 파일에 `// fork: O11` 훅 한 줄 + fork 파일(로직·테스트)로, 웹 5건은 upstream 컴포넌트에 최소 줄 변경(또는 fork 컴포넌트 분리)으로 넣는다. 모든 훅은 `docs/fork-overlays.md`의 O11 절에 "team 재현 감사 이월" 소제목 아래 파일:줄로 기록한다. relaykit과 `tokenkit`의 기존 파일은 건드리지 않는다(`tokenkit`에는 fork 파일만 추가).

**Tech Stack:** Go 1.26(`GOWORK=off`), Gin, miniredis/httptest, React 19 + TanStack Query + react-hook-form + zod, vitest, Bun.

**Spec:** `docs/2026-10-15-team-parity-audit.md` §3.1–3.4(옮김 행)와 §1 방법. team 출처 커밋: b5d733722, ecc9c511d, 203690366, 23d10ffb4, 2c619a31e(일반 부분만), cb220505e(429 부분만), f7ec7628c, 490d215a4, 338576ab2, 6fc579349.

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`(시작 HEAD `9a6b16907`). `/Users/molla/work/ai/new-api`(team 체크아웃)는 읽기 전용(`git show <sha>`, `git show team:<path>`).
- Go는 항상 `GOWORK=off`. JSON은 `common.Marshal/Unmarshal`만. 새 SQL 없음. relaykit(`relaykit/`) 수정 금지. `tokenkit/`의 기존 파일 수정 금지(새 fork 파일만).
- upstream Go 파일 수정은 한 줄 훅(`// fork: O11`)으로 제한하고 로직은 `*_fork.go`에 둔다. 웹 upstream 파일은 꼭 필요한 최소 줄만 바꾸고 각 줄에 `// fork: O11` 또는 `{/* fork: O11 */}` 표식을 단다. 모든 변경을 `docs/fork-overlays.md` O11 절에 기록한다.
- 웹 새 i18n 키는 `web/src/i18n/locales/en.json`(값=키)과 `ko.json`(번역, `docs/translation-glossary.ko.md` 용어)에만 넣는다. 키는 파일 끝에 텍스트로 추가하고 `bun run i18n:sync`는 돌리지 않는다. 넣은 뒤 `cd web && node scripts/ko-locale.mjs check`와 `bun run test -- ko-coverage`가 통과해야 한다.
- 웹 게이트: 손댄 파일마다 `bunx oxlint -c .oxlintrc.json <files>`·`bunx oxfmt --check <files>`에서 새 문제 없음, `bun run typecheck` 통과. `bun run format`은 쓰지 않는다. 새 TS/TSX 파일은 1행에 AGPL 헤더.
- 커밋 트레일러: `Ported-From: team <sha>`(여러 개면 줄 하나씩) + `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- 테스트는 SQLite in-memory TestMain과 httptest/miniredis로 한다. Docker 없음.

## Review Focus

1. **이미지 생성을 스트림으로 쓰는 사용자:** `"error": null`이 든 정상 partial 이벤트가 실패로 처리돼 요청이 끊기거나 환불되면 안 된다. → Task 1 `TestIsOpenAIImageStreamErrorEvent_NullErrorFieldIsNotAnError`.
2. **Epay를 설정하지 않은 운영자:** 충전 화면에 눌러도 동작하지 않는 Alipay/WeChat 버튼이 보이면 안 된다. Stripe/PayPal만 켠 경우 그 둘은 그대로 보여야 한다. → Task 2 `TestGetTopUpInfoHidesEpayMethodsWhenEpayNotConfigured`.
3. **환경 변수를 잘못 넣은 운영자(`GLOBAL_API_RATE_LIMIT=0`, 기간 0):** limiter가 모든 요청을 막거나 아무도 안 막는 대신 안전한 기본값으로 동작해야 하고, 로그에 값이 찍히면 안 된다. → Task 2 `TestNormalizeRateLimitParameters`.
4. **요청 한도에 걸린 대시보드 사용자:** 429를 받은 화면이 자동 재시도로 한도를 더 소진하면 안 된다. → Task 3 `query-client.test.ts` 429 케이스.
5. **충전 직후 대시보드로 돌아온 사용자:** 요약 카드 잔액이 충전 뒤 값이어야 한다. 새로고침 없이. → Task 4 `summary-cards.test.tsx`.

---

### Task 1: 이미지 스트림 `"error": null` 판정, 채널 테스트 Stream 전달, 토큰 코덱 예열

**Files:**
- Create: `relay/channel/openai/image_stream_error_fork.go`, `relay/channel/openai/image_stream_error_fork_test.go`, `controller/channel_test_image_stream_fork_test.go`, `tokenkit/warm_fork.go`, `tokenkit/warm_fork_test.go`
- Modify: `relay/channel/openai/relay_image.go:222`(판정식 한 줄), `controller/channel-test.go:716-721`(`ImageRequest` 리터럴에 필드 한 줄), `main.go:172` 다음 줄(한 줄)

**Interfaces:**
- Consumes: `isOpenAIImageStreamErrorEvent(data []byte) bool`(`relay_image.go:215`), `dto.ImageRequest.Stream *bool`(relaykit, 존재함), `tokenkit`의 비공개 `o200kBase`/`cl100kBase`(`tokenkit/tokenizer.go:14-15`, `sync.OnceValue`), `lo.ToPtr`, 채널 테스트의 `isStream` 변수(`controller/channel-test.go`에서 요청 빌더가 받는 값; 이름이 다르면 그 값을 쓴다).
- Produces: `rawJSONPresent(raw json.RawMessage) bool`(비어 있지 않고 `null`이 아님), `tokenkit.Warm()`.

- [ ] **Step 1: 실패하는 테스트**

`relay/channel/openai/image_stream_error_fork_test.go`(team b5d733722의 테스트를 develop 시그니처에 맞춰):

```go
package openai

import "testing"

func TestIsOpenAIImageStreamErrorEvent_NullErrorFieldIsNotAnError(t *testing.T) {
	data := []byte(`{"type":"image_generation.partial_image","error":null}`)
	if isOpenAIImageStreamErrorEvent(data) {
		t.Error(`"error": null must not be classified as an error event`)
	}
}

func TestIsOpenAIImageStreamErrorEvent_NonNullErrorFieldIsAnError(t *testing.T) {
	data := []byte(`{"type":"image_generation.partial_image","error":{"message":"boom"}}`)
	if !isOpenAIImageStreamErrorEvent(data) {
		t.Error("a populated error object must be classified as an error event")
	}
}

func TestExtractOpenAIImageStreamErrorMessage_NullErrorFallsBackToDefault(t *testing.T) {
	got := extractOpenAIImageStreamErrorMessage([]byte(`{"error":null}`))
	if got != "upstream image stream returned error event" {
		t.Errorf("got %q", got)
	}
}
```

`controller/channel_test_image_stream_fork_test.go`: 채널 테스트 요청 빌더(이미지 생성 엔드포인트 분기)를 `isStream=true`로 호출해 `*dto.ImageRequest.Stream`이 non-nil true인지, `false`면 non-nil false인지 확인한다. 빌더가 비공개면 같은 패키지 테스트로 직접 부른다(함수 이름은 `channel-test.go:700-725`를 읽어 확인).

`tokenkit/warm_fork_test.go`:

```go
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
```

실행: `GOWORK=off go test ./relay/channel/openai/ -run ImageStreamError -count=1` → null 케이스 FAIL. `GOWORK=off go test ./controller/ -run ImageStream -count=1` → FAIL. `cd tokenkit && GOWORK=off go test ./ -run Warm` → 컴파일 실패.

- [ ] **Step 2: 구현**

`relay/channel/openai/image_stream_error_fork.go`:

```go
package openai

import "encoding/json"

// fork: O11 — an explicit "error": null on an image stream event is not an
// error (team b5d733722). len(raw) alone treats the 4-byte literal as one.
func rawJSONPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}
```

`relay_image.go:222`의 `|| len(payload.Error) > 0`을 `|| rawJSONPresent(payload.Error) // fork: O11`로. `:244`(`if len(payload.Error) > 0 {`)도 같은 함수로 바꾼다(두 줄, 둘 다 표식).

`controller/channel-test.go:716-721`의 `ImageRequest` 리터럴에 `Stream: lo.ToPtr(isStream), // fork: O11` 한 줄(team ecc9c511d). `isStream`이 그 스코프에 없으면 빌더의 stream 인자를 찾아 쓰고 보고서에 적는다.

`tokenkit/warm_fork.go`:

```go
package tokenkit

// fork: O11 — Warm builds the o200k and cl100k codecs now instead of on the
// first GPT/o-series Count call (hundreds of ms). Safe to call repeatedly and
// concurrently; after the first call it returns immediately (team 338576ab2).
func Warm() {
	o200kBase()
	cl100kBase()
}
```

`main.go:172` 다음 줄에 `go tokenkit.Warm() // fork: O11`(import 추가가 필요하면 import 블록 한 줄도 표식).

실행: Step 1 PASS, `GOWORK=off go build ./... && GOWORK=off go vet ./relay/channel/openai/ ./controller/ && GOWORK=off go test ./relay/channel/openai/ ./controller/ -count=1 && (cd tokenkit && GOWORK=off go test ./ -count=1)`.

- [ ] **Step 3: 커밋**

```bash
git add relay/channel/openai controller/channel-test.go controller/channel_test_image_stream_fork_test.go tokenkit/warm_fork.go tokenkit/warm_fork_test.go main.go docs/fork-overlays.md
git commit -m "fix(relay): accept null error fields on image stream events; thread stream into image channel tests; warm token codecs at startup

Ported-From: team b5d733722
Ported-From: team ecc9c511d
Ported-From: team 338576ab2
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: Epay 미설정 시 결제 수단 숨김, rate-limit 설정값 보정

**Files:**
- Create: `controller/topup_pay_methods_fork.go`, `controller/topup_pay_methods_fork_test.go`, `middleware/rate_limit_normalize_fork.go`, `middleware/rate_limit_normalize_fork_test.go`
- Modify: `controller/topup.go:30-33`(4줄 블록 → 한 줄), `middleware/rate-limit.go:148`(`rateLimitFactory` 첫 줄에 한 줄)

**Interfaces:**
- Consumes: `operation_setting.PayMethods`, `operation_setting.IsPaymentComplianceConfirmed()`, `isEpayTopUpEnabled()`(`controller/payment_webhook_availability.go:95`), `common.RateLimitKeyExpirationDuration`(`common/constants.go:248`, 20분), `rateLimitFactory(maxRequestNum int, duration int64, mark string)`.
- Produces: `visibleEpayPayMethods(complianceConfirmed bool) []map[string]string`(compliance 확인 **그리고** Epay 설정 완료일 때만 `operation_setting.PayMethods`, 아니면 빈 슬라이스; team 203690366), `normalizeRateLimitParameters(maxRequestNum int, duration int64) (int, int64)`(team 2c619a31e의 일반 부분).

- [ ] **Step 1: 실패하는 테스트**

`controller/topup_pay_methods_fork_test.go`: `operation_setting.PayMethods`를 테스트 값(`[{"type":"alipay"}]`)으로 두고 (a) compliance true + Epay 미설정(`setting.PayAddress`·`EpayId`·`EpayKey`가 비어 `isEpayTopUpEnabled()`가 false가 되게; 어떤 설정이 그 함수를 참으로 만드는지 `payment_webhook_availability.go:95`를 읽어 맞춘다) → 빈 슬라이스, (b) compliance true + Epay 설정 → PayMethods 그대로, (c) compliance false → 빈 슬라이스. 전역 설정은 `t.Cleanup`으로 복구. 추가로 `GetTopUpInfo`를 httptest로 호출해 (a)에서 응답 JSON의 `pay_methods`에 alipay가 없고 Stripe를 켰을 때 `stripe`는 있는지 확인한다(응답 필드 이름은 `topup.go:34-80`에서 확인).

`middleware/rate_limit_normalize_fork_test.go`:

```go
package middleware

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRateLimitParameters(t *testing.T) {
	maxDur := int64(common.RateLimitKeyExpirationDuration / time.Second)
	cases := []struct {
		name     string
		num      int
		dur      int64
		wantNum  int
		wantDur  int64
	}{
		{"valid passes through", 100, 60, 100, 60},
		{"zero requests becomes 1", 0, 60, 1, 60},
		{"negative duration becomes 1", 100, -5, 100, 1},
		{"duration above key expiry is clamped", 100, maxDur + 1, 100, maxDur},
		{"both invalid", -1, 0, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			num, dur := normalizeRateLimitParameters(tc.num, tc.dur)
			require.Equal(t, tc.wantNum, num)
			require.Equal(t, tc.wantDur, dur)
		})
	}
}

func TestRateLimitFactoryUsesNormalizedValues(t *testing.T) {
	// memory limiter path (RedisEnabled false): 0 requests must not block everyone
	old := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = old })
	handler := rateLimitFactory(0, 60, "T0")
	// first request must pass (normalized to 1 request / 60s)
	w, c := newTestContext(t) // helper: httptest.NewRecorder + gin.CreateTestContext with a GET request
	handler(c)
	require.NotEqual(t, 429, w.Code)
}
```

`newTestContext`가 없으면 테스트 파일 안에 정의한다(기존 `middleware/rate_limit_test.go`에 비슷한 helper가 있으면 그것을 쓴다).

실행: `GOWORK=off go test ./controller/ -run PayMethods -count=1`, `GOWORK=off go test ./middleware/ -run 'Normalize|RateLimitFactory' -count=1` → FAIL/컴파일 실패.

- [ ] **Step 2: 구현**

`controller/topup_pay_methods_fork.go`:

```go
package controller

import "github.com/QuantumNous/new-api/setting/operation_setting"

// fork: O11 — operation_setting.PayMethods carries hard-coded defaults
// (Alipay/WeChat). Expose them only when the Epay gateway is actually
// configured, so the top-up page never shows methods that cannot pay
// (team 203690366).
func visibleEpayPayMethods(complianceConfirmed bool) []map[string]string {
	if complianceConfirmed && isEpayTopUpEnabled() {
		return operation_setting.PayMethods
	}
	return []map[string]string{}
}
```

`controller/topup.go:30-33`의 네 줄(`payMethods := operation_setting.PayMethods` … `}`)을 `payMethods := visibleEpayPayMethods(complianceConfirmed) // fork: O11` 한 줄로.

`middleware/rate_limit_normalize_fork.go`:

```go
package middleware

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var invalidRateLimitConfigWarningOnce sync.Once

// fork: O11 — clamp malformed rate-limit configuration to safe bounds
// (team 2c619a31e). The warning is fixed and value-free: constructors run for
// many routes and must not flood logs or echo deployment configuration.
func normalizeRateLimitParameters(maxRequestNum int, duration int64) (int, int64) {
	maxDuration := int64(common.RateLimitKeyExpirationDuration / time.Second)
	if maxDuration <= 0 {
		maxDuration = 1
	}
	if maxRequestNum > 0 && duration > 0 && duration <= maxDuration {
		return maxRequestNum, duration
	}
	invalidRateLimitConfigWarningOnce.Do(func() {
		common.SysError(common.LogText("invalid rate limit configuration normalized to safe bounds"))
	})
	if maxRequestNum <= 0 {
		maxRequestNum = 1
	}
	if duration <= 0 {
		duration = 1
	} else if duration > maxDuration {
		duration = maxDuration
	}
	return maxRequestNum, duration
}
```

`middleware/rate-limit.go:148` `rateLimitFactory` 본문 첫 줄에 `maxRequestNum, duration = normalizeRateLimitParameters(maxRequestNum, duration) // fork: O11`.

실행: Step 1 PASS, `GOWORK=off go vet ./controller/ ./middleware/ && GOWORK=off go test ./controller/ ./middleware/ -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add controller/topup.go controller/topup_pay_methods_fork.go controller/topup_pay_methods_fork_test.go middleware/rate-limit.go middleware/rate_limit_normalize_fork.go middleware/rate_limit_normalize_fork_test.go docs/fork-overlays.md
git commit -m "fix(topup,ratelimit): hide Epay methods until the gateway is configured; clamp invalid rate-limit settings

Ported-From: team 203690366
Ported-From: team 2c619a31e
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: 웹 소형 수정 — 429 재시도 금지, 가격·상태 8초 타임아웃, 관리자 쿼터 대화상자 빠른 금액

**Files:**
- Modify: `web/src/lib/query-client.ts:44`, `web/src/lib/api.ts:74`, `web/src/features/pricing/api.ts:29`, `web/src/features/users/components/user-quota-dialog.tsx`(Input 위에 블록), `web/src/i18n/locales/en.json`, `ko.json`
- Test: `web/src/lib/__tests__/query-client.test.ts`(없으면 생성), `web/src/features/users/components/__tests__/user-quota-dialog.test.tsx`(없으면 생성)

**Interfaces:**
- Consumes: `getServerErrorStatus(error)`(query-client.ts), `api.get(url, { timeout })`(axios), `QUICK_QUOTA_AMOUNTS = [1, 5, 10, 100]`와 렌더 패턴(`web/src/features/organizations/components/organization-users-table.tsx:136`), `parseQuotaFromDollars`, `formatQuota`, `tokensOnly`·`setAmount`(`user-quota-dialog.tsx:46-51`).
- Produces: i18n 키 `Quick amount`(ko `빠른 금액`; 조직 표에서 이미 쓰면 재사용).

- [ ] **Step 1: 실패하는 테스트**

`query-client.test.ts`: `createQueryClient()`(이름은 `query-client.ts:38` 확인)의 `defaultOptions.queries.retry`를 `(1, axiosError(429))`로 부르면 `false`, `(1, axiosError(500))`이면 `true`, `(1, axiosError(401))`이면 `false`. `import.meta.env.DEV`는 `vi.stubEnv('DEV', false)`로 끈다.

`user-quota-dialog.test.tsx`: 달러 통화 모드로 대화상자를 렌더하고 "Quick amount" 라벨과 네 개 버튼이 보이며, `$10` 버튼을 누르면 금액 입력값이 `10`이 된다. 토큰 통화(`currencyMeta.kind === 'tokens'`)면 블록이 없다. 렌더 helper는 `web/src/features/organizations/components/__tests__/` 안의 기존 테스트를 참고한다.

실행: `cd web && bun run test -- query-client user-quota-dialog` → FAIL.

- [ ] **Step 2: 구현**

`query-client.ts:44`: `return ![401, 403].includes(...)` → `return ![401, 403, 429].includes(getServerErrorStatus(error) ?? 0) // fork: O11 (team cb220505e)`.

`api.ts:74`: `api.get('/api/status')` → `api.get('/api/status', { timeout: 8000 }) // fork: O11 (team f7ec7628c)`; `pricing/api.ts:29` 같은 방식. 주석으로 "cold backend/Redis 첫 로드가 멈추면 거부해 React Query가 재시도하게" 한 줄.

`user-quota-dialog.tsx`: 파일 상단에 `const QUICK_QUOTA_AMOUNTS = [1, 5, 10, 100] // fork: O11`, 금액 `<Input>`(:165) 위에 team 6fc579349의 블록(`{!tokensOnly && (<div className='space-y-2'><Label>{t('Quick amount')}</Label><div className='flex flex-wrap gap-1'>{QUICK_QUOTA_AMOUNTS.map(... <Button type='button' variant='outline' size='sm' onClick={() => setAmount(String(quickAmount))}>{formatQuota(parseQuotaFromDollars(quickAmount))}</Button>)}</div></div>)}`)을 `{/* fork: O11 */}` 표식과 함께 넣는다. 이 파일이 upstream 파일이므로 블록이 10줄을 넘으면 `user-quota-quick-amounts-fork.tsx` 컴포넌트로 빼고 한 줄로 끼운다. `Button`·`Label` import가 없으면 추가(표식).

i18n: `Quick amount` 키가 en/ko에 없으면 끝에 추가.

실행: Step 1 PASS, `cd web && bun run typecheck && bun run test -- query-client user-quota-dialog ko-coverage && node scripts/ko-locale.mjs check`, oxlint/oxfmt 손댄 파일.

- [ ] **Step 3: 커밋**

```bash
git add web/src docs/fork-overlays.md
git commit -m "fix(web): stop retrying 429s, time out pricing/status loads, add quick amounts to the admin quota dialog

Ported-From: team cb220505e
Ported-From: team f7ec7628c
Ported-From: team 6fc579349
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: 대시보드 요약 카드 잔액 실시간 갱신

**Files:**
- Modify: `web/src/features/dashboard/components/overview/summary-cards.tsx:142-148`, `web/src/features/wallet/index.tsx:118`
- Test: `web/src/features/dashboard/components/overview/__tests__/summary-cards.test.tsx`(없으면 생성)

**Interfaces:**
- Consumes: `getSelf()`(`web/src/lib/api.ts:44`, `{success, data}`), `useAuthStore((s) => s.auth.user)` / `s.auth.setUser`(`stores/auth-store.ts:97`), `useQuery`.
- Produces: 요약 카드가 `['user','self']` 쿼리(staleTime 0)를 읽고 `selfQuery.data ?? user`를 쓴다. 지갑 화면의 `fetchUser`가 받은 데이터를 `setAuthUser`에도 넣는다(team 490d215a4).

- [ ] **Step 1: 실패하는 테스트**

`summary-cards.test.tsx`: `getSelf`를 `vi.mock('@/lib/api')`로 `{success:true, data:{quota: 5_000_000, used_quota: 1, request_count: 2}}`로 모킹하고, auth store에는 `quota: 1_000_000`을 넣은 뒤 렌더 → 잔액 표시가 모킹 값(`$10.00`, `formatQuota` 기준)으로 바뀌는지 `findByText`로 확인. `getSelf`가 실패하면 store 값이 보이는지도 확인. 렌더 helper(QueryClientProvider, i18n)는 `overview-dashboard` 기존 테스트가 있으면 재사용.

실행: `cd web && bun run test -- summary-cards` → FAIL(store 값 표시).

- [ ] **Step 2: 구현**

`summary-cards.tsx:142` 다음에(표식 `// fork: O11 (team 490d215a4)`):

```ts
  const selfQuery = useQuery({
    queryKey: ['user', 'self'],
    queryFn: async () => {
      const result = await getSelf()
      return result.success && result.data ? result.data : null
    },
    staleTime: 0,
  })
  const liveUser = selfQuery.data ?? user
```

이어 `:146-148`의 `user?.` 세 줄을 `liveUser?.`로. `getSelf` import 추가. 블록이 길면 `use-live-self-fork.ts` 훅으로 빼고 한 줄로 끼운다.

`wallet/index.tsx:118` `setUser(response.data as UserWalletData)` 바로 뒤에 `setAuthUser(response.data as AuthUser) // fork: O11`; `const setAuthUser = useAuthStore((s) => s.auth.setUser)`와 `AuthUser` import 추가, `useCallback` 의존성에 `setAuthUser`.

실행: Step 1 PASS, `cd web && bun run typecheck && bun run test -- summary-cards wallet`, oxlint/oxfmt.

- [ ] **Step 3: 커밋**

```bash
git add web/src docs/fork-overlays.md
git commit -m "fix(web): refresh the overview balance from the server instead of the login snapshot

Ported-From: team 490d215a4
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: 결제 설정의 Stripe/PayPal 자격 증명 지우기

**Files:**
- Create: `web/src/features/system-settings/integrations/payment-credentials-clear-fork.tsx`, `web/src/features/system-settings/integrations/__tests__/payment-credentials-clear-fork.test.tsx`
- Modify: `web/src/features/system-settings/integrations/payment-settings-section.tsx`(Stripe 절 `:1316` 아래와 PayPal 절 각각 한 줄), `en.json`, `ko.json`

**Interfaces:**
- Consumes: `useUpdateOption()`(`:234`, `mutateAsync({key, value})`), `form`(react-hook-form, `form.setValue`), `initialRef`(`:243`), `AlertDialog` 컴포넌트(`@/components/ui/alert-dialog`), toast.
- Produces: `<ClearPaymentCredentialsButton provider='stripe'|'paypal' form={form} initialRef={initialRef} />`(team 23d10ffb4). Stripe 키: `StripeApiSecret`, `StripeWebhookSecret`, `StripePriceId`; PayPal 키: `PayPalClientId`, `PayPalClientSecret`, `PayPalWebhookID`. 누르면 확인 대화상자 → 세 옵션을 빈 값으로 저장(`mutateAsync` 순차) → 폼 값과 `initialRef.current`의 해당 키를 ''로 → 성공 toast. i18n 키: `Clear`, `Clear all Stripe credentials and disable Stripe payments?`, `Clear all PayPal credentials and disable PayPal payments?`, `Credentials cleared`.

- [ ] **Step 1: 실패하는 테스트**

`payment-credentials-clear-fork.test.tsx`: `useUpdateOption`을 모킹해 `mutateAsync` 호출을 기록. Stripe 버튼 렌더 → "Clear" 클릭 → 확인 문구 표시 → 확인 클릭 → `mutateAsync`가 세 키로 빈 값 호출되고 `form.setValue`가 호출됐는지. 취소하면 호출 없음.

실행: `cd web && bun run test -- payment-credentials-clear` → FAIL(모듈 없음).

- [ ] **Step 2: 구현**

fork 컴포넌트를 만들고, `payment-settings-section.tsx`의 Stripe 절 제목(`:1316-1318`) 바로 아래와 PayPal 절 제목 아래에 각각 `<ClearPaymentCredentialsButton provider='stripe' form={form} initialRef={initialRef} /> {/* fork: O11 */}` 한 줄과 import 한 줄. `form`·`initialRef` 타입은 섹션 파일의 스키마 타입(`z.infer<typeof schema>`)을 export하거나 제네릭으로 받는다(upstream 파일에 export 한 줄 추가가 필요하면 표식).

i18n 네 키를 en/ko에 추가(ko: `지우기`, `Stripe 자격 증명을 모두 지우고 Stripe 결제를 끌까요?`, `PayPal 자격 증명을 모두 지우고 PayPal 결제를 끌까요?`, `자격 증명을 지웠습니다`).

실행: Step 1 PASS, `cd web && bun run typecheck && bun run test -- payment-credentials-clear payment-settings ko-coverage && node scripts/ko-locale.mjs check`, oxlint/oxfmt.

- [ ] **Step 3: 커밋**

```bash
git add web/src docs/fork-overlays.md
git commit -m "feat(web): add clear buttons for Stripe and PayPal credentials in payment settings

Ported-From: team 23d10ffb4
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: 기록·overlay 표·전체 검증·병합 리허설

**Files:**
- Modify: `docs/2026-10-15-team-parity-audit.md`(§3 "옮김" 행에 develop 커밋 적기, 상태를 "포팅 완료(1번 제외)"로), `docs/fork-overlays.md`(O11 절 "team 재현 감사 이월" 소제목 아래 모든 훅 파일:줄 확인)

- [ ] **Step 1: 전체 검증** — `GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | grep -E "^(FAIL|---)"`(없어야 함), `cd tokenkit && GOWORK=off go test ./`; `cd web && bun run typecheck && bun run test && bun run build && node scripts/ko-locale.mjs check`. 숫자를 기록.
- [ ] **Step 2: overlay 확인** — `grep -rn "fork: O11" --include='*.go' --include='*.ts' --include='*.tsx' . | grep -v node_modules`로 이번 계획의 훅이 모두 표에 있고 줄 번호가 맞는지.
- [ ] **Step 3: 병합 리허설** — `git fetch qn && git merge --no-commit --no-ff qn/main`; 충돌 목록을 기록하고 overlay 표의 파일인지 확인한 뒤 `git merge --abort`.
- [ ] **Step 4: 감사 문서 갱신** — §3.1–3.4의 옮긴 행에 develop 커밋 해시를, 1번 행에는 "relaykit 제약으로 보류(별도 설계)"를 적는다. 문서 상태 줄 갱신.
- [ ] **Step 5: 커밋** — `docs: record the nine team parity ports` (트레일러 `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`).

---

## 계획 결정 사항

1. UsageSemantic 덮어쓰기(감사 §3.1 첫 행)는 relaykit `dto.ChannelSettings`에 걸려 이 계획에서 뺀다. 원문 JSON을 fork가 따로 읽는 우회 설계가 필요하면 별도 계획으로 한다.
2. 모든 항목의 overlay 번호는 O11(fork 고유 수정)이다. 새 번호를 만들지 않는다.
3. 웹 upstream 파일 수정은 한 줄이 넘는 블록이면 fork 컴포넌트/훅으로 빼서 한 줄 삽입으로 만든다(Task 3 빠른 금액, Task 4 selfQuery, Task 5 버튼).
4. rate-limit 보정의 경고 로그는 team처럼 값 없이 한 번만 남긴다(설정값 노출 방지).
5. `tokenkit`은 별도 Go 모듈이지만 relaykit이 아니므로 fork 파일 추가는 허용한다. 기존 파일은 손대지 않는다.
6. 2c619a31e·cb220505e·3757c8244 등 묶음 커밋은 감사에서 "옮김"으로 분류한 부분만 옮긴다(Toss, 디바운스 등은 제외).
