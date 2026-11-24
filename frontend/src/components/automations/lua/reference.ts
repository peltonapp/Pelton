// reference.ts builds the API reference the Lua editor shows beside the code,
// from the registry catalog: the run's globals, the modules and their
// functions, the triggers with their event fields, the value types and the
// permissions. Every entry carries what inserting it puts at the caret.

import { entryDescription, entryLabel, needs, permissionDef, type ScriptCatalog } from '../../../lib/scripting'
import { runGlobals, signature } from './assist'

/** One line of the reference. */
export interface RefEntry {
  /** What it is called in code, e.g. "mail.move". */
  name: string
  /** The signature or type shown in mono. */
  code: string
  description: string
  permissions: string[]
  risk: '' | 'low' | 'medium' | 'high'
  risky: boolean
  /** What inserting it puts at the caret. */
  insert: string
}

/** A titled group of entries, e.g. one module. */
export interface RefSection {
  key: string
  kind: 'globals' | 'module' | 'trigger' | 'type' | 'permissions'
  title: string
  description: string
  entries: RefEntry[]
}

const order = ['', 'low', 'medium', 'high'] as const

function highest(catalog: ScriptCatalog, perms: string[]): RefEntry['risk'] {
  let risk: RefEntry['risk'] = ''
  for (const p of perms) {
    const r = permissionDef(catalog, p)?.risk ?? ''
    if (order.indexOf(r) > order.indexOf(risk)) {
      risk = r
    }
  }
  return risk
}

function plain(name: string, code: string, description: string, insert: string): RefEntry {
  return { name, code, description, permissions: [], risk: '', risky: false, insert }
}

/** Every section of the reference, in the order the panel lists them. */
export function referenceSections(catalog: ScriptCatalog, t: (key: string) => string): RefSection[] {
  const out: RefSection[] = [
    {
      key: 'globals',
      kind: 'globals',
      title: t('scripts.ref.globals'),
      description: '',
      entries: runGlobals.map((g) => plain(g, g, t(`scripts.lua.global.${g}`), g)),
    },
  ]
  for (const m of catalog.modules) {
    out.push({
      key: `module:${m.id}`,
      kind: 'module',
      title: `${m.id} (${entryLabel(t, m)})`,
      description: entryDescription(t, m),
      entries: m.functions.map((f) => {
        const perms = needs(f)
        return {
          name: `${m.id}.${f.name}`,
          code: signature(m.id, f),
          description: entryDescription(t, f),
          permissions: perms,
          risk: highest(catalog, perms),
          risky: !!f.risky,
          insert: `${m.id}.${f.name}(`,
        }
      }),
    })
  }
  for (const tr of catalog.triggers) {
    out.push({
      key: `trigger:${tr.id}`,
      kind: 'trigger',
      title: `${tr.id} (${entryLabel(t, tr)})`,
      description: entryDescription(t, tr),
      entries: tr.event.map((f) => plain(`event.${f.name}`, `event.${f.name}: ${f.of ? `${f.type} of ${f.of}` : f.type}`, entryLabel(t, f), `event.${f.name}`)),
    })
  }
  for (const v of catalog.valueTypes) {
    out.push({
      key: `type:${v.id}`,
      kind: 'type',
      title: `${v.id} (${entryLabel(t, v)})`,
      description: entryDescription(t, v),
      entries: v.fields.map((f) => plain(f.name, `${f.name}: ${f.of ? `${f.type} of ${f.of}` : f.type}`, entryLabel(t, f), f.name)),
    })
  }
  out.push({
    key: 'permissions',
    kind: 'permissions',
    title: t('scripts.ref.permissions'),
    description: t('scripts.ref.permissions.desc'),
    entries: catalog.permissions.map((p) => ({
      name: p.id,
      code: p.id,
      description: `${entryLabel(t, p)}: ${entryDescription(t, p)}`,
      permissions: [],
      risk: p.risk,
      risky: false,
      insert: `"${p.id}"`,
    })),
  })
  return out
}

/** The sections with only the entries matching a search, by name, code or description. */
export function searchReference(sections: RefSection[], query: string): RefSection[] {
  const q = query.trim().toLowerCase()
  if (!q) {
    return sections
  }
  const out: RefSection[] = []
  for (const s of sections) {
    const titleHit = s.title.toLowerCase().includes(q)
    const entries = titleHit ? s.entries : s.entries.filter((e) => `${e.name} ${e.code} ${e.description}`.toLowerCase().includes(q))
    if (entries.length > 0) {
      out.push({ ...s, entries })
    }
  }
  return out
}
