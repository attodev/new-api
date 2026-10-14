# 한국어 번역 용어집 (Korean Glossary)

이 문서는 `web/src/i18n/locales/ko.json`을 번역·검수할 때 따르는 기준입니다. 용어는 legacy `team` 브랜치의 `kr.json`에서 실제로 쓰인 표현을 먼저 조사했고, 한 용어가 둘 이상으로 번역되어 있으면 더 많이 쓰인 쪽을 골랐습니다(비고에 다른 표현과 사용 횟수를 적었습니다). team에 없던 용어는 이웃 용어와 맞춰 새로 정했습니다.

## 문체 규칙

- 문장(메시지, 설명, 토스트, 안내문)은 격식체로 씁니다: "~합니다", "~하세요", "~되었습니다", "~하지 못했습니다". 반말과 "~해요"체는 쓰지 않습니다.
- 버튼, 메뉴, 표 머리글, 폼 라벨은 명사형으로 씁니다: "저장", "삭제", "API 키 생성", "그룹 추가". "저장하기", "저장합니다" 같은 서술형으로 바꾸지 않습니다.
- 보간 `{{name}}`, Trans 태그 `<0>…</0>`, 줄바꿈 `\n`, 앞뒤 공백은 영어 원문과 정확히 같게 보존합니다. 보간 변수 이름은 번역하지 않습니다(`{{count}}`를 `{{개수}}`로 바꾸면 안 됩니다). 이 규칙은 `bun run i18n:ko -- check`가 검사합니다.
- 제품명·브랜드·프로토콜·코드 식별자는 영문 그대로 둡니다: OpenAI, Anthropic, Claude, Gemini, Stripe, Passkey 같은 고유명사, JSON, URL, API, HTTP, SSE, `gpt-4` 같은 모델 ID. 한글 문장 안에 들어가는 영문은 앞뒤를 띄어 씁니다("API 키", "Stripe 대시보드").
- 영어를 그대로 두어야 하는 키(숫자, JSON·URL 예시, 브랜드 리터럴)는 `docs/i18n/ko-skip.json`에 있습니다. 이 키는 번역하지 않고 영어 값을 유지합니다. 목록에 없는 키가 영어 그대로면 `check`가 미번역으로 보고합니다. 사람이 고른 영어 유지 리터럴(OAuth, vLLM, Top P 등)은 `web/scripts/ko-locale.mjs`의 `MANUAL_SKIP`이 기준이며, `ko-skip.json`은 `seed`가 다시 만들므로 직접 고치지 않습니다.
- 숫자와 단위는 원문 형식을 유지하고 단위만 한글화합니다("{{count}} items" → "항목 {{count}}개").
- 외래어는 국립국어원 표기를 따릅니다(대시보드, 웹훅, 게이트웨이, 패스키). 영어 약어는 한글로 풀지 않습니다(API, URL, ID, IP, TTL, 2FA).

## 핵심 용어

