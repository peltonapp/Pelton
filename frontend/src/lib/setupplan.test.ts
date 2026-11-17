import { describe, expect, it } from 'vitest'
import {
  checkDavServer,
  checkFeedAddress,
  emptyState,
  hiddenFor,
  landingSpace,
  parseProgress,
  planSteps,
  primaryAction,
  purposeFor,
  resumeStep,
  serializeProgress,
  setupSpaces,
  shownSpaces,
  spacesFor,
  startProgress,
  summarize,
  type SetupProgress,
} from './setupplan'
import { spaces } from './spaces'

describe('spaces of a purpose', () => {
  it('lists the same spaces as the navigation, in its order', () => {
    expect(setupSpaces).toEqual(spaces.map((s) => s.key))
  })

  it('maps the set purposes and always keeps mail', () => {
    expect(spacesFor('mail', [])).toEqual(['mail'])
    expect(spacesFor('mailcal', [])).toEqual(['mail', 'calendar', 'tasks', 'contacts'])
    expect(spacesFor('everything', [])).toEqual(setupSpaces)
    expect(spacesFor('custom', ['feeds', 'calendar'])).toEqual(['mail', 'calendar', 'feeds'])
    expect(spacesFor('custom', [])).toEqual(['mail'])
  })

  it('reads a purpose back from the spaces shown, ignoring chat', () => {
    expect(purposeFor(['mail'])).toBe('mail')
    expect(purposeFor(['contacts', 'mail', 'tasks', 'calendar'])).toBe('mailcal')
    expect(purposeFor(setupSpaces.filter((k) => k !== 'chat'))).toBe('everything')
    expect(purposeFor(setupSpaces)).toBe('everything')
    expect(purposeFor(['mail', 'notes'])).toBe('custom')
  })

  it('hides what was not chosen, but never chat', () => {
    expect(hiddenFor(['mail'])).toBe('calendar,tasks,projects,contacts,notes,feeds,automations')
    expect(hiddenFor(spacesFor('mailcal', []))).toBe('projects,notes,feeds,automations')
    expect(hiddenFor(setupSpaces)).toBe('')
  })

  it('reads the shown spaces from the stored list and the chat switch', () => {
    expect(shownSpaces('projects,notes,feeds,automations', false)).toEqual(['mail', 'calendar', 'tasks', 'contacts'])
    expect(shownSpaces('', true)).toEqual(setupSpaces)
    expect(shownSpaces('mail', false)).toContain('mail')
  })
})

describe('step planner', () => {
  it('shows only the steps for mail', () => {
    expect(planSteps(spacesFor('mail', []))).toEqual(['purpose', 'language', 'look', 'mailbox', 'keys', 'summary'])
  })

  it('adds calendar and contacts for mail and calendar', () => {
    expect(planSteps(spacesFor('mailcal', []))).toEqual(['purpose', 'language', 'look', 'mailbox', 'dav', 'keys', 'summary'])
  })

  it('runs every step for everything, chat included', () => {
    expect(planSteps(spacesFor('everything', []))).toEqual([
      'purpose',
      'language',
      'look',
      'mailbox',
      'dav',
      'notes',
      'feeds',
      'chat',
      'keys',
      'summary',
    ])
  })

  it('follows a custom pick', () => {
    expect(planSteps(spacesFor('custom', ['feeds']))).toEqual(['purpose', 'language', 'look', 'mailbox', 'feeds', 'keys', 'summary'])
    expect(planSteps(spacesFor('custom', ['tasks']))).toContain('dav')
    expect(planSteps(spacesFor('custom', ['projects']))).not.toContain('dav')
  })
})

