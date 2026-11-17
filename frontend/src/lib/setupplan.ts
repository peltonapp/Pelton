// setupplan.ts plans the setup flow of the new design: which spaces a purpose
// stands for, which steps follow from them, where a stopped run resumes, what
// each step needs before it can be confirmed and what the summary reports. it
// is pure and imports no stores or components, so the flow can be tested and
// resumed from a stored record alone.

import { hiddenSpaces, serializeHidden } from './spaceorder'
import type { Space } from './types'

/** What the user wants Pelton for, as picked on the first step. */
export type Purpose = 'mail' | 'mailcal' | 'everything' | 'custom'

/** One step of the flow. */
export type SetupStep = 'purpose' | 'language' | 'look' | 'mailbox' | 'dav' | 'notes' | 'feeds' | 'chat' | 'keys' | 'summary'

/** The steps that set something up, and so can be skipped and set up later. */
export type SetupPiece = 'mailbox' | 'dav' | 'notes' | 'feeds' | 'chat'

/** How a step was left: confirmed with something set up, or skipped. */
export type StepOutcome = 'done' | 'skipped'

/** Every space, in the default order of the navigation. */
export const setupSpaces: readonly Space[] = ['mail', 'calendar', 'tasks', 'projects', 'contacts', 'notes', 'feeds', 'chat', 'automations']

/** The purposes in the order the first step offers them. */
export const purposes: readonly Purpose[] = ['mail', 'mailcal', 'everything', 'custom']

const pieces: readonly SetupPiece[] = ['mailbox', 'dav', 'notes', 'feeds', 'chat']

const presets: Record<Exclude<Purpose, 'custom'>, readonly Space[]> = {
  mail: ['mail'],
  mailcal: ['mail', 'calendar', 'tasks', 'contacts'],
  everything: setupSpaces,
}

function canonical(keys: Iterable<Space>): Space[] {
  const set = new Set<Space>(keys)
  set.add('mail')
  return setupSpaces.filter((k) => set.has(k))
}

/** The spaces a purpose stands for, in navigation order. Mail is always one of them. */
export function spacesFor(purpose: Purpose, custom: readonly Space[]): Space[] {
  return purpose === 'custom' ? canonical(custom) : [...presets[purpose]]
}

/**
 * The purpose that matches a set of spaces, for preselecting the first step
 * on a re-run. Chat is left out of the comparison: whether it shows depends on
 * it being switched on, not on the purpose.
 */
export function purposeFor(shown: readonly Space[]): Purpose {
  const want = canonical(shown).filter((k) => k !== 'chat')
  for (const p of ['mail', 'mailcal', 'everything'] as const) {
    const have = presets[p].filter((k) => k !== 'chat')
    if (have.length === want.length && have.every((k, i) => k === want[i])) {
      return p
    }
  }
  return 'custom'
}

/** The spaces the navigation shows, read from the stored hidden list. */
export function shownSpaces(hiddenRaw: string, chatEnabled: boolean): Space[] {
  const hidden = hiddenSpaces(setupSpaces, hiddenRaw)
  return setupSpaces.filter((k) => !hidden.has(k) && (k !== 'chat' || chatEnabled))
}

/**
 * The hidden list to store for a choice. Chat is never hidden by it: chat
 * shows once it is switched on, which only its own step does.
 */
export function hiddenFor(chosen: readonly Space[]): string {
  const keep = new Set(chosen)
  const hidden = new Set(setupSpaces.filter((k) => !keep.has(k) && k !== 'chat'))
  return serializeHidden(setupSpaces, hidden)
}

/** The steps that apply to a choice of spaces, in the order they run. */
export function planSteps(chosen: readonly Space[]): SetupStep[] {
  const has = (k: Space): boolean => chosen.includes(k)
  const steps: SetupStep[] = ['purpose', 'language', 'look', 'mailbox']
  if (has('calendar') || has('contacts') || has('tasks')) {
    steps.push('dav')
  }
  if (has('notes')) {
    steps.push('notes')
  }
  if (has('feeds')) {
    steps.push('feeds')
  }
  if (has('chat')) {
    steps.push('chat')
  }
  steps.push('keys', 'summary')
  return steps
}

/** Whether a step sets something up that the summary reports on. */
export function isPiece(step: SetupStep): step is SetupPiece {
  return (pieces as readonly string[]).includes(step)
}

/** Where a run stands, as stored between launches. */
export interface SetupProgress {
  /** The step on screen. */
  step: SetupStep
  purpose: Purpose
  /** The spaces ticked for the custom purpose. */
  custom: Space[]
  /** How each step left so far was left. */
  outcomes: Partial<Record<SetupStep, StepOutcome>>
  /** Started on purpose from Settings or the palette, not on first launch. */
  rerun: boolean
}

/** What is already set up on this install, read when the flow opens. */
export interface SetupState {
  mailboxes: number
  /** CalDAV logins plus CardDAV address books. */
  davAccounts: number
  /** The notes folder was picked by the user rather than left at the default. */
  vaultChosen: boolean
  feeds: number
  chatSignedIn: boolean
}

/** Nothing set up yet. */
export const emptyState: SetupState = { mailboxes: 0, davAccounts: 0, vaultChosen: false, feeds: 0, chatSignedIn: false }