| English | 한국어 | 비고 |
|---|---|---|
| Quota | 쿼터 | 시드된 값에서 "쿼터"(51회)가 "할당량"(30회)보다 많아 "쿼터"를 씁니다. 정확히 `Quota` 키만 team에서 "할당량"이라 그 라벨과 소수 표기는 단계 8 점검에서 "쿼터"로 통일합니다(아래 "통일 예정"). 새 번역은 "쿼터"만 씁니다. |
| Balance | 잔액 | team 42회, 다른 표현 없음. |
| Credit | 충전 | team의 `Credit` 키가 "충전". 명사로 쓰이는 금액은 "충전 쿼터", 동사 "credit to user"는 "지급"(예: "사용자 계정에 지급할 쿼터"). |
| Top up | 충전 | 동사·명사 모두 "충전". "Top-up order"는 "충전 주문". |
| Recharge | 충전 | "Auto recharge"는 "자동 충전"(띄어쓰기). team에는 "자동충전", "자동결제"도 섞여 있습니다. |
| Redemption code | 충전 코드 | team 16회. "교환 코드"(8회), "상환 코드"(6회), "쿠폰"(8회)도 섞여 있으니 새 번역은 "충전 코드"로 맞춥니다. |
| Subscription | 구독 | |
| Plan | 플랜 | team 52회. "패키지"(2회), "요금제"(1회)는 쓰지 않습니다. |
| Token (API key) | API 키 | 자격 증명으로서의 토큰. team에서 `API Key`는 "API 키". Access Token, Bot Token처럼 이름에 token이 들어간 자격 증명은 "토큰"을 씁니다. |
| Token (usage unit) | 토큰 | 모델이 처리하는 텍스트 단위. team 154회. `token (unit)` 같은 단위 설명은 "토큰(단위)". |
| Channel | 채널 | team 146회. |
| Group | 그룹 | |
| Ratio | 비율 | team 79회, "배율" 14회. 새 번역은 "비율"로 통일합니다. |
| Model ratio | 모델 비율 | |
| Completion ratio | 출력 비율 | 출력(completion) 토큰에 적용하는 비율이라 "출력 비율"을 씁니다. "Completion price"는 "출력 가격", "Audio completion ratio"는 "오디오 출력 비율". team의 "최종 비율"/"완성 비율"은 시드된 값에 1회씩 남아 있어 통일 대상입니다. |
| Group ratio | 그룹 비율 | |
| Price | 가격 | team의 `Price` 키는 "가격 책정"이지만 이는 "Pricing"에 가깝습니다. "Pricing"은 "가격 책정", "Price"는 "가격"을 씁니다. |
| Discount | 할인 | "Discount rate"는 "할인율". |
| Vendor | 공급업체 | 모델 제작사(OpenAI 등)를 가리킬 때. 시드된 값에는 "공급자"(29회)도 섞여 있어 통일 대상입니다. Provider와 구분하려고 Vendor는 "공급업체"로 정합니다. |
| Model | 모델 | |
| Usage log | 사용 로그 | team의 `Usage logs`가 "사용 로그". 일반 "Logs"는 "로그". |
| Request | 요청 | "Requests"는 "요청 수". 단위로 쓰인 소문자 `request`는 team에서 "회". |
| Response | 응답 | |
| Stream | 스트림 | "Stream Mode"는 "스트림 모드". |
| Organization | 조직 | |
| Member | 멤버 | team에서 "멤버" 5회, "구성원" 4회로 거의 같습니다. 조직 화면 제목이 "조직 멤버"로 시작해서 "멤버"로 통일합니다. |
| Owner | 소유자 | team 4회. |
| Admin | 관리자 | "Super Admin"은 "슈퍼 관리자". |
| Suspend | 일시 정지 | team에 없던 용어. 과거형 "Suspended"는 "일시 정지됨", 문장은 "일시 정지되었습니다". Resume과 짝을 이룹니다. |
| Resume | 재개 | team에 없던 용어. 일시 정지된 구성원을 되돌리는 동작. "Pending resume"은 "재개 대기 중". |
| Dashboard | 대시보드 | |
| Billing | 결제 | team에서 "결제" 112회, "청구" 21회. "Billing History"는 "결제 내역", "Billing Details"는 "청구 상세"처럼 갈리지만 새 번역은 "결제"를 기본으로 합니다. team은 "Billing Mode"를 "청구 모드"로 옮겼으므로 같은 표현을 씁니다. |
| Settlement | 정산 | team에 정확한 키는 없고 문장 안에서 "정산"으로 2회 쓰임(예: "최종 정산 전에"). |
| Pre-consume | 사전 차감 | team에서 "사전 차감" 4회, "사전 소비" 2회. "Pre-consumed"는 "사전 차감됨". |
| Invoice | 인보이스 | team 1회. |
| Webhook | 웹훅 | team 대부분 "웹훅". 코드 식별자("webhook secret" 입력란 예시 등)는 영문 유지. |
| Wallet | 지갑 | "Organization wallet"은 "조직 지갑". |
| Limit | 한도 | 금액·쿼터의 상한은 "한도", 접근·빈도 제한은 "제한"(team "제한" 43회, "한도" 10회). |
| Cap | 상한 | team에 없던 용어. "한도"와 같은 계열로 최대값을 뜻할 때. |
| Rate limit | 속도 제한 | team 13회, "비율 제한"은 2회뿐입니다. 새 번역은 "속도 제한". |
| Expired | 만료됨 | 상태 라벨. 문장은 "만료되었습니다". |
| Pending | 대기 중 | |
| Success | 성공 | "Success rate"는 "성공률". |
| Failed | 실패 | 문장은 "~하지 못했습니다". |
| Disabled | 비활성화됨 | 상태 라벨. 문장은 "비활성화되었습니다". |
| Enabled | 활성화됨 | 상태 라벨. 문장은 "활성화되었습니다". |
| Log in | 로그인 | `Sign in`도 "로그인". |
| Sign up | 회원가입 | 계정을 만드는 동작. 항목을 만드는 "Register"는 "등록". |
| Reset | 초기화 | 시드된 값에서 "초기화"(34회)가 "재설정"(22회)보다 많아 "초기화"를 씁니다. 2FA·패스키 같은 보안 맥락도 "초기화"로 맞춥니다. |
| Verify | 인증 | 본인 확인은 "인증", 값 점검은 "확인". |
| Passkey | 패스키 | |
| Two-factor | 2단계 인증 | team "2단계 인증" 12회, "이중 인증" 2회. 약어 2FA는 영문 유지. |
| Export | 내보내기 | |
| Import | 가져오기 | |
| Save | 저장 | |
| Cancel | 취소 | 자동 갱신 해지는 "해지". |
| Delete | 삭제 | |
| Confirm | 확인 | |
| Search | 검색 | |
| Filter | 필터 | 동사는 "필터링". |
| Refresh | 새로고침 | 붙여 씁니다. |
| Copy | 복사 | |
| Timing | 타이밍 | team 1회. "Timing Breakdown"은 "타이밍 분석". |
| Latency | 지연 시간 | team 4회. 짧은 라벨 `Latency short`는 "지연". |
| Provider | 제공자 | OAuth 제공자, 업스트림 "Model Provider"는 "모델 제공자". Vendor(공급업체)와 구분합니다. |
| Gateway | 게이트웨이 | team 16회. "Gateway Overhead"는 "게이트웨이 오버헤드". 결제 게이트웨이도 "게이트웨이". |
| API key | API 키 | team 일관되게 "API 키". |
| Upstream | 업스트림 | 외부 모델 서비스 쪽. |
| Model discount | 모델 할인 | 할인 편집기용. 단계 C 키. |
| Vendor discount | 공급업체 할인 | 할인 편집기용. 단계 C 키. |
| Usage | 사용량 | "Usage records"는 "사용 기록". |
| Logs | 로그 | |
| Settings | 설정 | |
| User | 사용자 | |
| Role | 역할 | |
| Name | 이름 | |
| Status | 상태 | |
| Description | 설명 | |
| Create | 생성 | 예: "그룹 생성". |
| Edit | 편집 | |
| Add | 추가 | |
| Remove | 제거 | 삭제(Delete)와 구분합니다. 목록에서 빼는 동작. |
| Enable | 활성화 | |
| Disable | 비활성화 | |
| Auto-renew | 자동 갱신 | team은 "자동갱신" 4회, "자동결제" 7회로 갈립니다. 새 번역은 띄어 쓴 "자동 갱신"을 씁니다. |
| Free model | 무료 모델 | |
| Multiplier | 배율 | 단독 용어일 때만. 가격·비용 계산의 "ratio"는 "비율". |

