// assist.ts works out what the Lua editor suggests and explains, from the
// registry catalog alone: the globals and module functions a run has, the
// fields of the trigger's event and of their value types, the script's own
// settings and the user's libraries. Nothing here knows a particular module.

import { entryDescription, needs, permissionDef, type ScriptCatalog, type ScriptField, type ScriptFunctionDef, type ScriptParam } from '../../../lib/scripting'

/** What the editor knows about the script being written. */
export interface LuaContext {
  catalog: ScriptCatalog
  /** The trigger id, whose event fields event. offers. */
  trigger: string
  /** The names in the script's SETTINGS table. */
  settings: string[]
  /** The names of the user's libraries, for require. */
  libraries: string[]
  t: (key: string) => string
}

/** One suggestion: what the list shows and what it puts in. */
export interface Suggestion {
  label: string
  insert: string
  detail: string
  kind: 'module' | 'function' | 'field' | 'global' | 'keyword' | 'setting' | 'library'
}

/** What to complete at the caret: the query starts `back` characters before it. */
export interface Completion {
  back: number
  items: Suggestion[]
}

/** The globals a run has besides the modules; each is explained under scripts.lua.global.<name>. */
export const runGlobals = ['event', 'script', 'stop', 'print', 'require']

const luaGlobals = [
  'assert', 'error', 'ipairs', 'next', 'pairs', 'pcall', 'select', 'setmetatable', 'getmetatable',
  'tonumber', 'tostring', 'type', 'unpack', 'xpcall', 'string', 'table', 'math',
]

const keywords = [
  'and', 'break', 'do', 'else', 'elseif', 'end', 'false', 'for', 'function', 'if', 'in', 'local',
  'nil', 'not', 'or', 'repeat', 'return', 'then', 'true', 'until', 'while',
]

const luaLibraries: Record<string, string[]> = {
  string: ['byte', 'char', 'find', 'format', 'len', 'lower', 'rep', 'reverse', 'sub', 'upper'],
  table: ['concat', 'insert', 'remove', 'sort'],
  math: ['abs', 'ceil', 'floor', 'huge', 'max', 'min', 'pi', 'random', 'sqrt'],
}

const maxItems = 40

/** A parameter as a signature shows it, e.g. "folder: folder?". */
function paramSig(p: ScriptParam): string {
  let type: string = p.type
  if (p.type === 'list' && p.of) {
    type = `list of ${p.of}`
  } else if (p.type === 'ref' && p.valueType) {
    type = p.valueType
  } else if (p.type === 'enum' && p.options) {
    type = p.options.map((o) => o.value).join('|')
  }
  return `${p.name}: ${type}${p.required || p.default !== undefined ? '' : '?'}`
}

function fieldType(f: ScriptField): string {
  return f.of ? `${f.type} of ${f.of}` : f.type
}

/** A module function's call, e.g. "mail.move(message: message?, folder: folder?)", and its results. */
export function signature(module: string, f: ScriptFunctionDef): string {
  let s = `${module}.${f.name}(${f.params.map(paramSig).join(', ')})`
  if (f.returns && f.returns.length > 0) {
    s += ` -> {${f.returns.map((r) => `${r.name}: ${fieldType(r)}`).join(', ')}}`
  }
  return s
}

/** The fields of a value type, or of an event, by name. */
function fieldsOf(ctx: LuaContext, type: string): ScriptField[] {
  return ctx.catalog.valueTypes.find((v) => v.id === type)?.fields ?? []
}

function eventFields(ctx: LuaContext): ScriptField[] {
  return ctx.catalog.triggers.find((tr) => tr.id === ctx.trigger)?.event ?? []
}

/** The fields reached by a path below event, e.g. ["message"] gives a message's fields. */
function fieldsAt(ctx: LuaContext, path: string[]): ScriptField[] {
  let fields = eventFields(ctx)
  for (const seg of path) {
    const f = fields.find((x) => x.name === seg)
    if (!f) {
      return []
    }
    fields = fieldsOf(ctx, f.type === 'list' && f.of ? f.of : f.type)
  }
  return fields
}

