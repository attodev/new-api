# Upstream Rebase 단계 D 구현 계획 — O9 한글, O8 인터페이스 언어, 백엔드 메시지 번역 경로, 단계 C 이월

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `develop`에 한국어를 처음부터 끝까지 붙인다 — 프런트 `ko` locale(7,468키), 백엔드 `ko.yaml`과 fork 메시지의 번역 경로, 언어 선택·제한(en/ko), 달력 locale, README — 그리고 단계 C가 넘긴 소형 결함 4건을 닫는다.

**Architecture:** 프런트는 upstream i18next 구조 그대로 `ko.json` 한 파일을 더한다(코드는 BCP-47 `ko`; team의 `kr`은 레거시 값으로 받아만 준다). team `kr.json`에서 4,592키를 옮기고 나머지 2,876키(+team에 한글이 없던 179키)는 배치 6개로 번역·검증한다. 백엔드는 `i18n/i18n.go`에 `LangKo` 훅 4줄과 `ko.yaml`을 더하고, fork 파일의 `errors.New` 문자열을 `common.NewMessage`로 바꿔 upstream의 `message_key` 번역 경로에 태운다. 인터페이스 언어 제한은 team의 하드코딩 대신 스펙 §4.8대로 upstream 옵션 메커니즘(`InterfaceLanguages`, 기본 `en,ko`)으로 만든다.

**Tech Stack:** Go 1.26(`GOWORK=off`), go-i18n v2, React 19 + i18next/react-i18next + i18next-browser-languagedetector, react-day-picker locale, vitest, `bun run i18n:sync`(`web/scripts/sync-i18n.mjs`), Node 스크립트(번역 배치 검증).