## 번역 중 헷갈리기 쉬운 표현

- "Quota"를 "한도"로 옮기지 않습니다. 한도(Limit)는 금액·횟수의 상한이고, 쿼터는 사용자 잔여량 단위입니다.
- 같은 영어 단어가 라벨과 문장에서 다르게 번역되지 않게 합니다(예: 라벨 "쿼터", 문장 "쿼터가 부족합니다").
- 이 표에 없는 용어를 새로 정하면 이 표에 한 줄 추가하고 같은 PR에서 사용합니다.

## 통일 예정

`ko.json`에 team 값을 그대로 시드했기 때문에 아래 소수 표기가 재사용된 번역에 남아 있습니다. 단계 8 점검에서 용어집 표기로 통일합니다. 새 번역에는 쓰지 않습니다.

| 용어집 표기 | 통일할 소수 표기 | 비고 |
|---|---|---|
| 초기화 (Reset) | 재설정 | 시드된 값 22회. |
| 쿼터 (Quota) | 할당량 | 시드된 값 30회. `Quota` 라벨 키를 포함합니다. |
| 공급업체 (Vendor) | 공급자 | 시드된 값 29회. Provider(제공자) 번역과 헷갈리지 않게 구분합니다. |
| 출력 비율 (Completion ratio) | 최종 비율, 완성 비율 | 각 1회. |
