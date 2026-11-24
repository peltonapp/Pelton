// passport.ts turns a sender passport and a privacy shield into the words the
// reader shows. The verdicts are worded for what was checked (the sending
// domain), never for the person or the content.

import type { Passport, Shield } from './types'

type Tr = (key: string) => string

/** Puts values into a string's {placeholders}, leaving unknown ones as they are. */
export function fill(text: string, values: Record<string, string | number>): string {
  return text.replace(/\{(\w+)\}/g, (whole, name: string) => (name in values ? String(values[name]) : whole))
}

/** A date as month and year in the reader's locale, '' when it does not parse. */
export function monthYear(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { year: 'numeric', month: 'short' })
}

/** How a passport reads at a glance: good, caution, bad or neutral. */
export function passportTone(p: Passport): 'good' | 'caution' | 'bad' | 'neutral' {
  switch (p.state) {
    case 'verified':
      return 'good'
    case 'failed':
    case 'lookalike':
    case 'impostor':
      return 'bad'
    case 'unverified':
      return 'caution'
  }
  return 'neutral'
}

/** The one line shown next to the sender. */
export function passportLine(p: Passport, tr: Tr): string {
  const domain = p.domain || p.address
  let line: string
  switch (p.state) {
    case 'impostor':
      return tr('passport.impostor')
    case 'failed':
      return fill(tr('passport.failed'), { domain })
    case 'lookalike':
      return p.lookalike?.kind === 'name'
        ? fill(tr('passport.lookalike.name'), { name: p.lookalike.of })
        : fill(tr('passport.lookalike.domain'), { known: p.lookalike?.of ?? '', domain })
    case 'verified':
      if (p.sent > 0 && p.since) {
        line = fill(tr('passport.verified.together'), { domain, n: p.received + p.sent, year: new Date(p.since).getFullYear() })
      } else if (p.received > 1 && p.since) {
        line = fill(tr('passport.verified.received'), { domain, n: p.received, date: monthYear(p.since) })
      } else {
        line = fill(tr('passport.verified.first'), { domain })
      }
      break
    case 'unverified':
      line = fill(tr(`passport.unverified.${p.reason ?? 'no_pass'}`), { domain, sending: p.evidence.sendingDomain })
      break
    default:
      line = tr(`passport.unchecked.${p.reason ?? 'none'}`)
  }
  if (p.mark === 'known') {
    return `${line}. ${tr('passport.knownSuffix')}`
  }
  if (p.first) {
    return `${line}. ${tr('passport.firstSuffix')}`
  }
  return line
}

/** The shield's chip text, or '' when there is nothing to say. */
export function shieldLabel(s: Shield, tr: Tr): string {
  if (s.trackers > 0) {
    return s.trackers === 1 ? tr('shield.trackers.one') : fill(tr('shield.trackers.many'), { n: s.trackers })
  }
  if (s.links > 0) {
    return s.links === 1 ? tr('shield.links.one') : fill(tr('shield.links.many'), { n: s.links })
  }
  return ''
}
