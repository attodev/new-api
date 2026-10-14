/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { execFileSync } from 'node:child_process'
import { realpathSync } from 'node:fs'
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Korean locale tooling (fork: O9).
//
//   node scripts/ko-locale.mjs seed              build ko.json, batches and the skip list
//   node scripts/ko-locale.mjs check [--batch N] validate ko.json against en.json
//   node scripts/ko-locale.mjs status            count english placeholders still left
//
// Run from the web/ package root (see the `i18n:ko` script in package.json).
// `seed` is deterministic and idempotent: re-running it overwrites ko.json, the
// batch list and the skip list from en.json plus team's legacy kr.json, so any
// translation work must be committed before re-seeding.

const LOCALES = path.resolve('src/i18n/locales')
const EN = path.join(LOCALES, 'en.json')
const KO = path.join(LOCALES, 'ko.json')
const BATCHES = path.resolve('../docs/i18n/ko-batches.json')
const SKIP = path.resolve('../docs/i18n/ko-skip.json')
const TEAM_KR = 'team:web/default/src/i18n/locales/kr.json'
const BATCH_COUNT = 6
const HANGUL = /[가-힣]/
const INTERP = /\{\{\s*[^}]+\s*\}\}/g
const TAG = /<\/?\d+>/g

// en.json stores this key with a unicode escape (see sync-i18n.mjs); keep the
// same on-disk spelling so ko.json matches the other locales.
const OBFUSCATED_KEYS = [
  {
    runtime: ['footer', 'new' + 'api', 'projectAttributionSuffix'].join('.'),
    serialized: 'footer.new\\u0061pi.projectAttributionSuffix',
  },
]

export function validateEntry(en, ko, { skip }) {
  const issues = []
  if (typeof ko !== 'string') return ['value is not a string']
  if (skip) return en === ko ? [] : ['skip key must keep the english value']
  if (ko === en) issues.push('untranslated (equals english)')
  if (!HANGUL.test(ko)) issues.push('no hangul')
  const a = (en.match(INTERP) ?? []).map((s) => s.replaceAll(/\s/g, '')).sort()
  const b = (ko.match(INTERP) ?? []).map((s) => s.replaceAll(/\s/g, '')).sort()
  if (a.join('|') !== b.join('|')) {
    issues.push(`interpolation set differs: ${a} vs ${b}`)
  }
  const ta = (en.match(TAG) ?? []).sort()
  const tb = (ko.match(TAG) ?? []).sort()
  if (ta.join('|') !== tb.join('|')) {
    issues.push(`tag set differs: ${ta} vs ${tb}`)
  }
  if (
    en.startsWith(' ') !== ko.startsWith(' ') ||
    en.endsWith(' ') !== ko.endsWith(' ')
  ) {
    issues.push('whitespace drift')
  }
  if (en.includes('\n') !== ko.includes('\n')) issues.push('newline drift')
  return issues
}

// JSON samples, URLs and bare numbers/units keep the english value. A leading
// `{{` is an interpolation (a sentence), not JSON, so it must be translated.
export function isSkippable(key) {
  return (
    /^[\d\s.,%:/\-+*]+$/.test(key) ||
    (key.startsWith('{') && !key.startsWith('{{')) ||
    key.startsWith('[') ||
    /^https?:\/\//.test(key) ||
    key.length < 2
  )
}

// Brand names and code-like literals: team kept the english value and so did zh.
// Where zh translated it (e.g. Baidu), Korean is expected to as well.
export function isKeptLiteral(en, krValue, zhValue) {
  return typeof krValue === 'string' && krValue === en && zhValue === en
}

async function readJson(p) {
  return JSON.parse(await fs.readFile(p, 'utf8'))
}

async function writeJson(p, v) {
  await fs.mkdir(path.dirname(p), { recursive: true })
  await fs.writeFile(p, `${JSON.stringify(v, null, 2)}\n`)
}

async function writeLocale(translation) {
  let text = JSON.stringify({ translation }, null, 2)
  for (const key of OBFUSCATED_KEYS) {
    text = text.replaceAll(`"${key.runtime}":`, `"${key.serialized}":`)
  }
  await fs.writeFile(KO, `${text}\n`)
}

async function seed() {
  const en = (await readJson(EN)).translation
  const kr = JSON.parse(
    execFileSync('git', ['show', TEAM_KR], {
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
    })
  ).translation
  const zh = (await readJson(path.join(LOCALES, 'zh.json'))).translation
  const ko = {}
  const todo = []
  const skip = []
  for (const key of Object.keys(en)) {
    const v = kr[key]
    if (isSkippable(key) || isKeptLiteral(en[key], v, zh[key])) {
      ko[key] = en[key]
      skip.push(key)
      continue
    }
    if (
      typeof v === 'string' &&
      HANGUL.test(v) &&
      validateEntry(en[key], v, { skip: false }).length === 0
    ) {
      ko[key] = v
    } else {
      ko[key] = en[key]
      todo.push(key)
    }
  }
  await writeLocale(ko)
  await writeJson(SKIP, skip)
  const size = Math.ceil(todo.length / BATCH_COUNT)
  await writeJson(BATCHES, {
    batches: Array.from({ length: BATCH_COUNT }, (_, i) => ({
      id: i + 1,
      keys: todo.slice(i * size, (i + 1) * size),
    })),
  })
  const sizes = Array.from(
    { length: BATCH_COUNT },
    (_, i) => todo.slice(i * size, (i + 1) * size).length
  )
  console.log(
    `seeded: reused ${Object.keys(en).length - todo.length - skip.length}, todo ${todo.length}, skip ${skip.length}`
  )
  console.log(`batch sizes: ${sizes.join(', ')}`)
}

async function check(batchId) {
  const en = (await readJson(EN)).translation
  const ko = (await readJson(KO)).translation
  const skip = new Set(await readJson(SKIP))
  let keys = Object.keys(en)
  if (batchId !== undefined) {
    const batch = (await readJson(BATCHES)).batches.find(
      (b) => b.id === batchId
    )
    if (!batch) {
      console.error(`unknown batch: ${batchId}`)
      process.exit(2)
    }
    keys = batch.keys
  }
  const bad = []
  for (const k of keys) {
    if (!(k in ko)) {
      bad.push([k, ['missing']])
      continue
    }
    const issues = validateEntry(en[k], ko[k], { skip: skip.has(k) })
    if (issues.length) bad.push([k, issues])
  }
  for (const [k, issues] of bad) {
    console.log(`✗ ${JSON.stringify(k)}: ${issues.join('; ')}`)
  }
  console.log(`${keys.length - bad.length}/${keys.length} ok`)
  process.exit(bad.length ? 1 : 0)
}

async function status() {
  const en = (await readJson(EN)).translation
  const ko = (await readJson(KO)).translation
  const skip = new Set(await readJson(SKIP))
  const left = Object.keys(en).filter((k) => !skip.has(k) && ko[k] === en[k])
  console.log(`english placeholders left: ${left.length}`)
}

async function main(argv) {
  const [cmd, flag, arg] = argv
  if (cmd === 'seed') {
    await seed()
  } else if (cmd === 'check') {
    await check(flag === '--batch' ? Number(arg) : undefined)
  } else if (cmd === 'status') {
    await status()
  } else {
    console.log('usage: ko-locale.mjs seed | check [--batch N] | status')
  }
}

function isDirectRun() {
  if (!process.argv[1]) return false
  try {
    return (
      realpathSync(process.argv[1]) ===
      realpathSync(fileURLToPath(import.meta.url))
    )
  } catch {
    return false
  }
}

if (isDirectRun()) await main(process.argv.slice(2))
