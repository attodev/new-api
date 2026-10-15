# team 재현 감사 — develop이 team의 자체 커밋을 모두 담고 있는가

작성: 2026-10-15. 대상: `team` 팁 `9ef93eb8c` vs `develop` `1bc33c01b`. 상태: 옮김 9건 포팅 완료(2026-10-15, 커밋 5ff2f1b5c..5c6f345f9); 1번은 보류; 결정 항목 대기

## 1. 범위와 방법

- team에서 upstream(`qn/main`)에 없는 커밋 707개 중 upstream을 그대로 가져온 forward-port 40개(`Upstream:` 트레일러)를 뺀 **자체 커밋 667개**(2026-05-05 ~ 2026-10-14)를 모두 봤다.
- 커밋을 시간순 5묶음으로 나눠 각각 team 커밋의 동작을 읽고 develop에서 같은 동작을 찾았다(식별자·문자열·라우트·옵션 키로 검색, overlay 표와 단계 기록 대조). 한 커밋에 한 행씩 `docs/reviews/2026-10-15-team-parity-audit.tsv`에 근거를 남겼다.
- 분류: `covered`(develop에 같은 동작이 있음. upstream이 직접 제공하는 경우 포함), `dropped`(기록된 결정에 따라 일부러 뺌), `superseded`(team의 나중 커밋이 대체하거나 되돌림), `n/a`(문서·스크래치·테스트만), `gap`(develop에 없고 결정도 없음), `unsure`(판단 보류).
- 결제 관련 빈틈과 과금 관련 빈틈은 묶음 결과를 받은 뒤 코드로 다시 확인했다.

## 2. 결과

| 분류 | 커밋 수 |
|---|---|
| covered | 388 |
| dropped | 139 |
| n/a | 100 |
| superseded | 7 |
| gap | 30 |
| unsure | 3 |
| 합계 | 667 |

`dropped` 139개의 대부분은 Toss 결제 전체(일반 충전, 빌링키·정기결제, 자동충전과 cron, 조직 지갑 Toss 라우트, classic 패리티)다. 재기반 설계 §2.2가 Toss 제외를 명시했고 §9 결정 2가 테이블만 남기기로 했다. 그 밖의 `dropped`는 §9 결정(언어 en/ko만, Claude 네이티브 passthrough는 upstream 유지, Epay 응답 upstream 유지, Redis 캐시는 O12 상주 방식, 조직 로그 가입 시점 제한 없음, 잔여 중국어 문자열 등)과 단계 기록의 결정(공식 보기 보류, 모델 카드 레이아웃은 upstream 유지, 조직 이름 변경은 develop이 상위집합)으로 설명된다.

## 3. 빈틈 (30) + 보류 (3)

권장 열의 뜻: **옮김** = develop에 포팅 권장, **결정** = 사용자가 정할 것, **보류** = 비용 대비 효과가 낮아 그대로 둬도 됨.

### 3.1 과금·정산 (옮김 권장)

| team 커밋 | 내용 | develop 상태 | 권장 |
|---|---|---|---|
| e24c060c0, 93b81b0bd | 채널 설정 `UsageSemantic` 덮어쓰기(OpenAI 호환 채널로 Claude를 중계할 때 캐시 토큰 해석)와 이를 모델 glob으로 제한하는 `UsageSemanticModels`, 채널 편집 UI | `service/text_quota.go:377 usageSemanticFromUsage`에 덮어쓰기 없음(기본값을 0으로 자르기만). 해당 채널에서 **과금이 적게** 될 수 있다 | 보류 — relaykit dto.ChannelSettings 제약; 별도 설계(원문 JSON을 fork가 읽는 방식) 필요 |
| b5d733722 | 이미지 스트림 이벤트의 `"error": null`을 오류로 보지 않기 | `relay/channel/openai/relay_image.go:227,244`가 `len(payload.Error) > 0`으로 판정해 `null`(길이 4)을 오류로 분류 | 옮김 → develop 5ff2f1b5c |
| ecc9c511d | 관리자 채널 테스트에서 이미지 생성 요청에 `Stream` 전달 | `controller/channel-test.go:716` `ImageRequest`에 `Stream` 없음 → stream=true 테스트가 SSE 경로를 못 탐 | 옮김 → develop 5ff2f1b5c |
| 3cfc88575, 5e6c6a716 | 토큰·지갑 캐시 델타를 동기로 적용하고 DB 실패 시 보상 | develop은 upstream의 비동기 고루틴(`model/token.go:384`, `model/user.go:1398`)을 유지. 지갑 쪽은 O12 복구 목록이 캐시 쓰기 실패를 덮지만 순서·보상은 아님 | 결정 — O12가 지갑은 실질적으로 막았고, 토큰은 §6 한계로 기록됨. 토큰 해시만 동기 델타로 바꿀지 결정 |
| f1dd2da1e(토큰 부분) | 토큰 배치 DB 쓰기 실패 시 재큐 | `model/utils.go:92-101`은 로그만 남기고 버림. 지갑은 O12 Task 6이 재큐함 | 결정 — 토큰 한도 차감 손실이 생길 수 있음. 지갑과 같은 방식의 작은 fork 훅으로 가능 |

