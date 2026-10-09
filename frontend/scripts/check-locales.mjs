// Compares every locale catalog in src/lib/locales against en.ts, which is the
// source of truth, and exits non-zero on a difference.
//
// Nothing else catches this. A key added to en.ts alone builds, type-checks and
// ships; the string then renders as its own key for everyone on another
// language, and only a speaker of that language notices. The same goes for a
// translation that drops or renames a {placeholder}: it type-checks perfectly
// and produces a broken sentence at runtime.
// UI copy never uses an em-dash (AGENTS/no-slop.md); one in any catalog fails too.
//
// The catalogs are parsed rather than imported because CI runs node 20, which
// cannot load TypeScript. They are flat Record<string, string> literals with no
// nesting, computed keys or template literals, so a scan for quoted pairs reads
// them exactly; anything else in the file is ignored, and a catalog that stops
// having that shape shows up here as a key count that collapses.

import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const localesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'lib', 'locales')
const base = 'en'

// pairPattern matches one 'key': 'value' entry. The backreference closes on the
// first unescaped quote of the same kind the entry opened with, so a value
// holding a quote, a colon or a comma is read whole, and either quote style
// works (a value containing an apostrophe is written double-quoted).
const pairPattern = /(['"])((?:[^\\]|\\.)*?)\1\s*:\s*(['"])((?:[^\\]|\\.)*?)\3\s*,/gs

// placeholderPattern matches the {name} tokens a string interpolates.
const placeholderPattern = /\{[a-zA-Z0-9_]+\}/g

/** parseCatalog reads one locale file into a Map of key to value, in file order. */
function parseCatalog(file) {
  const source = readFileSync(join(localesDir, file), 'utf8')
  const entries = new Map()
  const duplicates = []
  for (const match of source.matchAll(pairPattern)) {
    const key = match[2]
    if (entries.has(key)) {
      duplicates.push(key)
      continue
    }
    entries.set(key, match[4])
  }
  return { entries, duplicates }
}

/** placeholders returns the sorted, de-duplicated {tokens} a string uses. */
function placeholders(value) {
  return [...new Set(value.match(placeholderPattern) ?? [])].sort()
}

const files = readdirSync(localesDir).filter((f) => f.endsWith('.ts')).sort()
if (!files.includes(`${base}.ts`)) {
  console.error(`check-locales: no ${base}.ts in ${localesDir}`)
  process.exit(1)
}

const catalogs = new Map(files.map((f) => [f.replace(/\.ts$/, ''), parseCatalog(f)]))
const reference = catalogs.get(base)
const problems = []

if (reference.entries.size === 0) {
  console.error(`check-locales: parsed no strings out of ${base}.ts, so the catalogs changed shape`)
  process.exit(1)
}

for (const [locale, { entries, duplicates }] of catalogs) {
  for (const key of duplicates) {
    problems.push(`${locale}: '${key}' appears twice, so the later one silently wins`)
  }
  for (const [key, value] of entries) {
    if (value.includes('—')) {
      problems.push(`${locale}: '${key}' uses an em-dash; UI copy never does (AGENTS/no-slop.md)`)
    }
  }
  if (locale === base) {
    continue
  }

  for (const key of reference.entries.keys()) {
    if (!entries.has(key)) {
      problems.push(`${locale}: missing '${key}'`)
    }
  }
  for (const key of entries.keys()) {
    if (!reference.entries.has(key)) {
      problems.push(`${locale}: has '${key}', which ${base} does not`)
    }
  }
  for (const [key, value] of entries) {
    const want = reference.entries.get(key)
    if (want === undefined) {
      continue
    }
    const theirs = placeholders(value).join(' ')
    const ours = placeholders(want).join(' ')
    if (theirs !== ours) {
      problems.push(`${locale}: '${key}' uses ${theirs || '(none)'}, ${base} uses ${ours || '(none)'}`)
    }
  }
}

if (problems.length > 0) {
  console.error('The locale catalogs disagree:\n')
  for (const problem of problems) {
    console.error(`  ${problem}`)
  }
  console.error(`\n${problems.length} problem(s). Every user-facing string belongs in all ${files.length} catalogs.`)
  process.exit(1)
}

console.log(`check-locales: ${files.length} catalogs agree on ${reference.entries.size} strings`)