/**
 * The record a run starts from. A first run preselects everything, which is
 * what the navigation shows before anyone changes it; a re-run starts from
 * what the navigation shows now.
 */
export function startProgress(rerun: boolean, shown: readonly Space[]): SetupProgress {
  const purpose = rerun ? purposeFor(shown) : 'everything'
  const custom = purpose === 'custom' ? canonical(shown) : spacesFor(purpose, [])
  return { step: 'purpose', purpose, custom, outcomes: {}, rerun }
}

const stepNames = new Set<string>(['purpose', 'language', 'look', 'mailbox', 'dav', 'notes', 'feeds', 'chat', 'keys', 'summary'])

/** Reads a stored record, or null when there is none or it is not one. */
export function parseProgress(raw: string): SetupProgress | null {
  if (!raw) {
    return null
  }
  try {
    const p = JSON.parse(raw) as Partial<SetupProgress>
    if (!p || typeof p !== 'object' || !stepNames.has(String(p.step)) || !purposes.includes(p.purpose as Purpose)) {
      return null
    }
    const outcomes: Partial<Record<SetupStep, StepOutcome>> = {}
    for (const [k, v] of Object.entries(p.outcomes ?? {})) {
      if (stepNames.has(k) && (v === 'done' || v === 'skipped')) {
        outcomes[k as SetupStep] = v
      }
    }
    const custom = Array.isArray(p.custom) ? canonical(p.custom.filter((k): k is Space => setupSpaces.includes(k as Space))) : ['mail' as Space]
    return { step: p.step as SetupStep, purpose: p.purpose as Purpose, custom, outcomes, rerun: p.rerun === true }
  } catch {
    return null
  }
}

/** The record as stored. */
export function serializeProgress(p: SetupProgress): string {
  return JSON.stringify(p)
}

/**
 * The step a resumed run opens on: the stored one while it still applies,
 * else the first applicable step not yet left, else the summary.
 */
export function resumeStep(p: SetupProgress, steps: readonly SetupStep[]): SetupStep {
  if (steps.includes(p.step)) {
    return p.step
  }
  return steps.find((s) => !p.outcomes[s]) ?? 'summary'
}

/** Whether a piece already exists on this install. */
export function alreadySet(piece: SetupPiece, state: SetupState): boolean {
  switch (piece) {
    case 'mailbox':
      return state.mailboxes > 0
    case 'dav':
      return state.davAccounts > 0
    case 'notes':
      return state.vaultChosen
    case 'feeds':
      return state.feeds > 0
    case 'chat':
      return state.chatSignedIn
  }
}

/** What the footer's moving-on button says: continue once there is something, skip before. */
export function primaryAction(step: SetupStep, p: SetupProgress, state: SetupState): 'continue' | 'skip' | 'finish' {
  if (step === 'summary') {
    return 'finish'
  }
  if (!isPiece(step)) {
    return 'continue'
  }
  return p.outcomes[step] === 'done' || alreadySet(step, state) ? 'continue' : 'skip'
}

/** Why a step's form cannot be sent yet, as a locale key, or null when it can. */
export type FieldProblem = 'setup.invalid.empty' | 'setup.invalid.address' | 'setup.invalid.spaces' | null

/** Checks a feed address before it is looked up: a web address or a bare host. */
export function checkFeedAddress(raw: string): FieldProblem {
  const value = raw.trim()
  if (!value) {
    return 'setup.invalid.empty'
  }
  if (/\s/.test(value)) {
    return 'setup.invalid.spaces'
  }
  return looksLikeAddress(value) ? null : 'setup.invalid.address'
}

/**
 * Checks a CalDAV or CardDAV server before it is asked. Empty is fine when a
 * mailbox can stand in, since discovery then starts from its domain.
 */
export function checkDavServer(raw: string, hasMailbox: boolean): FieldProblem {
  const value = raw.trim()
  if (!value) {
    return hasMailbox ? null : 'setup.invalid.empty'
  }
  if (/\s/.test(value)) {
    return 'setup.invalid.spaces'
  }
  return looksLikeAddress(value) ? null : 'setup.invalid.address'
}

function looksLikeAddress(value: string): boolean {
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(value) ? value : `https://${value}`
  try {
    const url = new URL(withScheme)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') {
      return false
    }
    return url.hostname === 'localhost' || url.hostname.includes('.') || /^\[.*\]$/.test(url.hostname)
  } catch {
    return false
  }
}

/** One line of the summary. */
export interface SummaryLine {
  piece: SetupPiece
  /** done: set up now or before; skipped: offered and passed over. */
  status: 'done' | 'skipped'
}

/** The pieces of this run and how each ended up, for the summary. */
export function summarize(steps: readonly SetupStep[], p: SetupProgress, state: SetupState): SummaryLine[] {
  return steps.filter(isPiece).map((piece) => ({
    piece,
    status: p.outcomes[piece] === 'done' || alreadySet(piece, state) ? 'done' : 'skipped',
  }))
}

/** Where finishing lands: mail once there is a mailbox, else the first other space chosen. */
export function landingSpace(chosen: readonly Space[], state: SetupState): Space {
  if (state.mailboxes > 0) {
    return 'mail'
  }
  return chosen.find((k) => k !== 'mail' && k !== 'chat') ?? 'mail'
}