### 3.2 결제 설정 (옮김 권장)

| team 커밋 | 내용 | develop 상태 | 권장 |
|---|---|---|---|
| 203690366 | Epay가 설정되지 않으면 기본 Alipay/WeChat 결제 수단을 숨김 | `controller/topup.go:30 GetTopUpInfo`가 compliance만 보고 `PayMethods`를 그대로 노출 | 옮김 → develop d1ee2f430 |
| 3f6ab2e33(일부) | `ComplianceConfirmed` 기본값 true | develop 기본값 false → 새 설치는 관리자가 확인하기 전까지 결제 수단이 안 보임 | 결정 — 관리자 화면에서 한 번 확인하면 되므로 운영 절차로 대신할 수 있음 |
| 23d10ffb4 | 결제 설정의 Stripe/PayPal 자격 증명 "지우기" 버튼(확인 대화상자) | `payment-settings-section.tsx`에 없음 | 옮김 → develop 5c6f345f9 |

### 3.3 동작·안정성 (작은 수정)

| team 커밋 | 내용 | develop 상태 | 권장 |
|---|---|---|---|
| 2c619a31e(일반 부분) | `normalizeRateLimitParameters`: 잘못된 rate-limit 설정값을 안전 범위로 보정 | 없음(`middleware/rate-limit.go`) | 옮김 → develop d1ee2f430, 779b1000d |
| cb220505e(일부) | 프런트 쿼리가 429에 재시도하지 않음 | `web/src/lib/query-client.ts:44`가 401/403만 제외 | 옮김 → develop 0367daf86 |
| f7ec7628c | 가격·상태 API 호출 8초 타임아웃 | 없음 | 옮김 → develop 0367daf86 |
| 490d215a4 | 대시보드 요약 카드 잔액을 새로 가져와 갱신(staleTime 0, auth store 동기화) | `summary-cards.tsx`가 auth store의 로그인 시 값만 읽음 → 잔액이 안 바뀜 | 옮김 → develop 9a13e5913 |
| 338576ab2 | 시작 시 `tokenkit.Warm()`으로 OpenAI 코덱 예열 | 없음. 첫 토큰 계산이 수백 ms 느림 | 옮김(성능) → develop 5ff2f1b5c |
| 43c0105ff | Midjourney 환불 로그에 제출 노드 `NodeName` 기록 | `model/midjourney.go`에 NodeName 없음. develop은 NodeName을 소비 로그 내보내기에만 씀 | 보류 — upstream 설계상 큰 의미 없음 |

### 3.4 화면·운영 편의 (결정)

