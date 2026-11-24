// linkclean.ts cleans a link before the reader opens or copies it: tracking
// parameters come off and a redirect wrapper whose destination sits plainly
// inside the address is unwrapped. It is a pure string function. It never
// fetches anything, so a shortener or a wrapper that hides its destination
// behind a lookup is left as it is.
//
// It errs towards leaving a link alone. Only parameters named in
// linkcleanrules.ts are removed, only hosts named there are unwrapped, and a
// link that looks like a sign-in, verification, unsubscribe or reset flow is
// returned untouched, because there the whole address can be the credential.

import { flowWords, trackingParams, trackingPrefixes, wrappers } from './linkcleanrules'

/** The outcome of cleaning one link. */
export interface CleanedLink {
  /** The address to open or copy: the cleaned one, or the original when nothing changed. */
  url: string
  /** Whether anything was removed or unwrapped. */
  changed: boolean
  /** Whether a redirect wrapper was taken off. */
  unwrapped: boolean
  /** The tracking parameters that were removed, by name. */
  removed: string[]
}

interface Parts {
  url: URL
  base: string
  pairs: string[]
  hash: string
}

const maxUnwraps = 3

function safeDecode(text: string): string {
  try {
    return decodeURIComponent(text.replace(/\+/g, ' '))
  } catch {
    return text
  }
}

// split reads an http or https address into the pieces it is rebuilt from. The
// query stays as the raw pairs it came in, so what is kept is not re-encoded.
function split(raw: string): Parts | null {
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    return null
  }
  if ((url.protocol !== 'http:' && url.protocol !== 'https:') || url.hostname === '') {
    return null
  }
  const hashAt = raw.indexOf('#')
  const hash = hashAt >= 0 ? raw.slice(hashAt) : ''
  const rest = hashAt >= 0 ? raw.slice(0, hashAt) : raw
  const queryAt = rest.indexOf('?')
  const base = queryAt >= 0 ? rest.slice(0, queryAt) : rest
  const query = queryAt >= 0 ? rest.slice(queryAt + 1) : ''
  return { url, base, pairs: query === '' ? [] : query.split('&'), hash }
}

function keyOf(pair: string): string {
  return safeDecode(pair.split('=')[0]).toLowerCase()
}

function isTracking(key: string): boolean {
  return trackingParams.has(key) || trackingPrefixes.some((p) => key.startsWith(p))
}

// inFlow reports whether the address looks like a sign-in, verification,
// unsubscribe, reset or similar flow. The tracking parameters themselves are
// left out of the look, so a campaign name cannot make a link look like one.
function inFlow(parts: Parts): boolean {
  const seen = [parts.url.hostname, safeDecode(parts.url.pathname), ...parts.pairs.filter((p) => !isTracking(keyOf(p))).map(safeDecode)]
  return flowWords.test(seen.join(' ').toLowerCase())
}

// destination is the address a wrapper link carries, or null when the link is
// not on a known wrapper or its parameter does not hold an absolute address.
function destination(parts: Parts): string | null {
  const host = parts.url.hostname.toLowerCase().replace(/^www\./, '')
  const rule = wrappers.find((w) => w.host.test(host) && (!w.path || w.path.test(parts.url.pathname)))
  if (!rule) {
    return null
  }
  for (const name of rule.params) {
    const value = parts.url.searchParams.get(name)?.trim() ?? ''
    if (/^https?:\/\//i.test(value) && split(value)) {
      return value
    }
  }
  return null
}

/**
 * Cleans a link. Anything that is not an http or https address, anything that
 * looks like a sign-in or verification flow, and anything with nothing on the
 * list in it comes back unchanged.
 */
export function cleanLink(raw: string): CleanedLink {
  const unchanged: CleanedLink = { url: raw, changed: false, unwrapped: false, removed: [] }
  let current = raw.trim()
  let parts = split(current)
  if (!parts || inFlow(parts)) {
    return unchanged
  }

  let unwrapped = false
  for (let i = 0; i < maxUnwraps; i++) {
    const target = destination(parts)
    const next = target ? split(target) : null
    if (!target || !next || inFlow(next)) {
      break
    }
    current = target
    parts = next
    unwrapped = true
  }

  const removed: string[] = []
  const kept = parts.pairs.filter((pair) => {
    const key = keyOf(pair)
    if (isTracking(key)) {
      removed.push(key)
      return false
    }
    return true
  })
  if (removed.length > 0) {
    current = parts.base + (kept.length > 0 ? `?${kept.join('&')}` : '') + parts.hash
  }

  if (!unwrapped && removed.length === 0) {
    return unchanged
  }
  return { url: current, changed: true, unwrapped, removed }
}

/**
 * The address a link opens or copies as: cleaned when cleaning is on, as it
 * came otherwise.
 */
export function resolveLink(raw: string, clean: boolean): CleanedLink {
  return clean ? cleanLink(raw) : { url: raw, changed: false, unwrapped: false, removed: [] }
}
