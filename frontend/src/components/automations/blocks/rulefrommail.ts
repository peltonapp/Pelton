// rulefrommail.ts makes a block script from a message: a mail rule started
// when mail arrives, with an if block that matches the message on its first
// field the registry can test. The editor then offers chips to match on the
// other fields instead. Which fields and which conditions come from the
// message value type's condition hints, so nothing here names a condition.

import { get, writable } from 'svelte/store'
import { scriptValue } from '../../../lib/api'
import { t } from '../../../lib/i18n'
import { newInput, serializeTree, withTriggerPermission, type BlockCond, type FieldCondition, type ScriptCatalog } from '../../../lib/scripting'
import { createScript, loadCatalog } from '../../../stores/scripts'
import { openSpace } from '../../../stores/space'
import { errorMessage, toastError } from '../../../stores/toast'
import { ifWith, treeOf } from './tree'

/** The trigger a rule from a message starts with. */
export const ruleTrigger = 'mail.arrived'

/** The value type a rule is made from. */
const ruleValueType = 'message'

/** One thing a rule can match the message on. */
export interface RuleChoice {
  /** The field's path, e.g. "subject". */
  key: string
  /** What the match is called, as a label key with an English fallback. */
  labelKey: string
  label: string
  /** The text matched, shown on the chip. */
  value: string
  cond: BlockCond
}

/** A rule just made from a message, whose chips the editor shows while it is open. */
export interface RuleDraft {
  scriptId: number
  block: string
  choices: RuleChoice[]
}

/** The rule from a message open now, or null. */
export const ruleDraft = writable<RuleDraft | null>(null)

function fieldAt(fields: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((v, seg) => (v && typeof v === 'object' ? (v as Record<string, unknown>)[seg] : undefined), fields)
}

/** The text a hint matches on: the first part of a split field, without a display name around an address. */
export function matchText(raw: unknown, hint: Pick<FieldCondition, 'split'>): string {
  let text = typeof raw === 'string' ? raw.trim() : ''
  if (hint.split) {
    text = (text.split(hint.split)[0] ?? '').trim()
  }
  const angle = text.match(/^[^<]*<([^>]+)>$/)
  return (angle ? angle[1] : text).trim()
}

/** The matches a value offers, in the order its type lists them, for the conditions the catalog has. */
export function ruleChoices(catalog: ScriptCatalog, valueType: string, fields: Record<string, unknown>): RuleChoice[] {
  const out: RuleChoice[] = []
  for (const hint of catalog.valueTypes.find((v) => v.id === valueType)?.conditions ?? []) {
    const value = matchText(fieldAt(fields, hint.field), hint)
    if (!value || !catalog.conditions.some((c) => c.id === hint.condition)) {
      continue
    }
    const args: BlockCond['args'] = { [hint.param]: { value } }
    for (const [name, v] of Object.entries(hint.args ?? {})) {
      args[name] = { value: v }
    }
    out.push({ key: hint.field, labelKey: hint.labelKey, label: hint.label ?? hint.field, value, cond: { kind: 'condition', type: hint.condition, args } })
  }
  return out
}

/** Makes a rule from a message and opens it in the Automations space. */
export async function createRuleFromMail(messageId: number): Promise<void> {
  const say = get(t)
  try {
    const [catalog, fields] = await Promise.all([loadCatalog(), scriptValue(ruleValueType, String(messageId))])
    const choices = ruleChoices(catalog, ruleValueType, fields)
    if (choices.length === 0 || !catalog.triggers.some((tr) => tr.id === ruleTrigger)) {
      toastError(say('scripts.blocks.rule.unavailable'))
      return
    }
    const block = ifWith(treeOf([]), choices[0].cond, 'b1')
    const input = newInput('blocks', say('scripts.blocks.rule.name').replace('{value}', choices[0].value))
    input.blocks = serializeTree(treeOf([block]))
    input.trigger = { type: ruleTrigger, params: {} }
    input.permissions = withTriggerPermission(catalog, ruleTrigger, [])
    const script = await createScript(input)
    ruleDraft.set({ scriptId: script.id, block: block.id, choices })
    openSpace('automations')
  } catch (err) {
    toastError(errorMessage(err))
  }
}