| team 커밋 | 내용 | develop 상태 | 권장 |
|---|---|---|---|
| a05006778, 4e787397f | 로고·Home 링크가 서버 렌더 랜딩으로 전체 새로고침(`forceReload`), 로그인 시 `/dashboard/overview` | SPA Home 페이지로 클라이언트 이동 | 결정 — upstream Home 페이지를 쓸지 랜딩으로 보낼지 |
| 9b4ee5213, a3e606bc9 | 대시보드 "빠른 시작 가이드(PDF)" 다운로드(ko/en PDF) | 없음 | 결정 |
| 3757c8244(일부) | SPA `index.html`의 챗봇 위젯 스크립트(`chatbot.alrouter.ai`) | 랜딩에만 있고 SPA에는 없음 | 결정 |
| d5e459993, 330d46466 | SPA 가격 페이지의 프로모션 안내문(`Alert` variant `notice`) | 랜딩 팝업에만 있음 | 결정 |
| 7daa397a1(일부) | 가입 화면 "계약 도메인만 가입 가능" 안내(ShieldCheck), 사용 로그 열 이름 "토큰 (입력/출력)" | 없음 | 결정 |
| 6fc579349 | 관리자 쿼터 조정 대화상자의 빠른 금액 버튼($1/5/10/100) | 조직 멤버 표에는 있고 `user-quota-dialog.tsx`에는 없음 | 옮김(작음) → develop 0367daf86 |
| 7fe52317f(일부) | 설정 가이드 완료 시 접기(오래된 펼침 상태 무시), 오류 "(Error Code: N)" fallback | 없음 | 보류 |
| 7c0e34323(일부) | 404 페이지 "탭 닫기" fallback(history 1일 때), 브랜드 링크 `/dashboard/overview` | 없음 / `/` | 보류 |

### 3.5 스타일 (보류)

| team 커밋 | 내용 |
|---|---|
| f34c341c2 | 결제 수단 템플릿 기본 이름 영어(Alipay / WeChat Pay / Custom 1) — develop은 upstream 중국어 |
| 428f19795 | 빠른 시작 단계 연결선 간격 |
| c16e8c6d7 | 스위치 thumb 그림자, 사이드바 메뉴 간격 |

### 3.6 보류(unsure) 3건

| team 커밋 | 내용 | 확인할 것 |
|---|---|---|
| 350662f4c | GA4 ID `G-Z73Y7213LF`를 코드에 고정 | develop은 `GOOGLE_ANALYTICS_ID` 환경 변수를 읽는다. 운영 환경에 설정돼 있는지 확인 |
| a2f680700 | 상단 Home 링크 전체 새로고침(프로필 배지는 §4.2로 뺌) | 3.4의 랜딩 이동 결정과 함께 |
| c1325db97 | 프로필 화면 배치, 사이드바 모듈 카드 이름, 메뉴 순서 | upstream 새 UI(`/security` 페이지 등)를 따를지 결정 |

## 4. 함께 확인할 것 (dropped이지만 증상이 남는 것)

- **Claude adaptive thinking 표시(9088a5703):** §9 결정 "네이티브 passthrough는 upstream 유지"로 뺐다. develop은 `thinking.display`를 클라이언트가 `IncludeThoughts`를 보낼 때만 설정한다(`relaykit/relayconvert/reasoning/claude.go:139,191,263`). 그래서 `thinking:{"type":"adaptive"}`만 보낸 네이티브 요청은 Opus 4.7+에서 thinking 본문이 비어 돌아온다. team 커밋이 고친 증상이 그대로다. 결정을 유지할지 확인이 필요하다.
- **이메일 인증 오류(30a3fe6a9):** `controller/misc.go:243 SendEmailVerification`이 SMTP 원문 오류를 사용자에게 그대로 돌려준다. team은 고정 메시지로 바꿨다. §9 결정 12(잔여 문자열)로 뺐지만 원문 노출은 별개 문제다.
- **공식 보기(eb8baa760 외 2):** 과금 상세의 "공식 보기" 접이식 화면은 단계 C 기록대로 뺐다(upstream 상세 대화상자에 공식이 없음). 할인은 분해 행으로 보인다.

## 5. 제안

1. 3.1·3.2·3.3의 "옮김" 항목(10건 안팎)은 모두 작고 서로 독립이라 계획 하나로 묶어 포팅한다. 과금 영향이 있는 `UsageSemantic` 덮어쓰기와 이미지 스트림 오류 판정을 먼저 한다.
2. "결정" 항목은 이 문서에서 골라 주면 같은 계획에 넣는다.
3. 토큰 캐시 동기 델타·재큐(3.1 아래 두 행)는 O12의 지갑 쪽과 같은 방식으로 할 수 있으나, API 키 캐시를 upstream 그대로 둔다는 O12 결정 4와 충돌하므로 따로 결정한다.
4. 4절의 adaptive thinking은 운영에서 실제로 쓰는 요청 형태인지 확인한 뒤 정한다.

감사 원본: `docs/reviews/2026-10-15-team-parity-audit.tsv` (667행, 묶음 순서, 근거 포함).
