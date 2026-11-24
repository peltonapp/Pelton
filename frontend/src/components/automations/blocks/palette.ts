// palette.ts turns the catalog into what the palette and the insert menu
// list: every registered condition and action grouped by category, plus the
// control blocks of the tree format. Nothing here names a condition or
// action, so whatever is registered shows up.

import { entryDescription, entryLabel, type Block, type BlockCond, type BlockTree, type ScriptCatalog } from '../../../lib/scripting'
import { actionBlock, conditionOf, controlBlock, ifWith, type ControlKind, type GroupKind } from './tree'

/** One thing that can be added. */
export interface PaletteItem {
  /** Unique within the palette, e.g. "action:mail.move" or "control:if". */
  key: string
  section: 'conditions' | 'actions' | 'control'
  category: string
  label: string
  description: string
  /** The catalog id of an action or condition. */
  type?: string
  control?: ControlKind
  group?: GroupKind
  permission?: string
  risky?: boolean
  /** Why it cannot be used with this script, or ''. */
  unavailable: string
}

/** A titled run of palette items. */
export interface PaletteGroup {
  key: string
  section: PaletteItem['section']
  label: string
  items: PaletteItem[]
}

const controls: ControlKind[] = ['if', 'foreach', 'set', 'stop', 'lua']
const groups: GroupKind[] = ['all', 'any', 'not']

/** Every item the catalog offers. t resolves locale keys. */
export function paletteItems(catalog: ScriptCatalog, trigger: string, t: (key: string) => string): PaletteItem[] {
  const out: PaletteItem[] = []
  for (const c of catalog.conditions) {
    out.push({
      key: `condition:${c.id}`,
      section: 'conditions',
      category: c.category,
      label: entryLabel(t, c),
      description: entryDescription(t, c),
      type: c.id,
      permission: c.permission,
      unavailable: '',
    })
  }
  for (const g of groups) {
    out.push({
      key: `group:${g}`,
      section: 'conditions',
      category: 'group',
      label: t(`scripts.blocks.group.${g}`),
      description: t(`scripts.blocks.group.${g}.desc`),
      group: g,
      unavailable: '',
    })
  }
  for (const a of catalog.actions) {
    let unavailable = a.missing ? t('scripts.blocks.unavailable.missing') : ''
    if (!unavailable && a.triggers && a.triggers.length > 0 && !a.triggers.includes(trigger)) {
      const names = a.triggers.map((id) => {
        const tr = catalog.triggers.find((x) => x.id === id)
        return tr ? entryLabel(t, tr) : id
      })
      unavailable = t('scripts.blocks.unavailable.trigger').replace('{list}', names.join(', '))
    }
    out.push({
      key: `action:${a.id}`,
      section: 'actions',
      category: a.category,
      label: entryLabel(t, a),
      description: entryDescription(t, a),
      type: a.id,
      permission: a.permission,
      risky: a.risky,
      unavailable,
    })
  }
  for (const c of controls) {
    out.push({
      key: `control:${c}`,
      section: 'control',
      category: 'control',
      label: t(`scripts.blocks.kind.${c}`),
      description: t(`scripts.blocks.kind.${c}.desc`),
      control: c,
      unavailable: '',
    })
  }
  return out
}

/** Whether an item matches a search: every word in its label, description or id. */
export function matches(item: PaletteItem, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  const hay = `${item.label} ${item.description} ${item.type ?? ''} ${item.category}`.toLowerCase()
  return words.every((w) => hay.includes(w))
}

/** The items grouped by section and category, filtered by a search and by what fits: only conditions inside a condition group. */
export function paletteGroups(items: PaletteItem[], query: string, t: (key: string) => string, only?: PaletteItem['section']): PaletteGroup[] {
  const out: PaletteGroup[] = []
  for (const item of items) {
    if ((only && item.section !== only) || !matches(item, query)) {
      continue
    }
    const key = `${item.section}/${item.category}`
    let g = out.find((x) => x.key === key)
    if (!g) {
      g = { key, section: item.section, label: groupLabel(item, t), items: [] }
      out.push(g)
    }
    g.items.push(item)
  }
  return out
}

function groupLabel(item: PaletteItem, t: (key: string) => string): string {
  const section = t(`scripts.blocks.section.${item.section}`)
  if (item.section === 'control') {
    return section
  }
  if (item.category === 'group') {
    return `${section}: ${t('scripts.blocks.section.groups')}`
  }
  const key = `scripts.category.${item.category}`
  const cat = t(key)
  return `${section}: ${cat === key ? item.category : cat}`
}

/** The block an item adds among blocks. A condition or group comes wrapped in an if. */
export function blockFor(item: PaletteItem, tree: BlockTree): Block {
  if (item.section === 'actions' && item.type) {
    return actionBlock(tree, item.type)
  }
  if (item.control) {
    return controlBlock(tree, item.control)
  }
  return ifWith(tree, condFor(item)!)
}

/** The condition an item adds inside a condition group, or null for an action or control block. */
export function condFor(item: PaletteItem): BlockCond | null {
  if (item.group) {
    return { kind: item.group, items: [] }
  }
  if (item.section === 'conditions' && item.type) {
    return conditionOf(item.type)
  }
  return null
}
