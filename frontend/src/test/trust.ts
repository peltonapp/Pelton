// trust.ts builds sender passports and shields for tests, with ordinary
// defaults a test overrides where it cares.
import type { Passport, PassportEvidence, Shield } from '../lib/types'

/** A trusted record that verified example.org. */
export const evidence: PassportEvidence = {
  authState: 'trusted',
  servId: 'mx.example.net',
  spf: 'pass',
  spfDomain: 'example.org',
  dkim: 'pass',
  dkimDomains: ['example.org'],
  dmarc: 'pass',
  dmarcPolicy: 'reject',
  headerFrom: 'example.org',
  alignedSpf: true,
  alignedDkim: true,
  sendingDomain: 'example.org',
  returnPath: 'bounce@example.org',
  sender: '',
  signers: ['example.org'],
  receivedSpf: '',
  arc: '',
  mixedScript: false,
}

/** A verified passport of someone the user writes to. */
export function passport(over: Partial<Passport> = {}): Passport {
  return {
    address: 'anna@example.org',
    domain: 'example.org',
    state: 'verified',
    mark: '',
    first: false,
    received: 12,
    sent: 11,
    since: '2024-03-04T10:00:00Z',
    contact: false,
    sameDomain: true,
    changes: [],
    lookalike: null,
    replyGoesTo: 'anna@example.org',
    replyElsewhere: false,
    warnings: [],
    evidence,
    banner: { level: 'none' },
    ...over,
  }
}

/** A shield with nothing found. */
export function shield(over: Partial<Shield> = {}): Shield {
  return { remoteAllowed: false, trackers: 0, links: 0, items: [], unsub: null, ...over }
}
