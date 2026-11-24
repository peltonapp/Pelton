// Every key in en.ts belongs to one area of stringareas.ts, and a key may only
// be shown where its area is loaded: core everywhere, any other area inside
// code loaded through withStrings (lazyload.ts) with that area. A key in a
// lazily loaded area that is named by startup code would otherwise render as
// the key itself until something happened to load it.
//
// Where a key is used is read statically: a string literal equal to a key, a
// literal ending in a dot (a prefix completed at runtime), and the text of a
// template literal before its first ${...} all count. Availability follows the
// import graph from main.ts; code reached in more than one way gets only the
// areas every way to it loads.
import { describe, expect, it } from 'vitest'
import en from '../lib/locales/en'
import { areaOf, stringAreas, type StringArea } from '../lib/stringareas'

const raw = import.meta.glob('/src/**/*.{ts,svelte}', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
const sources = Object.fromEntries(
  Object.entries(raw).filter(([file]) => !file.endsWith('.test.ts') && !file.startsWith('/src/test/') && !file.startsWith('/src/lib/locales/')),
)
const keys = Object.keys(en)

function withoutMarkupComments(code: string): string {
  let out = code
  let before = ''
  while (out !== before) {
    before = out
    out = out.replace(/<!--[\s\S]*?-->/g, '')
  }
  return out
}

interface Literal {
  text: string
  prefix: boolean
}

/** literals lists the string literals in code, with the static start of each template literal. */
function literals(code: string): Literal[] {
  const out: Literal[] = []
  let i = 0
  const quoted = (q: string) => {
    let s = ''
    i++
    while (i < code.length && code[i] !== q && code[i] !== '\n') {
      if (code[i] === '\\') {
        s += code[i + 1] ?? ''
        i += 2
        continue
      }
      s += code[i++]
    }
    i++
    out.push({ text: s, prefix: false })
  }
  const template = () => {
    let s = ''
    let whole = true
    i++
    while (i < code.length && code[i] !== '`') {
      if (code[i] === '\\') {
        s += code[i + 1] ?? ''
        i += 2
        continue
      }
      if (code[i] === '$' && code[i + 1] === '{') {
        if (whole) {
          out.push({ text: s, prefix: true })
          whole = false
        }
        i += 2
        expression(true)
        continue
      }
      s += code[i++]
    }
    i++
    if (whole) {
      out.push({ text: s, prefix: false })
    }
  }
  const expression = (nested: boolean) => {
    let depth = 0
    while (i < code.length) {
      const c = code[i]
      if (c === "'" || c === '"') {
        quoted(c)
      } else if (c === '`') {
        template()
      } else if (c === '/' && code[i + 1] === '/') {
        while (i < code.length && code[i] !== '\n') i++
      } else if (c === '/' && code[i + 1] === '*') {
        const end = code.indexOf('*/', i + 2)
        i = end < 0 ? code.length : end + 2
      } else {
        if (nested && c === '{') depth++
        if (nested && c === '}') {
          if (depth === 0) {
            i++
            return
          }
          depth--
        }
        i++
      }
    }
  }
  expression(false)
  return out
}

/** usedKeys lists the keys a source names, by the literals in it. */
function usedKeys(code: string): Set<string> {
  const used = new Set<string>()
  for (const { text, prefix } of literals(withoutMarkupComments(code))) {
    if (!prefix && text in en) {
      used.add(text)
    } else if ((prefix && text.includes('.')) || (!prefix && text.length > 3 && text.endsWith('.'))) {
      for (const key of keys) {
        if (key.startsWith(text)) used.add(key)
      }
    }
  }
  return used
}

function join(dir: string, rel: string): string {
  const parts = dir.split('/')
  for (const part of rel.split('/')) {
    if (part === '..') {
      parts.pop()
    } else if (part !== '.') {
      parts.push(part)
    }
  }
  return parts.join('/')
}

function resolve(from: string, spec: string): string | null {
  const bare = spec.split('?')[0]
  if (!bare.startsWith('.')) {
    return null
  }
  const base = join(from.slice(0, from.lastIndexOf('/')), bare)
  for (const candidate of [base, `${base}.ts`, `${base}/index.ts`]) {
    if (candidate in sources) {
      return candidate
    }
  }
  return null
}

interface Edge {
  to: string
  areas: StringArea[]
}

const importPattern = /(?:^|[\s;])(?:import|export)\s+(?!type\s)(?:[^'"`;]*?\sfrom\s+)?['"]([^'"]+)['"]/g
const withStringsPattern = /withStrings\(\s*\[([^\]]*)\]\s*,\s*\(\)\s*=>\s*import\(\s*['"]([^'"]+)['"]\s*\)\s*\)/g
const dynamicPattern = /import\(\s*['"]([^'"]+)['"]\s*\)/g
const globPattern = /import\.meta\.glob(?:<[^>]*>)?\(\s*['"]([^'"]+)['"]\s*(,[^)]*eager:\s*true)?/g

/** edges lists what a source imports, statically or lazily, and the areas a lazy import loads. */
function edges(file: string): Edge[] {
  const code = withoutMarkupComments(sources[file].replace(/^\s*\/\*[\s\S]*?\*\//gm, '').replace(/^\s*\/\/.*$/gm, ''))
  const script = file.endsWith('.svelte') ? [...code.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script\b[^>]*>/gi)].map((m) => m[1]).join('\n') : code
  const out: Edge[] = []
  const add = (spec: string, areas: StringArea[]) => {
    const to = resolve(file, spec)
    if (to) out.push({ to, areas })
  }
  for (const m of script.matchAll(importPattern)) {
    add(m[1], [])
  }
  for (const m of code.matchAll(withStringsPattern)) {
    add(m[2], [...m[1].matchAll(/['"]([^'"]+)['"]/g)].map((a) => a[1] as StringArea))
  }
  for (const m of code.replace(withStringsPattern, '').matchAll(dynamicPattern)) {
    add(m[1], [])
  }
  for (const m of code.matchAll(globPattern)) {
    const pattern = new RegExp(`^${join(file.slice(0, file.lastIndexOf('/')), m[1]).replace(/[\\^$.+?()[\]{}|]/g, '\\$&').replace(/\*/g, '[^/]*')}$`)
    for (const target of Object.keys(sources)) {
      if (pattern.test(target)) out.push({ to: target, areas: [] })
    }
  }
  return out
}

/** loadedAreas maps every file reached from main.ts to the string areas loaded wherever it runs. */
function loadedAreas(): Map<string, Set<StringArea>> {
  const graph = new Map(Object.keys(sources).map((file) => [file, edges(file)]))
  const loaded = new Map<string, Set<StringArea>>([['/src/main.ts', new Set<StringArea>(['core'])]])
  let changed = true
  while (changed) {
    changed = false
    for (const [file, out] of graph) {
      const here = loaded.get(file)
      if (!here) continue
      for (const { to, areas } of out) {
        const there = new Set([...here, ...areas])
        const known = loaded.get(to)
        if (!known) {
          loaded.set(to, there)
          changed = true
          continue
        }
        for (const area of known) {
          if (!there.has(area)) {
            known.delete(area)
            changed = true
          }
        }
      }
    }
  }
  return loaded
}

describe('string areas', () => {
  it('give every key an area', () => {
    expect(keys.filter((key) => areaOf(key) === null)).toEqual([])
  })

  it('list each prefix once, for keys that exist, apart from the area above it', () => {
    const listed = Object.entries(stringAreas).flatMap(([area, prefixes]) => prefixes.map((prefix) => [prefix, area] as const))
    const problems: string[] = []
    const seen = new Set<string>()
    for (const [prefix, area] of listed) {
      if (seen.has(prefix)) problems.push(`${prefix} is listed twice`)
      seen.add(prefix)
      if (!keys.some((key) => key === prefix || key.startsWith(`${prefix}.`))) problems.push(`${prefix} names no key`)
      const parent = prefix.includes('.') ? areaOf(prefix.slice(0, prefix.lastIndexOf('.'))) : null
      if (parent === area) problems.push(`${prefix} repeats the ${area} area of the prefix above it`)
    }
    expect(problems).toEqual([])
  })

  it('show a key only where its area is loaded', () => {
    const loaded = loadedAreas()
    expect(loaded.get('/src/App.svelte')).toEqual(new Set(['core']))
    const problems: string[] = []
    for (const [file, areas] of loaded) {
      if (file === '/src/lib/stringareas.ts') continue
      for (const key of usedKeys(sources[file])) {
        const area = areaOf(key)
        if (area && !areas.has(area)) {
          problems.push(`${file} shows ${key} from the ${area} strings, but only ${[...areas].join(', ')} are loaded there`)
        }
      }
    }
    expect(problems).toEqual([])
  })

  it('reach the lazily loaded spaces with their strings', () => {
    const loaded = loadedAreas()
    expect(loaded.get('/src/components/calendar/CalendarScreen.svelte')?.has('calendar')).toBe(true)
    expect(loaded.get('/src/components/settings2/pages/General.svelte')?.has('settings')).toBe(true)
  })
})
