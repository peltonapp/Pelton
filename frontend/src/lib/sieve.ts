// sieve.ts holds the logic behind the out of office and server filter pages:
// fresh rules and conditions, the operator choices per field, a rule's check
// before saving, and where the out of office stands today.

import type { SieveAction, SieveCaps, SieveCondition, SieveRule, SieveState, SieveVacation } from './types'

/** A condition with nothing filled in. */
export function blankCondition(): SieveCondition {
  return { field: 'from', header: '', op: 'contains', negate: false, value: '' }
}

/** A new rule: one empty condition and a move to a folder. */
export function blankRule(name: string): SieveRule {
  return { name, enabled: true, matchAny: false, conditions: [blankCondition()], actions: [{ kind: 'fileinto', arg: '' }] }
}

/** A deep copy of a rule under a new name. */
export function copyRule(rule: SieveRule, name: string): SieveRule {
  return {
    ...rule,
    name,
    conditions: rule.conditions.map((c) => ({ ...c })),
    actions: rule.actions.map((a) => ({ ...a })),
  }
}

/** An empty out of office for an account. */
export function blankVacation(addresses: string[]): SieveVacation {
  return { enabled: false, start: '', end: '', days: 7, subject: '', from: '', addresses: addresses.slice(0, 1), body: '' }
}

/** The condition fields the server can back. */
export function conditionFields(caps: SieveCaps): string[] {
  const out = ['from', 'to', 'subject', 'header', 'size']
  if (caps.envelope) {
    out.push('envfrom', 'envto')
  }
  return out
}

/**
 * The operator choices of a field, as keys that fold the negation in:
 * 'contains', 'not-contains', and so on.
 */
export function operatorKeys(field: string): string[] {
  if (field === 'size') {
    return ['over', 'under']
  }
  const out = ['contains', 'not-contains', 'is', 'not-is', 'matches', 'not-matches']
  if (field === 'header') {
    out.push('exists', 'not-exists')
  }
  return out
}

/** The operator key of a condition. */
export function operatorKey(c: SieveCondition): string {
  return (c.negate ? 'not-' : '') + c.op
}

/** A condition with its field changed, its operator kept when it still applies. */
export function withField(c: SieveCondition, field: string): SieveCondition {
  const keys = operatorKeys(field)
  const next = { ...c, field }
  if (!keys.includes(operatorKey(c))) {
    return withOperator(next, keys[0])
  }
  return next
}

/** A condition with an operator key applied. */
export function withOperator(c: SieveCondition, key: string): SieveCondition {
  const negate = key.startsWith('not-')
  return { ...c, negate, op: negate ? key.slice(4) : key }
}

/** Whether a condition compares against a value. */
export function needsValue(c: SieveCondition): boolean {
  return c.op !== 'exists'
}

/** The action kinds the server can back, in menu order. */
export function actionKinds(caps: SieveCaps): string[] {
  const out: string[] = []
  if (caps.fileInto) {
    out.push('fileinto')
  }
  if (caps.flags) {
    out.push('seen', 'flagged')
  }
  out.push('redirect', 'keep', 'discard', 'stop')
  return out
}

/** The menu kind of an action: addflag splits into marking read and flagging. */
export function actionMenuKind(a: SieveAction): string {
  if (a.kind === 'addflag') {
    return a.arg === '\\Seen' ? 'seen' : 'flagged'
  }
  return a.kind
}

/** The action a menu kind stands for. */
export function actionFromMenu(kind: string, arg = ''): SieveAction {
  switch (kind) {
    case 'seen':
      return { kind: 'addflag', arg: '\\Seen' }
    case 'flagged':
      return { kind: 'addflag', arg: '\\Flagged' }
    case 'fileinto':
    case 'redirect':
      return { kind, arg }
  }
  return { kind, arg: '' }
}

/**
 * What is wrong with a rule before it can be saved, as a localization key, or
 * '' when nothing is.
 */
export function ruleProblem(rule: SieveRule): string {
  if (rule.actions.length === 0) {
    return 'sieve.problem.noActions'
  }
  for (const c of rule.conditions) {
    if (c.field === 'header' && c.header.trim() === '') {
      return 'sieve.problem.noHeader'
    }
    if (c.field === 'size' && !/^\d+[kmgKMG]?$/.test(c.value.trim())) {
      return 'sieve.problem.size'
    }
    if (needsValue(c) && c.value === '') {
      return 'sieve.problem.noValue'
    }
  }
  for (const a of rule.actions) {
    if (a.kind === 'fileinto' && a.arg === '') {
      return 'sieve.problem.noFolder'
    }
    if (a.kind === 'redirect' && !a.arg.includes('@')) {
      return 'sieve.problem.noAddress'
    }
  }
  return ''
}

/** Where an out of office stands on a given day. */
export type VacationPhase = 'off' | 'inactive' | 'scheduled' | 'active' | 'ended'

/**
 * Where the saved out of office stands: off, saved but its script not the
 * active one on the server, waiting for its first day, answering now, or past
 * its last day. `today` is YYYY-MM-DD.
 */
export function vacationPhase(v: SieveVacation | null, scriptActive: boolean, today: string): VacationPhase {
  if (!v || !v.enabled) {
    return 'off'
  }
  if (!scriptActive) {
    return 'inactive'
  }
  if (v.start && today < v.start) {
    return 'scheduled'
  }
  if (v.end && today > v.end) {
    return 'ended'
  }
  return 'active'
}

/** A state with the lists Go sends as null turned into empty arrays. */
export function normalizeState(s: SieveState): SieveState {
  const script = s.script ?? { vacation: null, rules: [], includes: [] }
  return {
    ...s,
    addresses: s.addresses ?? [],
    caps: { ...s.caps, extensions: s.caps?.extensions ?? [] },
    script: {
      vacation: script.vacation ? { ...script.vacation, addresses: script.vacation.addresses ?? [] } : null,
      rules: (script.rules ?? []).map((r) => ({ ...r, conditions: r.conditions ?? [], actions: r.actions ?? [] })),
      includes: script.includes ?? [],
    },
  }
}

/** Whether two values are the same model, compared as JSON. */
export function sameModel(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}
