import { describe, expect, it } from 'vitest'
import {
  actionFromMenu,
  actionKinds,
  actionMenuKind,
  blankRule,
  conditionFields,
  copyRule,
  normalizeState,
  operatorKeys,
  ruleProblem,
  vacationPhase,
  withField,
  withOperator,
} from './sieve'
import type { SieveCaps, SieveState, SieveVacation } from './types'

const caps: SieveCaps = {
  implementation: '',
  extensions: [],
  vacation: true,
  dates: true,
  fileInto: true,
  flags: true,
  envelope: false,
  include: true,
}

describe('vacationPhase', () => {
  const v: SieveVacation = { enabled: true, start: '2026-12-20', end: '2027-01-06', days: 7, subject: '', from: '', addresses: [], body: 'x' }

  it('follows the saved dates', () => {
    expect(vacationPhase(v, true, '2026-12-01')).toBe('scheduled')
    expect(vacationPhase(v, true, '2026-12-20')).toBe('active')
    expect(vacationPhase(v, true, '2027-01-06')).toBe('active')
    expect(vacationPhase(v, true, '2027-01-07')).toBe('ended')
  })

  it('is off when switched off or never saved, and inactive when the script is not running', () => {
    expect(vacationPhase(null, true, '2026-12-25')).toBe('off')
    expect(vacationPhase({ ...v, enabled: false }, true, '2026-12-25')).toBe('off')
    expect(vacationPhase(v, false, '2026-12-25')).toBe('inactive')
  })

  it('runs without end when no dates are set', () => {
    expect(vacationPhase({ ...v, start: '', end: '' }, true, '2030-01-01')).toBe('active')
  })
})

describe('operators', () => {
  it('offers what each field can do', () => {
    expect(operatorKeys('size')).toEqual(['over', 'under'])
    expect(operatorKeys('header')).toContain('not-exists')
    expect(operatorKeys('from')).not.toContain('exists')
  })

  it('folds the negation into the key and back', () => {
    const c = withOperator(blankRule('r').conditions[0], 'not-contains')
    expect(c).toMatchObject({ op: 'contains', negate: true })
    expect(withOperator(c, 'is')).toMatchObject({ op: 'is', negate: false })
  })

  it('resets an operator the new field cannot take', () => {
    const c = withOperator(blankRule('r').conditions[0], 'not-matches')
    expect(withField(c, 'size')).toMatchObject({ field: 'size', op: 'over', negate: false })
    expect(withField(c, 'subject')).toMatchObject({ field: 'subject', op: 'matches', negate: true })
  })
})

describe('capability gating', () => {
  it('hides fields and actions the server lacks', () => {
    expect(conditionFields(caps)).not.toContain('envfrom')
    expect(conditionFields({ ...caps, envelope: true })).toContain('envfrom')
    expect(actionKinds({ ...caps, fileInto: false, flags: false })).toEqual(['redirect', 'keep', 'discard', 'stop'])
  })

  it('maps flag actions to their menu entries', () => {
    expect(actionMenuKind(actionFromMenu('seen'))).toBe('seen')
    expect(actionFromMenu('flagged')).toEqual({ kind: 'addflag', arg: '\\Flagged' })
  })
})

describe('ruleProblem', () => {
  it('names the first thing missing', () => {
    const rule = blankRule('r')
    expect(ruleProblem(rule)).toBe('sieve.problem.noValue')
    rule.conditions[0].value = 'news@'
    expect(ruleProblem(rule)).toBe('sieve.problem.noFolder')
    rule.actions[0].arg = 'Lists'
    expect(ruleProblem(rule)).toBe('')
    expect(ruleProblem({ ...rule, conditions: [{ field: 'size', header: '', op: 'over', negate: false, value: '5 MB' }] })).toBe('sieve.problem.size')
    expect(ruleProblem({ ...rule, actions: [] })).toBe('sieve.problem.noActions')
  })
})

describe('copyRule', () => {
  it('copies deeply', () => {
    const rule = blankRule('a')
    const copy = copyRule(rule, 'b')
    copy.conditions[0].value = 'changed'
    expect(rule.conditions[0].value).toBe('')
    expect(copy.name).toBe('b')
  })
})

describe('normalizeState', () => {
  it('turns the lists Go sends as null into arrays', () => {
    const raw = {
      caps: { ...caps, extensions: null },
      addresses: null,
      script: { vacation: { enabled: true, addresses: null }, rules: [{ name: 'r', conditions: null, actions: null }], includes: null },
    } as unknown as SieveState
    const s = normalizeState(raw)
    expect(s.addresses).toEqual([])
    expect(s.caps.extensions).toEqual([])
    expect(s.script.vacation?.addresses).toEqual([])
    expect(s.script.rules[0].conditions).toEqual([])
    expect(s.script.includes).toEqual([])
  })
})
