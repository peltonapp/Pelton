// rulefrommail.ts makes a block script from a message: a mail rule started
// when mail arrives, with an if block that matches the message's sender. The
// editor then offers chips to match on the subject, the List-Id or a
// recipient instead. Only conditions the catalog has are offered.

import { get, writable } from 'svelte/store'
import { getMessage, getMessageSource } from '../../../lib/api'
import { t } from '../../../lib/i18n'
import { newInput, serializeTree, withTriggerPermission, type BlockCond, type ScriptCatalog } from '../../../lib/scripting'
import { createScript, loadCatalog } from '../../../stores/scripts'
import { openSpace } from '../../../stores/space'
import { errorMessage, toastError } from '../../../stores/toast'
import { ifWith, treeOf } from './tree'

/** The trigger a rule from a message starts with. */
export const ruleTrigger = 'mail.arrived'

/** One thing a rule can match the message on. */
export interface RuleChoice {
  key: 'from' | 'subject' | 'list' | 'to'
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

/** What a message offers to match on. */
export interface RuleSource {
  fromAddress: string
  subject: string
  toAddresses: string
  listId: string
}

const sources: { key: RuleChoice['key']; condition: string; field: keyof RuleSource; header?: string }[] = [
  { key: 'from', condition: 'mail.from', field: 'fromAddress' },
  { key: 'subject', condition: 'mail.subject', field: 'subject' },
  { key: 'list', condition: 'mail.header', field: 'listId', header: 'List-Id' },
  { key: 'to', condition: 'mail.to', field: 'toAddresses' },
]

/** The first plain address in an address list such as "Ann <ann@x.org>, bob@y.org". */
export function firstAddress(list: string): string {
  const first = list.split(',')[0] ?? ''
  const angle = first.match(/<([^>]+)>/)
  return (angle ? angle[1] : first).trim()
}

/** The List-Id of raw message source, without its display name, or ''. */
export function listIdOf(source: string): string {
  const head = source.split(/\r?\n\r?\n/)[0] ?? ''
  const m = head.match(/^list-id:[ \t]*((?:.*)(?:\r?\n[ \t].*)*)/im)
  if (!m) {
    return ''
  }
  const value = m[1].replace(/\r?\n[ \t]+/g, ' ').trim()
  const angle = value.match(/<([^>]+)>/)
  return (angle ? angle[1] : value).trim()
}

/** The matches a message offers, in the order the chips show them. */
export function ruleChoices(catalog: ScriptCatalog, src: RuleSource): RuleChoice[] {
  const out: RuleChoice[] = []
  for (const s of sources) {
    const value = s.key === 'to' ? firstAddress(src.toAddresses) : src[s.field].trim()
    if (!value || !catalog.conditions.some((c) => c.id === s.condition)) {
      continue
    }
    const args: BlockCond['args'] = { value: { value } }
    if (s.header) {
      args.name = { value: s.header }
    }
    out.push({ key: s.key, value, cond: { kind: 'condition', type: s.condition, args } })
  }
  return out
}

/** Makes a rule from a message, opens it in the Automations space, and adds the List-Id choice once the source is read. */
export async function createRuleFromMail(messageId: number): Promise<void> {
  const say = get(t)
  try {
    const [catalog, msg] = await Promise.all([loadCatalog(), getMessage(messageId)])
    const src: RuleSource = { fromAddress: msg.fromAddress, subject: msg.subject, toAddresses: msg.toAddresses ?? '', listId: '' }
    const choices = ruleChoices(catalog, src)
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
    void getMessageSource(messageId)
      .then((source) => {
        const listId = listIdOf(source)
        const draft = get(ruleDraft)
        if (!listId || draft?.scriptId !== script.id) {
          return
        }
        ruleDraft.set({ ...draft, choices: ruleChoices(catalog, { ...src, listId }) })
      })
      .catch(() => undefined)
  } catch (err) {
    toastError(errorMessage(err))
  }
}