**Spec:** `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §4.8(O9), §2.1 O8 "인터페이스 언어 en/ko 제한", §8 단계 D, §9 결정 4(기계 번역 초안 + 용어집 검수). 이월 출처: `docs/2026-10-14-rebase-phase-c.md` §5.

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`. `/Users/molla/work/ai/new-api`(team 체크아웃)는 읽기 전용; team 소스는 `git show team:<path>`.
- Go는 항상 `GOWORK=off`; JSON은 `common.Marshal/Unmarshal/DecodeJson*`만. SQLite/MySQL/PostgreSQL 동시 호환.
- upstream 파일 수정은 훅 한 줄(또는 필드·상수 한 줄)로 제한하고 `// fork: O9` / `// fork: O8` / `// fork: O3` / `// fork: O6` / `// fork: O7` 주석을 단다(Markdown은 `<!-- fork: O9 -->`). 모든 훅은 `docs/fork-overlays.md`에 `파일:줄` 행으로 기록한다. 로직과 새 locale 파일은 fork 파일.
- 한국어 코드는 **`ko`**(i18next/BCP-47 표준). `kr`은 team 사용자가 `user.setting.language`에 저장했을 수 있는 레거시 값이므로 프런트·백엔드 normalizer가 `ko`로 받아 준다. `Intl` API에 `kr`을 넘기면 카누리어가 되므로 절대 `kr`을 내부 코드로 쓰지 않는다.
- 번역 문체: team `kr.json`의 기존 문체를 따른다 — 안내·오류는 격식체 "~합니다/~하세요", 버튼·라벨은 명사형("저장", "충전"), 용어는 Task 1의 `docs/translation-glossary.ko.md`를 따른다. i18next 보간 `{{name}}`과 Trans 태그 `<0>…</0>`, 줄바꿈 `\n`, 앞뒤 공백은 원문과 **정확히 같게** 유지한다. 숫자·URL·JSON 예시·모델명만 있는 키(`docs/i18n/ko-skip.json`)는 원문 그대로 둔다.
- 다른 locale(zh, zh-TW, fr, ru, ja, vi)의 영어 placeholder 160개는 이 단계에서 번역하지 않는다(결정, 아래 "계획 결정 사항" 2).
- `web/src` 새 TS/TSX 파일은 1행 AGPL 헤더. 손댄 파일마다 `bunx oxlint -c .oxlintrc.json ./src/...`, `bunx oxfmt --check ./src/...`에 새 문제 없음. `bun run format` 금지.
- 커밋: team에서 옮긴 것은 `Ported-From: team <path>`, 모든 커밋에 `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- MySQL/PostgreSQL DSN 테스트·스키마 리허설은 컨테이너가 없어 운영자 작업으로 기록(이 단계는 스키마 변경 없음).

## Review Focus

1. **`ko-KR` 브라우저** — i18next `load: 'currentOnly'`라 `ko-KR`은 `ko` 리소스에 안 맞는다. `convertDetectedLanguage('ko-KR') === 'ko'`여야 한국어 화면이 뜬다. → Task 2 `languages.test.ts` "maps Korean BCP-47 tags onto ko".
2. **team에서 넘어온 사용자(`language: 'kr'`)** — 로그인 뒤 영어로 떨어지면 안 된다. 프런트 `normalizeInterfaceLanguage('kr') === 'ko'`, 백엔드 `normalizeLang("kr") == LangKo`. → Task 2·Task 9 테스트.
3. **보간 변수 훼손** — `{{count}}`를 `{{개수}}`로 번역하면 화면에 깨진 문자열이 뜬다. 배치 검증 스크립트가 보간·태그 집합 불일치를 실패로 처리한다. → Task 1 `ko-locale.mjs check` 테스트(일부러 깨뜨린 값).
4. **백엔드 오류가 영어로만 나오는 조직 화면** — 조직 API의 거부 메시지(`organization target permission denied` 등)가 ko 사용자에게도 영어로 보이면 안 된다. 45개 키가 `en.json`/`ko.json`에 있어야 하고 `server-error-message`가 번역한다. → Task 9 `server-error-message.test.ts` 조직 키 케이스.
5. **허용 언어 목록에서 빠진 언어를 이미 쓰는 사용자** — `InterfaceLanguages=en,ko`로 바꿨을 때 `ja`를 저장한 사용자가 깨진 화면을 보면 안 된다: 선택 메뉴에서만 빠지고 현재 언어는 유지된다. → Task 10 `language-switcher.test.tsx` "keeps a saved language that is no longer offered".

---

## 파일 구조

| 영역 | 생성(fork) | 수정(훅) |
|---|---|---|
| 번역 도구·용어집 | `web/scripts/ko-locale.mjs`, `web/scripts/ko-locale.test.mjs`, `docs/translation-glossary.ko.md`, `docs/i18n/ko-batches.json`, `docs/i18n/ko-skip.json`(Task 12에서 배치 파일 삭제) | — |
| ko locale | `web/src/i18n/locales/ko.json`, `web/src/i18n/locales/_reports/ko.untranslated.json` | `web/src/i18n/languages.ts`, `web/src/i18n/config.ts`, `web/src/i18n/languages.test.ts`, `web/scripts/sync-i18n.mjs:230`, `web/src/components/date-picker.tsx`, `web/src/components/datetime-picker.tsx` |
| 백엔드 | `i18n/locales/ko.yaml`, `i18n/ko_test.go`, `common/message_fork.go`(필요 시) | `i18n/i18n.go:19-24,44,54-56,195-215`, `i18n/i18n_test.go`, `common/constants.go:39-41`(주석), fork 파일들의 `errors.New` → `NewMessage`, `web/src/i18n/locales/*.json`(fork 메시지 키 45개) |
| O8 옵션 | `web/src/features/system-settings/general/interface-languages-section.tsx`, `web/src/lib/interface-languages.ts`(+test) | `common/constants.go`, `model/option.go:81,616`, `controller/misc.go:85`, `web/src/lib/status-query.ts:97`, `web/src/stores/system-config-store.ts:42-49`, `web/src/features/system-settings/hooks/use-update-option.ts:30-49`, `operations/section-registry.tsx`, `operations/index.tsx`, `system-settings/types.ts`, `web/src/components/language-switcher.tsx`, `web/src/features/profile/components/language-preferences-card.tsx` |
| README·이월 | `README.kr.md` | `README*.md`(nav 1줄), `model/pricing_discount.go`, `model/topup.go`/`controller/topup.go`(개인 주문 만료), `router/api-router.go`(1줄), `web/src/features/usage-logs/components/common-log-mobile-card.tsx:203-204,381` |
| 기록 | `docs/2026-10-15-rebase-phase-d.md` | `docs/fork-overlays.md`, spec §4.8 |

---

### Task 1: 한글 용어집과 번역 작업 도구

**Files:**
- Create: `docs/translation-glossary.ko.md`, `web/scripts/ko-locale.mjs`, `web/scripts/ko-locale.test.mjs`, `docs/i18n/ko-skip.json`, `docs/i18n/ko-batches.json`

**Interfaces:**
- Produces: `node scripts/ko-locale.mjs seed` → `src/i18n/locales/ko.json`(team 한글 + 영어 placeholder) + `docs/i18n/ko-batches.json`(`{"batches":[{"id":1,"keys":[…]}, …]}`, 배치 6개 ≈ 각 510키) + `docs/i18n/ko-skip.json`; `node scripts/ko-locale.mjs check --batch N` → 종료코드 0/1과 위반 목록; `node scripts/ko-locale.mjs status` → 남은 영어 placeholder 수.
- 용어집: 표 `| English | 한국어 | 비고 |`, 최소 60개 핵심 용어.

- [ ] **Step 1: 검증 스크립트 테스트(실패 확인)**

`web/scripts/ko-locale.test.mjs`(Node `node:test`):

```js
import test from 'node:test'
import assert from 'node:assert/strict'
import { validateEntry } from './ko-locale.mjs'

test('accepts a faithful translation', () => {
  assert.deepEqual(validateEntry('Top up {{amount}} now', '지금 {{amount}} 충전', { skip: false }), [])
})
test('rejects a changed interpolation set', () => {
  const v = validateEntry('{{count}} items', '{{개수}}개 항목', { skip: false })
  assert.ok(v.some((x) => x.includes('interpolation')))
})
test('rejects a missing Trans tag', () => {
  const v = validateEntry('Read the <0>docs</0>', '문서를 읽으세요', { skip: false })
  assert.ok(v.some((x) => x.includes('tag')))
})
test('rejects an untranslated english value unless skipped', () => {
  assert.ok(validateEntry('Save group settings', 'Save group settings', { skip: false }).length > 0)
  assert.deepEqual(validateEntry('https://example.com', 'https://example.com', { skip: true }), [])
})
test('rejects leading/trailing whitespace drift', () => {
  assert.ok(validateEntry(' Prefix', '접두어', { skip: false }).length > 0)
})
```

Run: `cd web && node --test scripts/ko-locale.test.mjs` → FAIL(모듈 없음).

- [ ] **Step 2: 스크립트 구현**

`web/scripts/ko-locale.mjs`(AGPL 헤더는 `sync-i18n.mjs` 상단과 같은 형식으로):

```js
// subcommands: seed | check --batch N | status
import fs from 'node:fs/promises'
import path from 'node:path'
import { execFileSync } from 'node:child_process'

const LOCALES = path.resolve('src/i18n/locales')
const EN = path.join(LOCALES, 'en.json')
const KO = path.join(LOCALES, 'ko.json')
const BATCHES = path.resolve('../docs/i18n/ko-batches.json')
const SKIP = path.resolve('../docs/i18n/ko-skip.json')
const BATCH_COUNT = 6
const HANGUL = /[가-힣]/
const INTERP = /\{\{\s*[^}]+\s*\}\}/g
const TAG = /<\/?\d+>/g

export function validateEntry(en, ko, { skip }) {
  const issues = []
  if (typeof ko !== 'string') return ['value is not a string']
  if (skip) return en === ko ? [] : ['skip key must keep the english value']
  if (ko === en) issues.push('untranslated (equals english)')
  if (!HANGUL.test(ko)) issues.push('no hangul')
  const a = (en.match(INTERP) ?? []).map((s) => s.replace(/\s/g, '')).sort()
  const b = (ko.match(INTERP) ?? []).map((s) => s.replace(/\s/g, '')).sort()
  if (a.join('|') !== b.join('|')) issues.push(`interpolation set differs: ${a} vs ${b}`)
  const ta = (en.match(TAG) ?? []).sort(), tb = (ko.match(TAG) ?? []).sort()
  if (ta.join('|') !== tb.join('|')) issues.push(`tag set differs: ${ta} vs ${tb}`)
  if ((en.startsWith(' ') !== ko.startsWith(' ')) || (en.endsWith(' ') !== ko.endsWith(' '))) issues.push('whitespace drift')
  if (en.includes('\n') !== ko.includes('\n')) issues.push('newline drift')
  return issues
}

function isSkippable(key) {
  return /^[\d\s.,%:/\-+*]+$/.test(key) || key.startsWith('{') || key.startsWith('[') || /^https?:\/\//.test(key) || key.length < 2
}

async function readJson(p) { return JSON.parse(await fs.readFile(p, 'utf8')) }
async function writeJson(p, v) { await fs.writeFile(p, JSON.stringify(v, null, 2) + '\n') }

async function seed() {
  const en = (await readJson(EN)).translation
  const kr = JSON.parse(execFileSync('git', ['show', 'team:web/default/src/i18n/locales/kr.json'], { encoding: 'utf8' })).translation
  const ko = {}, todo = [], skip = []
  for (const key of Object.keys(en)) {
    const v = kr[key]
    if (isSkippable(key)) { ko[key] = en[key]; skip.push(key); continue }
    if (typeof v === 'string' && HANGUL.test(v) && validateEntry(en[key], v, { skip: false }).length === 0) ko[key] = v
    else { ko[key] = en[key]; todo.push(key) }
  }
  await writeJson(KO, { translation: ko })
  await writeJson(SKIP, skip)
  const size = Math.ceil(todo.length / BATCH_COUNT)
  await writeJson(BATCHES, { batches: Array.from({ length: BATCH_COUNT }, (_, i) => ({ id: i + 1, keys: todo.slice(i * size, (i + 1) * size) })) })
  console.log(`seeded: reused ${Object.keys(en).length - todo.length - skip.length}, todo ${todo.length}, skip ${skip.length}`)
}

async function check(batchId) {
  const en = (await readJson(EN)).translation, ko = (await readJson(KO)).translation
  const skip = new Set(await readJson(SKIP))
  const keys = batchId ? (await readJson(BATCHES)).batches.find((b) => b.id === batchId).keys : Object.keys(en)
  const bad = []
  for (const k of keys) {
    if (!(k in ko)) { bad.push([k, ['missing']]); continue }
    const issues = validateEntry(en[k], ko[k], { skip: skip.has(k) })
    if (issues.length) bad.push([k, issues])
  }
  for (const [k, issues] of bad) console.log(`✗ ${JSON.stringify(k)}: ${issues.join('; ')}`)
  console.log(`${keys.length - bad.length}/${keys.length} ok`)
  process.exit(bad.length ? 1 : 0)
}

async function status() {
  const en = (await readJson(EN)).translation, ko = (await readJson(KO)).translation, skip = new Set(await readJson(SKIP))
  const left = Object.keys(en).filter((k) => !skip.has(k) && ko[k] === en[k])
  console.log(`english placeholders left: ${left.length}`)
}

const [cmd, flag, arg] = process.argv.slice(2)
if (cmd === 'seed') await seed()
else if (cmd === 'check') await check(flag === '--batch' ? Number(arg) : undefined)
else if (cmd === 'status') await status()
else if (!cmd || cmd === '--help') console.log('usage: ko-locale.mjs seed | check [--batch N] | status')
```

`import.meta.url`로 직접 실행 여부를 판정해 테스트가 import할 때 subcommand가 돌지 않게 한다(`if (process.argv[1] && import.meta.url.endsWith(path.basename(process.argv[1])))`). `package.json` scripts에 `"i18n:ko": "node scripts/ko-locale.mjs"` 추가(upstream 파일 한 줄, `// fork: O9`는 JSON이라 못 쓰니 overlay 표에만 기록).

Run: `node --test scripts/ko-locale.test.mjs` → PASS. `node scripts/ko-locale.mjs seed` → `ko.json`, `docs/i18n/ko-batches.json`(6배치), `docs/i18n/ko-skip.json` 생성, 출력 예 `seeded: reused ~4413, todo ~2994, skip ~61`(정확한 수를 보고서에 적는다).

- [ ] **Step 3: 용어집**

`docs/translation-glossary.ko.md`: team `kr.json`에서 아래 영어 용어가 어떻게 옮겨졌는지 `git show team:web/default/src/i18n/locales/kr.json`으로 찾아 표를 채운다(값이 없거나 두 가지로 섞여 있으면 하나를 고르고 비고에 적는다): Quota, Balance, Credit, Top up, Recharge, Redemption code, Subscription, Plan, Token(API key), Token(usage unit), Channel, Group, Ratio, Model ratio, Completion ratio, Group ratio, Price, Discount, Vendor, Model, Usage log, Request, Response, Stream, Organization, Member, Owner, Admin, Suspend, Resume, Dashboard, Billing, Settlement, Pre-consume, Invoice, Webhook, Wallet, Limit, Cap, Rate limit, Expired, Pending, Success, Failed, Disabled, Enabled, Log in, Sign up, Reset, Verify, Passkey, Two-factor, Export, Import, Save, Cancel, Delete, Confirm, Search, Filter, Refresh, Copy, Timing, Latency, Provider, Gateway. 머리말에 문체 규칙(격식체/명사형, 보간·태그 보존, 영문 고유명사는 그대로)을 적는다.

- [ ] **Step 4: 커밋**

```bash
git add web/scripts/ko-locale.mjs web/scripts/ko-locale.test.mjs web/package.json docs/translation-glossary.ko.md docs/i18n/ko-skip.json docs/i18n/ko-batches.json web/src/i18n/locales/ko.json
git commit -m "feat(i18n): seed the Korean locale from team and add the batch validation tool

Ported-From: team web/default/src/i18n/locales/kr.json
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: `ko` locale 배선(언어 목록·감지·달력·sync)

**Files:**
- Modify: `web/src/i18n/languages.ts:19-27,32-50,55-70`, `web/src/i18n/config.ts:23-48`, `web/src/i18n/languages.test.ts`, `web/scripts/sync-i18n.mjs:230`, `web/src/components/date-picker.tsx:20,33-40`, `web/src/components/datetime-picker.tsx:21,36-43`

**Interfaces:**
- Consumes: Task 1의 `ko.json`.
- Produces: `INTERFACE_LANGUAGE_OPTIONS`에 `{ code: 'ko', label: '한국어' }`; `normalizeInterfaceLanguage('kr'|'ko-KR'|'ko_KR') === 'ko'`; `convertDetectedLanguage('ko-KR') === 'ko'`; `resources.ko`; `supportedLngs`에 `'ko'`; 달력 `calendarLocales.ko`.

- [ ] **Step 1: 테스트(실패 확인)** — `languages.test.ts`에 추가:

```ts
test('maps Korean BCP-47 tags onto ko', () => {
  expect(convertDetectedLanguage('ko')).toBe('ko')
  expect(convertDetectedLanguage('ko-KR')).toBe('ko')
  expect(convertDetectedLanguage('ko_KR')).toBe('ko')
  expect(convertDetectedLanguage('kr')).toBe('ko') // legacy team code
})
test('normalizes saved Korean codes, including the legacy kr', () => {
  expect(normalizeInterfaceLanguage('ko')).toBe('ko')
  expect(normalizeInterfaceLanguage('ko-KR')).toBe('ko')
  expect(normalizeInterfaceLanguage('kr')).toBe('ko')
  expect(normalizeInterfaceLanguage('pt-BR')).toBe('en')
})
test('lists Korean as an interface language', () => {
  expect(INTERFACE_LANGUAGE_OPTIONS.map((o) => o.code)).toContain('ko')
})
```

Run: `cd web && bun run test -- i18n/languages` → FAIL.

- [ ] **Step 2: 구현**

`languages.ts`: 옵션 목록에 `{ code: 'ko', label: '한국어' }, // fork: O9` 추가(순서: en 다음); `normalizeInterfaceLanguage`에 `if (normalized === 'kr' || normalized.startsWith('ko')) normalized = 'ko' // fork: O9`; `convertDetectedLanguage`에 `if (lower === 'kr' || lower.startsWith('ko')) return 'ko' // fork: O9` (zh 분기 앞). `config.ts`: `import ko from './locales/ko.json' // fork: O9`, `resources`에 `ko,`, `supportedLngs`에 `'ko'`. `sync-i18n.mjs:230`: `if (locale === 'ja' || locale === 'zh' || locale === 'ko') return true // fork: O9`. `date-picker.tsx`/`datetime-picker.tsx`: `import { …, ko } from 'react-day-picker/locale'`와 맵에 `ko, // fork: O9`. Run: Step 1 PASS, `bun run i18n:sync` → `_reports/ko.untranslated.json` 생성(초안 전이므로 ~2,994개), `bun run typecheck`, 파일별 oxlint/oxfmt.

- [ ] **Step 3: 브라우저 확인**

`bun run dev`로 열고 브라우저 언어를 `ko-KR`로(또는 localStorage `i18nextLng=ko`) 설정 → 로그인 화면 라벨이 team 한글로 보이는지, 언어 메뉴에 "한국어"가 있는지, 달력이 한국어 요일인지 확인. 스크린샷 `docs/i18n/images/login-ko.png`.

- [ ] **Step 4: 커밋**

```bash
git add web/src/i18n web/scripts/sync-i18n.mjs web/src/components/date-picker.tsx web/src/components/datetime-picker.tsx docs/i18n/images docs/fork-overlays.md
git commit -m "feat(web): wire the Korean interface language (ko) into i18next, detection and calendars

Ported-From: team web/default/src/i18n/languages.ts web/default/src/i18n/config.ts
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: 한국어 번역 배치 1

**Files:**
- Modify: `web/src/i18n/locales/ko.json`(배치 1 키만), `web/src/i18n/locales/_reports/ko.untranslated.json`(sync 결과)

**Interfaces:**
- Consumes: `docs/i18n/ko-batches.json`의 `batches[0].keys`, `docs/translation-glossary.ko.md`, `docs/i18n/ko-skip.json`.
- Produces: 배치 1 키 전부 한국어; `node scripts/ko-locale.mjs check --batch 1` 종료코드 0.

- [ ] **Step 1: 실패 확인**

Run: `cd web && node scripts/ko-locale.mjs check --batch 1` → 영어 placeholder 수만큼 `✗ … untranslated` 출력, 종료코드 1.

- [ ] **Step 2: 번역**

배치 1 키(≈510개)를 `docs/translation-glossary.ko.md`의 용어·문체 규칙으로 번역해 `ko.json`의 해당 키 값만 바꾼다(다른 키·순서는 그대로; 파일은 2칸 들여쓰기 JSON 유지). 긴 안내문(80자 이상, 배치당 수십 개)은 원문의 문장 수를 유지한다. 보간·태그·공백·줄바꿈은 원문과 동일. 애매한 용어는 용어집에 행을 추가하고 커밋에 포함한다. 브랜드·제품명·프로토콜 이름처럼 한국어로 옮기지 않는 키("OpenAI", "Bearer", "JSON")는 원문을 그대로 두고 `docs/i18n/ko-skip.json`에 키를 추가한다(배치당 추가 수와 목록을 보고서에 적는다; 검증 스크립트는 skip 키에 원문 유지를 요구한다).

- [ ] **Step 3: 검증**

Run: `node scripts/ko-locale.mjs check --batch 1` → `N/N ok`, 종료코드 0; `bun run i18n:sync`(ko 보고서 갱신, 다른 locale 파일이 바뀌면 안 됨 — `git status`로 확인); `bun run test -- i18n` 통과. 무작위 표본 20개를 보고서에 원문/번역 쌍으로 적는다.

- [ ] **Step 4: 커밋**

```bash
git add web/src/i18n/locales/ko.json web/src/i18n/locales/_reports/ko.untranslated.json docs/translation-glossary.ko.md
git commit -m "feat(i18n): translate Korean locale batch 1/6

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: 한국어 번역 배치 2

Task 3과 동일한 절차를 `batches[1]`(배치 id 2)에 적용한다.

- [ ] **Step 1:** `node scripts/ko-locale.mjs check --batch 2` → 종료코드 1 확인.
- [ ] **Step 2:** 배치 2 키 번역(용어집·문체·보간 규칙 동일, 용어집 보강 포함).
- [ ] **Step 3:** `check --batch 2` → 0; `bun run i18n:sync`; `bun run test -- i18n`; 표본 20개 기록.
- [ ] **Step 4:** 커밋 `feat(i18n): translate Korean locale batch 2/6` (같은 트레일러).

---

### Task 5: 한국어 번역 배치 3

Task 3과 동일한 절차를 `batches[2]`(배치 id 3)에 적용한다.

- [ ] **Step 1:** `check --batch 3` → 1 확인. **Step 2:** 번역. **Step 3:** `check --batch 3` → 0, `i18n:sync`, `test -- i18n`, 표본 20개. **Step 4:** 커밋 `feat(i18n): translate Korean locale batch 3/6`.

---

### Task 6: 한국어 번역 배치 4

Task 3과 동일한 절차를 `batches[3]`(배치 id 4)에 적용한다.

- [ ] **Step 1:** `check --batch 4` → 1 확인. **Step 2:** 번역. **Step 3:** `check --batch 4` → 0, `i18n:sync`, `test -- i18n`, 표본 20개. **Step 4:** 커밋 `feat(i18n): translate Korean locale batch 4/6`.

---

### Task 7: 한국어 번역 배치 5

Task 3과 동일한 절차를 `batches[4]`(배치 id 5)에 적용한다.

- [ ] **Step 1:** `check --batch 5` → 1 확인. **Step 2:** 번역. **Step 3:** `check --batch 5` → 0, `i18n:sync`, `test -- i18n`, 표본 20개. **Step 4:** 커밋 `feat(i18n): translate Korean locale batch 5/6`.

---

### Task 8: 한국어 번역 배치 6과 전체 검수

Task 3과 동일한 절차를 `batches[5]`(배치 id 6)에 적용한 뒤 전체를 검수한다.

- [ ] **Step 1:** `check --batch 6` → 1 확인. **Step 2:** 번역. **Step 3:** `check --batch 6` → 0.
- [ ] **Step 4: 전체 검수**

Run: `node scripts/ko-locale.mjs check`(배치 없이 전체) → 0; `node scripts/ko-locale.mjs status` → `english placeholders left: 0`; `bun run i18n:sync` → `_reports/ko.untranslated.json`이 skip 목록 외에 비어 있음. 용어 일관성: 용어집의 각 한국어 용어에 대해 `ko.json`에서 같은 영어 용어가 다른 한국어로 옮겨진 값을 grep으로 찾아(예: "Quota"가 "할당량"과 "잔액"으로 섞임) 용어집 기준으로 통일하고 수정 수를 보고서에 적는다. 화면 확인: `bun run dev` + ko로 콘솔 대시보드·토큰·사용 로그·지갑·설정 각 1회 스크린샷(`docs/i18n/images/*-ko.png`, 5장 이내).

- [ ] **Step 5:** 커밋 `feat(i18n): translate Korean locale batch 6/6 and unify terminology`.

---

### Task 9: 백엔드 한국어 — `ko.yaml`, 언어 감지, fork 메시지의 번역 경로

**Files:**
- Create: `i18n/locales/ko.yaml`, `i18n/ko_test.go`
- Modify: `i18n/i18n.go:19-24`(상수), `:44`(파일 목록), `:54-56`(localizer), `:195-215`(`normalizeLang`, `SupportedLanguages`), `i18n/i18n_test.go:32-39`(케이스), `common/constants.go:39-41`(주석 한 줄), fork 파일의 `errors.New`/`fmt.Errorf` → `common.NewMessage`(목록: `docs/i18n/fork-errors.json`를 Task 시작 때 `grep`으로 다시 만든다), `web/src/i18n/locales/{en,ko,zh,zh-TW,fr,ja,ru,vi}.json`(fork 메시지 키 추가), `web/src/lib/server-error-message.test.ts`

**Interfaces:**
- Produces: `i18n.LangKo = "ko"`; `normalizeLang("ko-KR"|"kr") == LangKo`; `SupportedLanguages()`에 `LangKo`; `ko.yaml`은 `en.yaml`의 66키 전부; fork 조직·결제 파일의 사용자 노출 오류는 모두 `common.NewMessage(key, params)`(키 = 영어 원문, `{{name}}` 보간)이고 그 키가 `en.json`/`ko.json`에 있다.

- [ ] **Step 1: 백엔드 테스트(실패 확인)** — `i18n/ko_test.go`:

```go
package i18n

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKoreanIsSupported(t *testing.T) {
	require.Equal(t, LangKo, normalizeLang("ko"))
	require.Equal(t, LangKo, normalizeLang("ko-KR"))
	require.Equal(t, LangKo, normalizeLang("ko_KR"))
	require.Equal(t, LangKo, normalizeLang("kr")) // legacy team value
	require.True(t, IsSupported("ko"))
	require.Contains(t, SupportedLanguages(), LangKo)
	require.Equal(t, LangKo, ParseAcceptLanguage("ko-KR,ko;q=0.9,en;q=0.8"))
}

func TestKoreanYamlCoversEveryEnglishKey(t *testing.T) {
	en, err := localeFS.ReadFile("locales/en.yaml")
	require.NoError(t, err)
	ko, err := localeFS.ReadFile("locales/ko.yaml")
	require.NoError(t, err)
	for _, key := range yamlKeys(en) {
		require.Contains(t, yamlKeys(ko), key, "ko.yaml lacks %s", key)
	}
	require.NotEqual(t, Translate(LangKo, "common.database_error"), Translate(LangEn, "common.database_error"))
}
```

`yamlKeys`는 `i18n_test.go`에 이미 있는 helper를 쓰거나(`:139` 근처) 없으면 테스트 파일에 `^([A-Za-z0-9_.-]+):` 정규식으로 만든다. Run: `GOWORK=off go test ./i18n/ -run Korean -v` → FAIL.

- [ ] **Step 2: 백엔드 구현**

`i18n/i18n.go`: `LangKo = "ko" // fork: O9`; 파일 목록에 `"locales/ko.yaml"`; `localizers[LangKo] = i18n.NewLocalizer(bundle, LangKo) // fork: O9`; `normalizeLang`에 `case lang == "kr" || strings.HasPrefix(lang, "ko"): return LangKo // fork: O9`(zh 분기 앞); `SupportedLanguages()`에 `LangKo`. `i18n_test.go`의 케이스 표에 `{"ko", LangKo}, {"ko-KR", LangKo}, {"kr", LangKo}` 세 줄(`// fork: O9`). `common/constants.go:39-41` 주석에 `, ko` 한 단어(`// fork: O9`). `ko.yaml`: `en.yaml`의 66키를 같은 순서·주석 구조로; team `ko.yaml`(`git show team:i18n/locales/ko.yaml`)에 같은 키가 있는 19개는 그 값을, 나머지 47개는 용어집으로 번역(보간 `{{.Group}}` 같은 Go 템플릿 변수는 그대로). Run: Step 1 PASS, `GOWORK=off go test ./i18n/ ./middleware/`.

- [ ] **Step 3: fork 메시지 번역 경로(실패 확인 → 구현)**

인벤토리: `grep -rn 'errors\.New("\|fmt\.Errorf("\|NewMessage("\|ApiErrorT(c, "' --include='*.go' model/organization*.go controller/organization*.go service/*organization*.go model/topup_target.go controller/topup_target.go service/org_user_export.go model/organization_personal_wallet.go controller/contact.go controller/email.go controller/topup_paypal.go setting/ratio_setting/model_discount.go | grep -v _test`. 규칙: 사용자에게 응답으로 나가는 문자열(핸들러가 `common.ApiError(c, err)`/`ApiErrorT`/`c.JSON(... "message": ...)`로 보내거나, 모델·서비스에서 그런 핸들러로 올라가는 오류)은 전부 `common.NewMessage("English text {{name}}", map[string]any{"name": v})`로 바꾼다(`fmt.Errorf("%s is required", f)` → `NewMessage("{{field}} is required", map[string]any{"field": f})`). 내부 전용 오류(로그에만 남거나 `errors.Is` 센티널로만 쓰임)는 그대로 두되 보고서에 목록을 적는다. `errors.Is` 비교 대상인 센티널(`ErrOrganizationDisabled` 등)은 `common.NewMessage` 값으로 바꿔도 `errors.Is`가 포인터 동일성으로 동작하는지 확인(`common.Message`가 `Error()`를 구현하므로 `var ErrX = common.NewMessage(...)` 패턴은 Phase B에서 이미 쓰고 있다). 테스트: 기존 컨트롤러 테스트가 `resp.Body`의 영어 문구를 단언하므로 그대로 통과해야 한다; 추가로 `controller/organization_i18n_test.go`에 "모든 fork `NewMessage` 키가 `web/src/i18n/locales/en.json`에 존재" 검사를 넣는다(Go 테스트가 `../web/src/i18n/locales/en.json`을 읽어 `translation` 맵의 키 집합과 비교; 키 목록은 소스 파일을 정규식으로 스캔). Run → FAIL(45개 미등록) → `en.json`과 `ko.json`에 키 추가(ko는 번역, 나머지 5개 locale은 영어 placeholder) → PASS. `web/src/lib/server-error-message.test.ts`에 조직 키 1건: `{message_key: 'organization target permission denied'}`가 ko에서 한국어로 렌더되는 케이스(기존 테스트의 언어 전환 방식을 따른다).

- [ ] **Step 4: 전체 검증과 커밋**

Run: `GOWORK=off go vet ./... && GOWORK=off go test ./i18n/ ./middleware/ ./controller/ ./model/ ./service/... ./router/`; `cd web && bun run test -- server-error-message i18n && bun run typecheck && bun run i18n:sync`(ko 보고서에 새 키 0). 커밋 2개:

```bash
git commit -m "feat(i18n): add the Korean backend locale and accept legacy kr codes

Ported-From: team i18n/locales/ko.yaml i18n/i18n.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
git commit -m "fix(organization): route fork error messages through the translatable message path

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: O8 인터페이스 언어 제한(옵션 `InterfaceLanguages`, 기본 `en,ko`)

**Files:**
- Create: `web/src/lib/interface-languages.ts`, `web/src/lib/interface-languages.test.ts`, `web/src/features/system-settings/general/interface-languages-section.tsx`, `web/src/components/__tests__/language-switcher.test.tsx`, `common/interface_languages_fork.go`, `common/interface_languages_fork_test.go`
- Modify: `common/constants.go`(var 1줄), `model/option.go:81`(InitOptionMap 1줄), `:616`(updateOptionMap case), `controller/misc.go:85`(status 1줄), `web/src/lib/status-query.ts:97`, `web/src/stores/system-config-store.ts:42-49`, `web/src/features/system-settings/hooks/use-update-option.ts:30-49`, `web/src/features/system-settings/operations/section-registry.tsx`, `operations/index.tsx`, `system-settings/types.ts:369`, `web/src/components/language-switcher.tsx:36`, `web/src/features/profile/components/language-preferences-card.tsx:119`, 7 locale

**Interfaces:**
- Produces: 옵션 키 `InterfaceLanguages`(쉼표 구분 코드 문자열, 기본 `"en,ko"`); `common.InterfaceLanguages string`; `common.ParseInterfaceLanguages(value string) []string`(공백 제거, 소문자화 후 `kr→ko`, 중복 제거, 알 수 없는 코드 제거, 비면 `["en"]`); `/api/status`의 `interface_languages: []string`; 프런트 `SystemConfig.interfaceLanguages: string[]`; `filterInterfaceLanguages(options, allowed, current)` → 허용 목록 ∪ {현재 언어}.

- [ ] **Step 1: 테스트(실패 확인)**

`common/interface_languages_fork_test.go`:

```go
func TestParseInterfaceLanguages(t *testing.T) {
	require.Equal(t, []string{"en", "ko"}, ParseInterfaceLanguages("en, ko"))
	require.Equal(t, []string{"ko", "en"}, ParseInterfaceLanguages("kr,EN,unknown,ko"))
	require.Equal(t, []string{"en"}, ParseInterfaceLanguages(""))
	require.Equal(t, []string{"en"}, ParseInterfaceLanguages("xx"))
}
```

`web/src/lib/interface-languages.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { filterInterfaceLanguages } from './interface-languages'
const all = [{ code: 'en', label: 'English' }, { code: 'ko', label: '한국어' }, { code: 'ja', label: '日本語' }]
describe('filterInterfaceLanguages', () => {
  it('keeps only allowed codes', () => {
    expect(filterInterfaceLanguages(all, ['en', 'ko'], 'en').map((o) => o.code)).toEqual(['en', 'ko'])
  })
  it('keeps a saved language that is no longer offered', () => {
    expect(filterInterfaceLanguages(all, ['en', 'ko'], 'ja').map((o) => o.code)).toEqual(['en', 'ko', 'ja'])
  })
  it('falls back to every option when the list is empty', () => {
    expect(filterInterfaceLanguages(all, [], 'en')).toEqual(all)
  })
})
```

`language-switcher.test.tsx`: 스토어 `config.interfaceLanguages = ['en','ko']`, 현재 언어 `ja` → 메뉴에 English, 한국어, 日本語 3개; `['en','ko']`, 현재 `en` → 2개. Run → FAIL.

- [ ] **Step 2: 구현**

백엔드: `common/constants.go`에 `var InterfaceLanguages = "en,ko" // fork: O8`; `common/interface_languages_fork.go`에 `ParseInterfaceLanguages`(허용 코드 집합 `{en, ko, zhCN, zhTW, fr, ru, ja, vi}`); `model/option.go:81` 옆 `common.OptionMap["InterfaceLanguages"] = common.InterfaceLanguages // fork: O8`, `:616` 옆 `case "InterfaceLanguages": common.InterfaceLanguages = value // fork: O8`; `controller/misc.go:85` 옆 `"interface_languages": common.ParseInterfaceLanguages(common.InterfaceLanguages), // fork: O8`. 프런트: `status-query.ts:97`에 `interfaceLanguages: data.interface_languages ?? [], // fork: O8`; `system-config-store.ts`의 `SystemConfig`에 `interfaceLanguages: string[] // fork: O8`(기본 `[]`); `use-update-option.ts` `STATUS_RELATED_KEYS`에 `'InterfaceLanguages', // fork: O8`; `lib/interface-languages.ts`에 `filterInterfaceLanguages`; `language-switcher.tsx:36`과 `language-preferences-card.tsx:119`가 `INTERFACE_LANGUAGE_OPTIONS` 대신 `filterInterfaceLanguages(INTERFACE_LANGUAGE_OPTIONS, useSystemConfigStore(s => s.config.interfaceLanguages), i18n.language)`를 쓰도록 한 줄씩(`// fork: O8`); 설정 UI `general/interface-languages-section.tsx`(체크박스 목록 → 쉼표 문자열로 `InterfaceLanguages` 저장; `system-behavior-section.tsx`의 `SettingsSection`/저장 패턴을 따른다)를 `operations/section-registry.tsx`에 등록(`id: 'interface-languages'`), `operations/index.tsx` 기본값 `InterfaceLanguages: 'en,ko'`, `types.ts`에 필드. i18n 키 `Interface languages`, `Languages offered in the language menu. The current language of a user stays available.` 7 locale(ko 번역 포함).

Run: Step 1 PASS, `GOWORK=off go test ./common/ ./model/ ./controller/`, `bun run test -- interface-languages language-switcher system-settings`, typecheck, 파일별 oxlint/oxfmt, `bun run build`. 브라우저: 설정에서 ja 해제 → 헤더 언어 메뉴에 en/ko만; `/api/status`에 `interface_languages`. overlay 행 추가(O8).

- [ ] **Step 3: 커밋**

```bash
git commit -m "feat(settings): restrict the interface language menu through the InterfaceLanguages option

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: README 한국어판과 단계 C 이월 4건

**Files:**
- Create: `README.kr.md`, `model/pricing_discount_refresh_test.go`, `controller/topup_expire_admin.go`, `controller/topup_expire_admin_test.go`
- Modify: `README.md`, `README.en.md`, `README.zh_CN.md`, `README.zh_TW.md`, `README.fr.md`, `README.ja.md`(nav 링크 1줄, `<!-- fork: O9 -->`), `model/pricing_discount.go:57-120`, `model/topup.go`(개인 주문 만료 함수, fork 파일 `model/topup_expire_fork.go`로), `router/api-router.go:159-160`(관리자 라우트 1줄, `// fork: O3`), `service/authz/resources_*.go`(해당 리소스), `web/src/features/usage-logs/components/common-log-mobile-card.tsx:203-204,381`, `docs/openapi/organization-rest-api.md`

**Interfaces:**
- Produces: `POST /api/topup/expire`(관리자; body `{"user_id": n}` 선택, 없으면 전체) → `{expired: n, skipped_recent: m}`: 개인 pending 주문 중 `create_time < now-1h`를 `expired`로(조직 주문 제외); 벤더명 스냅숏이 1분보다 오래되면 비차단 재구축(`go GetPricing()` + `sync.Mutex.TryLock`); 모바일 카드 tokens/s가 `effectiveUseTimeSec`을 쓴다.

- [ ] **Step 1: README**

`README.kr.md`: develop의 `README.md`(361줄, upstream 신판)를 한국어로 옮긴다 — team `README.kr.md`(구판 488줄 기준)에서 같은 절의 문장은 재사용, 새 절은 번역. 상단 언어 nav에 `한국어` 링크를 넣고, 다른 README 6개의 nav `<p align="center">` 블록에 `| <a href="./README.kr.md">한국어</a>`(Markdown/HTML 주석 `<!-- fork: O9 -->`을 같은 줄 끝에). 이미지 경로·배지 그대로.

- [ ] **Step 2: 벤더명 스냅숏 갱신(실패 확인 → 구현)**

`model/pricing_discount_refresh_test.go`: 스냅숏 발행 시각을 2분 전으로 돌린 뒤 `resolveVendorName("gpt-4o")` 호출 → 비차단 재구축이 예약되고(테스트용 훅으로 `GetPricing` 호출 횟수 카운트) 호출 자체는 즉시 이전 스냅숏을 돌려준다; 1분 이내면 재구축 없음; 재구축 중 두 번째 호출은 `TryLock` 실패로 중복 기동 없음. 구현: `publishModelVendorNames`가 `modelVendorNamePublishedAt`을 기록; `resolveVendorName`이 `time.Since(publishedAt) > vendorNameRefreshAge(1m)`이면 `go refreshPricingOnce()`(패키지 `refreshMu sync.Mutex` + `TryLock`) 후 스냅숏 반환. `updatePricing`의 벤더 조회 오류를 무시하는 줄(`_ = DB.Find(&vendors)`)은 upstream 줄이므로 그대로 두고, 벤더 이름이 비어 있는 스냅숏은 발행하지 않도록 `publishModelVendorNames`에서 `len(names) == 0 && previous != nil`이면 이전 스냅숏 유지(테스트 추가).

- [ ] **Step 3: 개인 주문 만료 엔드포인트(실패 확인 → 구현)**

`controller/topup_expire_admin_test.go`: 사용자 A의 pending 개인 주문 2건(2시간 전 1, 10분 전 1)과 조직 주문 1건 → 관리자 `POST /api/topup/expire {"user_id": A}` → `{expired:1, skipped_recent:1}`, 조직 주문 불변; 그 뒤 A의 조직 가입이 pending 주문 때문에 막히지 않음(2시간 전 것은 만료, 10분 전 것은 남아 아직 막힘 — 두 단계로 단언). 구현: `model/topup_expire_fork.go`에 `ExpirePendingPersonalTopUps(userId int /*0=all*/) (expired, skippedRecent int64, err error)`(`target_type <> 'organization'`, `status = pending`, `create_time < now-1h`; `ExpirePendingOrganizationTopUps`와 같은 트랜잭션 패턴); 핸들러는 관리자 전용(라우트를 `adminRoute`의 `/topup/complete` 옆에 `// fork: O3`), 감사 `topup.expire`; access-token 범위는 `handlePermissionRoute`가 자동 선언하는지 Phase C Task 10처럼 확인하고 `router/access_token_scope_test.go`·authz 테스트 갱신. OpenAPI/매뉴얼에 추가하고 Phase C 기록의 "운영자가 DB에서 바꿔야 함" 문구를 갱신.

