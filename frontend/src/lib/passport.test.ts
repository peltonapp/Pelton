import { describe, expect, it } from 'vitest'
import { get } from 'svelte/store'
import { t } from './i18n'
import { passportLine, passportTone, shieldLabel } from './passport'
import { evidence, passport, shield } from '../test/trust'

const tr = get(t)

describe('passportLine', () => {
  it.each([
    [passport(), 'Verified: really from example.org, and you have written to each other 23 times since 2024'],
    [passport({ sent: 0, received: 5 }), /^Verified: really from example.org, 5 mails since /],
    [passport({ sent: 0, received: 1, first: true }), 'Verified: really from example.org. First mail from this address'],
    [passport({ state: 'unchecked', reason: 'none', first: true }), 'Could not be verified: your mail server did not check this message. First mail from this address'],
    [passport({ state: 'unchecked', reason: 'untrusted' }), 'Could not be verified: the only check in this mail came from another server'],
    [passport({ state: 'unchecked', reason: 'legacy' }), 'Not checked: this mail arrived before Pelton kept this record'],
    [
      passport({ state: 'unverified', reason: 'other_domain', evidence: { ...evidence, sendingDomain: 'esp.test' } }),
      'Not verified: sent through esp.test, not confirmed for example.org',
    ],
    [passport({ state: 'unverified', reason: 'no_pass' }), 'Not verified: nothing confirmed that example.org sent this'],
    [passport({ state: 'failed' }), 'Failed verification: probably not from example.org'],
    [
      passport({ state: 'lookalike', lookalike: { kind: 'name', of: 'Anna Müller', address: 'anna@company.test' } }),
      'Looks like Anna Müller but comes from a different address',
    ],
    [passport({ state: 'lookalike', domain: 'paypa1.com', lookalike: { kind: 'domain', of: 'paypal.com' } }), 'Looks like paypal.com but comes from paypa1.com'],
    [passport({ state: 'impostor', mark: 'impostor' }), 'You marked this sender as not who they claim to be'],
    [passport({ state: 'unchecked', reason: 'none', first: true, mark: 'known' }), 'Could not be verified: your mail server did not check this message. You said you know this sender'],
  ])('%#: words the state for what was checked', (p, want) => {
    const line = passportLine(p, tr)
    if (typeof want === 'string') {
      expect(line).toBe(want)
    } else {
      expect(line).toMatch(want)
    }
  })

  it('never calls the content or the person safe', () => {
    for (const state of ['verified', 'unverified', 'unchecked', 'failed', 'lookalike', 'impostor'] as const) {
      const line = passportLine(passport({ state, lookalike: { kind: 'domain', of: 'x.test' } }), tr)
      expect(line).not.toMatch(/safe|trusted|genuine|legit/i)
    }
  })

  it('reads a tone that does not depend on color alone', () => {
    expect(passportTone(passport())).toBe('good')
    expect(passportTone(passport({ state: 'failed' }))).toBe('bad')
    expect(passportTone(passport({ state: 'unverified' }))).toBe('caution')
    expect(passportTone(passport({ state: 'unchecked' }))).toBe('neutral')
  })
})

describe('shieldLabel', () => {
  it('counts trackers first, then tracked links, and is quiet otherwise', () => {
    expect(shieldLabel(shield({ trackers: 3, links: 2 }), tr)).toBe('3 trackers blocked')
    expect(shieldLabel(shield({ trackers: 1 }), tr)).toBe('1 tracker blocked')
    expect(shieldLabel(shield({ links: 1 }), tr)).toBe('1 tracked link')
    expect(shieldLabel(shield({ links: 4 }), tr)).toBe('4 tracked links')
    expect(shieldLabel(shield({}), tr)).toBe('')
  })
})