describe('progress', () => {
  it('starts a first run on everything and a re-run on what is shown', () => {
    expect(startProgress(false, ['mail'])).toMatchObject({ step: 'purpose', purpose: 'everything', rerun: false })
    expect(startProgress(true, ['mail', 'calendar', 'tasks', 'contacts'])).toMatchObject({ purpose: 'mailcal', rerun: true })
    expect(startProgress(true, ['mail', 'notes'])).toMatchObject({ purpose: 'custom', custom: ['mail', 'notes'] })
  })

  it('round-trips through storage and rejects what is not a record', () => {
    const p: SetupProgress = { step: 'dav', purpose: 'custom', custom: ['mail', 'calendar'], outcomes: { mailbox: 'skipped' }, rerun: true }
    expect(parseProgress(serializeProgress(p))).toEqual(p)
    expect(parseProgress('')).toBeNull()
    expect(parseProgress('{')).toBeNull()
    expect(parseProgress('{"step":"nowhere","purpose":"mail"}')).toBeNull()
    expect(parseProgress('{"step":"look","purpose":"mail","outcomes":{"look":"maybe","mailbox":"done"},"custom":["bogus"]}')).toEqual({
      step: 'look',
      purpose: 'mail',
      custom: ['mail'],
      outcomes: { mailbox: 'done' },
      rerun: false,
    })
  })

  it('resumes on the stored step while it still applies', () => {
    const p = startProgress(false, [])
    expect(resumeStep({ ...p, step: 'notes' }, planSteps(setupSpaces))).toBe('notes')
  })

  it('resumes on the first step not left when the stored one no longer applies', () => {
    const p: SetupProgress = { ...startProgress(false, []), step: 'feeds', outcomes: { purpose: 'done', language: 'done' } }
    expect(resumeStep(p, planSteps(['mail']))).toBe('look')
    const all: SetupProgress = { ...p, outcomes: { purpose: 'done', language: 'done', look: 'done', mailbox: 'done', keys: 'done' } }
    expect(resumeStep(all, planSteps(['mail']))).toBe('summary')
  })
})

describe('moving on', () => {
  const p = startProgress(false, [])

  it('offers skip on a piece until something is set up', () => {
    expect(primaryAction('mailbox', p, emptyState)).toBe('skip')
    expect(primaryAction('mailbox', { ...p, outcomes: { mailbox: 'done' } }, emptyState)).toBe('continue')
    expect(primaryAction('look', p, emptyState)).toBe('continue')
    expect(primaryAction('summary', p, emptyState)).toBe('finish')
  })

  it('treats what already exists as set up on a re-run', () => {
    const state = { ...emptyState, mailboxes: 2, davAccounts: 1, feeds: 4, chatSignedIn: true, vaultChosen: true }
    for (const step of ['mailbox', 'dav', 'notes', 'feeds', 'chat'] as const) {
      expect(primaryAction(step, { ...p, rerun: true }, state)).toBe('continue')
    }
  })
})

describe('step validation', () => {
  it('checks a feed address', () => {
    expect(checkFeedAddress('')).toBe('setup.invalid.empty')
    expect(checkFeedAddress('  ')).toBe('setup.invalid.empty')
    expect(checkFeedAddress('two words')).toBe('setup.invalid.spaces')
    expect(checkFeedAddress('nodot')).toBe('setup.invalid.address')
    expect(checkFeedAddress('ftp://example.com/feed')).toBe('setup.invalid.address')
    expect(checkFeedAddress('example.com')).toBeNull()
    expect(checkFeedAddress('https://example.com/feed.xml')).toBeNull()
    expect(checkFeedAddress('http://localhost:8080/rss')).toBeNull()
  })

  it('lets the server be empty only with a mailbox to start from', () => {
    expect(checkDavServer('', true)).toBeNull()
    expect(checkDavServer('', false)).toBe('setup.invalid.empty')
    expect(checkDavServer('cloud.example.com', false)).toBeNull()
    expect(checkDavServer('https://cloud.example.com/remote.php/dav', false)).toBeNull()
    expect(checkDavServer('cloud example', true)).toBe('setup.invalid.spaces')
    expect(checkDavServer('not-a-host', true)).toBe('setup.invalid.address')
  })
})

describe('summary', () => {
  const steps = planSteps(setupSpaces)

  it('lists every piece of the run as set up or skipped', () => {
    const p: SetupProgress = { ...startProgress(false, []), outcomes: { mailbox: 'done', dav: 'skipped', feeds: 'done' } }
    expect(summarize(steps, p, emptyState)).toEqual([
      { piece: 'mailbox', status: 'done' },
      { piece: 'dav', status: 'skipped' },
      { piece: 'notes', status: 'skipped' },
      { piece: 'feeds', status: 'done' },
      { piece: 'chat', status: 'skipped' },
    ])
  })

  it('counts what existed before a re-run as set up', () => {
    const p = startProgress(true, setupSpaces)
    const lines = summarize(planSteps(['mail', 'calendar']), p, { ...emptyState, mailboxes: 1, davAccounts: 2 })
    expect(lines).toEqual([
      { piece: 'mailbox', status: 'done' },
      { piece: 'dav', status: 'done' },
    ])
  })

  it('lands in mail with a mailbox, else in the first other space chosen', () => {
    expect(landingSpace(['mail', 'calendar'], { ...emptyState, mailboxes: 1 })).toBe('mail')
    expect(landingSpace(['mail', 'calendar', 'notes'], emptyState)).toBe('calendar')
    expect(landingSpace(['mail', 'chat'], emptyState)).toBe('mail')
    expect(landingSpace(['mail'], emptyState)).toBe('mail')
  })
})