- [ ] **Step 4: 모바일 카드**

`common-log-mobile-card.tsx:203-204`: `log.use_time` 대신 `effectiveUseTimeSec(other, log.use_time)`(이미 import됨)로 tokens/s 계산(`// fork: O7`); `:381` 줄 끝에 `{/* fork: O3 */}`가 JSX 문법상 가능하면 그 형태로, 아니면 바로 윗줄 주석. vitest: `use_time: 0, other.e2e_ms: 2000, completion_tokens: 100` → `50 tok/s` 표시.

- [ ] **Step 5: 검증과 커밋**

Run: `GOWORK=off go vet ./... && GOWORK=off go test ./model/ ./controller/ ./router/ ./service/authz/`; `cd web && bun run test -- usage-logs && bun run typecheck`; 파일별 oxlint/oxfmt. 커밋 3개(README / 스냅숏+만료 / 모바일 카드), 트레일러 동일.

---

### Task 12: 기록·overlay 표·전체 검증·병합 리허설

**Files:**
- Create: `docs/2026-10-15-rebase-phase-d.md`
- Modify: `docs/fork-overlays.md`(O9·O8 절 신설, O3/O6/O7 추가 행), `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §4.8·§9, `docs/i18n/ko-batches.json`(삭제), `web/src/i18n/locales/_reports/ko.untranslated.json`(최종)

- [ ] **Step 1: 전체 검증**

```bash
GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | grep -E "^(FAIL|---)"; cd web && bun run typecheck && bun run test 2>&1 | grep -E "Test Files|Tests " && bun run build 2>&1 | tail -1 && node scripts/ko-locale.mjs check && node scripts/ko-locale.mjs status && bun run i18n:sync && git status --short src/i18n && bun run e2e 2>&1 | tail -3; cd ..
```

Expected: FAIL 없음, 전부 통과, 빌드 성공, `check` 0, placeholder 0, sync가 locale 파일을 바꾸지 않음, e2e 20 통과. 추가로 e2e 1건을 ko로: `web/e2e/korean.e2e.ts`에 "localStorage `i18nextLng=ko`로 로그인 화면을 열면 `로그인` 버튼이 보인다" 수준의 스모크 1개(Task 2의 라벨 확인에 맞춘 셀렉터).

- [ ] **Step 2: 수동 시나리오**

ko 브라우저로: 로그인 → 대시보드 → 토큰 생성 → 사용 로그 상세(할인·타이밍 행 한글) → 지갑 → 조직 화면(정지/조회 문구) → 설정의 "인터페이스 언어"에서 ja 해제 → 언어 메뉴 en/ko만 → 백엔드 오류 1건(조직 거부)이 한국어로 토스트. `Accept-Language: ko`로 `/v1/chat/completions` 잘못된 토큰 → 오류 본문이 `ko.yaml` 문구. 결과 표를 기록 §2에.

- [ ] **Step 3: 병합 리허설**

```bash
git fetch qn && git merge --no-commit --no-ff qn/main 2>&1 | tail -3; git diff --name-only --diff-filter=U; git merge --abort 2>/dev/null || true; git status --short | wc -l
```

충돌 파일 ⊆ `docs/fork-overlays.md`의 upstream 파일 집합인지 확인하고 결과를 기록 §4에(README nav 줄과 `languages.ts`가 충돌 후보).

- [ ] **Step 4: 기록·표·정리**

`docs/2026-10-15-rebase-phase-d.md`: §1 ko locale(키 수: 재사용/번역/skip, 배치별 커밋, 용어 통일 수), §2 백엔드·메시지 경로(전환한 오류 수, 남긴 내부 오류 목록), §3 O8 옵션, §4 이월 4건과 리허설, §5 결정 사항(실행 중 ruling 포함), §6 단계 E로 넘기는 것(다른 locale placeholder 160개, 기존 team 사용자의 `language:'kr'` 데이터 마이그레이션 여부, 조직 로그 가입 시점 제한 등 단계 C §5 잔여), §7 검증. `docs/fork-overlays.md`: O9 절(languages.ts, config.ts, sync-i18n.mjs, date-picker, datetime-picker, i18n.go 4곳, i18n_test.go, constants.go, package.json, README nav 7줄), O8 절(constants/option/misc/status-query/store/use-update-option/registry/index/types/switcher/profile 카드), O3·O6·O7 추가 행; `grep -rn "fork: O" --include='*.go' --include='*.ts' --include='*.tsx' --include='*.mjs' --include='*.md' .`로 줄 번호 검증. spec §4.8을 실제 구조(코드 `ko`, 레거시 `kr` 수용, 옵션 `InterfaceLanguages`)로 고치고 §9에 결정 추가. `docs/i18n/ko-batches.json` 삭제(`ko-skip.json`은 `check`가 쓰므로 유지).

- [ ] **Step 5: 커밋**

```bash
git add docs/ web/e2e web/src/i18n/locales/_reports
git commit -m "docs: record phase D of the upstream rebase (Korean locale, interface languages)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