function filter(items: Suggestion[], query: string): Suggestion[] {
  const q = query.toLowerCase()
  return items.filter((s) => s.label.toLowerCase().startsWith(q) && s.label !== query).slice(0, maxItems)
}

// inCode is false when the caret sits in a comment or an open string.
function inCode(before: string): boolean {
  let quote = ''
  for (let i = 0; i < before.length; i++) {
    const ch = before[i]
    if (quote) {
      if (ch === '\\') {
        i++
      } else if (ch === quote) {
        quote = ''
      }
    } else if (ch === '"' || ch === "'") {
      quote = ch
    } else if (ch === '-' && before[i + 1] === '-') {
      return false
    }
  }
  return quote === ''
}

/** What to suggest for the text of a line before the caret, or null for nothing. */
export function completionsAt(before: string, ctx: LuaContext): Completion | null {
  const lib = /\brequire\s*\(?\s*["']([a-z0-9_]*)$/.exec(before)
  if (lib) {
    const items = filter(ctx.libraries.map((n) => ({ label: n, insert: n, detail: '', kind: 'library' as const })), lib[1])
    return items.length ? { back: lib[1].length, items } : null
  }
  const setting = /\bsettings\.get\(\s*["']([A-Za-z0-9_]*)$/.exec(before)
  if (setting) {
    const items = filter(ctx.settings.map((n) => ({ label: n, insert: n, detail: '', kind: 'setting' as const })), setting[1])
    return items.length ? { back: setting[1].length, items } : null
  }
  if (!inCode(before)) {
    return null
  }
  const path = /\bevent((?:\.[A-Za-z_][A-Za-z0-9_]*)*)\.([A-Za-z0-9_]*)$/.exec(before)
  if (path) {
    const segs = path[1].split('.').filter(Boolean)
    const items = fieldsAt(ctx, segs).map((f) => ({ label: f.name, insert: f.name, detail: fieldType(f), kind: 'field' as const }))
    const out = filter(items, path[2])
    return out.length ? { back: path[2].length, items: out } : null
  }
  const member = /(?:^|[^.\w])([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z0-9_]*)$/.exec(before)
  if (member) {
    const mod = ctx.catalog.modules.find((m) => m.id === member[1])
    let items: Suggestion[] = []
    if (mod) {
      items = mod.functions.map((f) => ({ label: f.name, insert: `${f.name}(`, detail: signature(mod.id, f), kind: 'function' as const }))
    } else if (luaLibraries[member[1]]) {
      items = luaLibraries[member[1]].map((n) => ({ label: n, insert: n, detail: `${member[1]}.${n}`, kind: 'function' as const }))
    }
    const out = filter(items, member[2])
    return out.length ? { back: member[2].length, items: out } : null
  }
  const word = /(?:^|[^.\w])([A-Za-z_][A-Za-z0-9_]*)$/.exec(before)
  if (word) {
    const items: Suggestion[] = [
      ...ctx.catalog.modules.map((m) => ({ label: m.id, insert: m.id, detail: entryDescription(ctx.t, m), kind: 'module' as const })),
      ...runGlobals.map((name) => ({ label: name, insert: name, detail: ctx.t(`scripts.lua.global.${name}`), kind: 'global' as const })),
      ...luaGlobals.map((name) => ({ label: name, insert: name, detail: '', kind: 'global' as const })),
      ...keywords.map((name) => ({ label: name, insert: name, detail: '', kind: 'keyword' as const })),
    ]
    const out = filter(items, word[1])
    return out.length ? { back: word[1].length, items: out } : null
  }
  return null
}

/** What the docs line shows for the name at the caret. */
export interface LuaDoc {
  title: string
  description: string
  permissions: string[]
  /** The highest risk of those permissions, '' for none. */
  risk: '' | 'low' | 'medium' | 'high'
  /** The call asks the user first. */
  risky: boolean
  /** The call changes something; a test run only plans it. */
  effect: boolean
}

const riskOrder = ['', 'low', 'medium', 'high'] as const

function riskOf(ctx: LuaContext, perms: string[]): LuaDoc['risk'] {
  let risk: LuaDoc['risk'] = ''
  for (const p of perms) {
    const r = permissionDef(ctx.catalog, p)?.risk ?? ''
    if (riskOrder.indexOf(r) > riskOrder.indexOf(risk)) {
      risk = r
    }
  }
  return risk
}

/** The dotted name around column col of a line, e.g. "mail.move". */
function nameAt(line: string, col: number): string {
  let from = col
  let to = col
  while (from > 0 && /[\w.]/.test(line[from - 1])) {
    from--
  }
  while (to < line.length && /[\w]/.test(line[to])) {
    to++
  }
  return line.slice(from, to).replace(/^\.+|\.+$/g, '')
}

/** The function whose call the caret is inside, e.g. "mail.move" in mail.move(m, |). */
function callAround(line: string, col: number): string {
  let depth = 0
  for (let i = col - 1; i >= 0; i--) {
    const ch = line[i]
    if (ch === ')') {
      depth++
    } else if (ch === '(') {
      if (depth === 0) {
        return nameAt(line, i)
      }
      depth--
    }
  }
  return ''
}

/** Explains a dotted name: a module function, a module, an event field or a run global. */
export function docFor(name: string, ctx: LuaContext): LuaDoc | null {
  const parts = name.split('.')
  const mod = ctx.catalog.modules.find((m) => m.id === parts[0])
  if (mod && parts.length === 2) {
    const f = mod.functions.find((x) => x.name === parts[1])
    if (f) {
      const perms = needs(f)
      return {
        title: signature(mod.id, f),
        description: entryDescription(ctx.t, f),
        permissions: perms,
        risk: riskOf(ctx, perms),
        risky: !!f.risky,
        effect: !!f.effect,
      }
    }
  }
  if (mod && parts.length === 1) {
    return { title: mod.id, description: entryDescription(ctx.t, mod), permissions: [], risk: '', risky: false, effect: false }
  }
  if (parts[0] === 'event' && parts.length > 1) {
    const fields = fieldsAt(ctx, parts.slice(1, -1))
    const f = fields.find((x) => x.name === parts[parts.length - 1])
    if (f) {
      return { title: `${name}: ${fieldType(f)}`, description: '', permissions: [], risk: '', risky: false, effect: false }
    }
  }
  if (parts.length === 1 && runGlobals.includes(parts[0])) {
    return { title: parts[0], description: ctx.t(`scripts.lua.global.${parts[0]}`), permissions: [], risk: '', risky: false, effect: false }
  }
  return null
}

/** The docs for the caret at column col of a line: the name under it, or the call it is inside. */
export function docAt(line: string, col: number, ctx: LuaContext): LuaDoc | null {
  return docFor(nameAt(line, col), ctx) ?? docFor(callAround(line, col), ctx)
}

/** The setting names a SETTINGS table declares, read loosely from the source. */
export function settingNames(source: string): string[] {
  const block = /\bSETTINGS\s*=\s*\{([\s\S]*?)\n\}/.exec(source)
  if (!block) {
    return []
  }
  const out: string[] = []
  for (const m of block[1].matchAll(/\bname\s*=\s*["']([A-Za-z_][A-Za-z0-9_]*)["']/g)) {
    if (!out.includes(m[1])) {
      out.push(m[1])
    }
  }
  return out
}

/** The line a Lua syntax error names, e.g. "script line:3(column:5) near ...", or 0. */
export function syntaxErrorLine(message: string): number {
  const m = /line:(\d+)/.exec(message)
  return m ? Number(m[1]) : 0
}