## 계획 결정 사항

1. 한국어 코드는 `ko`. team의 `kr`은 레거시 값으로만 수용(프런트·백엔드 normalizer). `Intl`에 `kr`을 넘기면 카누리어라 내부 코드로 쓸 수 없다. 기존 team 사용자의 `user.setting.language = 'kr'`는 데이터 마이그레이션 없이 읽기 시점 정규화로 처리한다.
2. 다른 6개 locale의 단계 A~C 영어 placeholder 160개는 번역하지 않는다(한국어가 목표; 비용 대비 효용 낮음). 기록 §6에 남긴다.
3. 번역은 스펙 §9 결정 4대로 기계(LLM) 초안 + 용어집 검수. 배치 6개(각 ≈510키)로 나눠 배치마다 검증 스크립트(보간·태그·공백·한글 포함)와 표본 20개 리뷰를 거친다. 전부 사람 번역으로 바꾸려면 Task 3 전에 알린다.
4. O8 인터페이스 언어 제한은 team의 하드코딩(`[en, kr]`)이 아니라 스펙 §4.8대로 옵션 `InterfaceLanguages`(기본 `en,ko`)로 만든다. 리소스는 7+1개 모두 로드하고 메뉴만 거른다. 이미 다른 언어를 저장한 사용자는 현재 언어를 유지한다.
5. 백엔드 `ko.yaml`은 develop `en.yaml`의 66키 기준이다(team 477키 중 upstream이 `message_key` 방식으로 옮긴 키는 버린다).
6. fork 파일의 `errors.New`는 사용자에게 나가는 것만 `NewMessage`로 바꾼다. upstream 파일의 같은 패턴(`model/topup.go` 등)은 훅 규칙상 손대지 않는다.
7. `README.kr.md`는 team 구판을 그대로 쓰지 않고 upstream 신판 `README.md`를 기준으로 다시 옮긴다(team 문장은 같은 절에서 재사용).
8. 개인 주문 만료는 관리자 엔드포인트 `POST /api/topup/expire`(1시간 age gate)로 두고 조직 주문 만료와 분리한다.
9. 단계 C §5의 조직 로그·대시보드 가입 시점 제한(I5 나머지)은 이 단계에 넣지 않는다(단계 E 이후).
10. 배치 파일 `docs/i18n/ko-batches.json`은 작업 중에만 커밋되고 Task 12에서 삭제한다. `ko-skip.json`은 검증 도구의 입력이라 유지한다.
