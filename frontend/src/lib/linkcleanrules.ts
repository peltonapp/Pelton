// linkcleanrules.ts is the curated list link cleaning works from, kept apart so
// it can be read and extended in one place. Nothing outside it decides what
// counts as tracking: a parameter that is not named here is never removed, and
// a host that is not named here is never unwrapped.

/**
 * Query parameters that only identify a click or a campaign, by exact name in
 * lower case. A parameter is matched ignoring case.
 */
export const trackingParams: ReadonlySet<string> = new Set([
  // google
  'gclid',
  'gclsrc',
  'dclid',
  'gbraid',
  'wbraid',
  '_ga',
  '_gl',
  // meta
  'fbclid',
  'igshid',
  'igsh',
  // microsoft
  'msclkid',
  // mailchimp
  'mc_eid',
  'mc_cid',
  // hubspot
  '_hsenc',
  '_hsmi',
  'hsctatracking',
  // vero
  'vero_id',
  'vero_conv',
  // omeda
  'oly_enc_id',
  'oly_anon_id',
  // yandex
  'yclid',
  'ymclid',
  // x, tiktok, linkedin
  'twclid',
  'ttclid',
  'li_fat_id',
  // marketo, klaviyo
  'mkt_tok',
  '_kx',
])

/** Parameter name prefixes that are tracking whatever follows, in lower case. */
export const trackingPrefixes: readonly string[] = ['utm_']

/**
 * A redirect wrapper: a link that bounces through a tracking or safety host and
 * carries the real destination, as an absolute address, in one parameter.
 * host is matched against the host without a leading www, in lower case.
 */
export interface WrapperRule {
  host: RegExp
  /** The path the wrapper lives on; any path when omitted. */
  path?: RegExp
  /** The parameters that can hold the destination, tried in order. */
  params: readonly string[]
}

/** The wrappers whose destination is plainly inside the address. */
export const wrappers: readonly WrapperRule[] = [
  { host: /^google\.[a-z]{2,3}(\.[a-z]{2})?$/, path: /^\/url$/, params: ['q', 'url'] },
  { host: /^(l|lm)\.facebook\.com$/, path: /^\/l\.php$/, params: ['u'] },
  { host: /^facebook\.com$/, path: /^\/l\.php$/, params: ['u'] },
  { host: /^l\.messenger\.com$/, path: /^\/l\.php$/, params: ['u'] },
  { host: /^l\.instagram\.com$/, params: ['u'] },
  { host: /(^|\.)safelinks\.protection\.outlook\.com$/, params: ['url'] },
  { host: /^linkedin\.com$/, path: /^\/redir\/redirect\/?$/, params: ['url'] },
  { host: /^slack-redir\.net$/, path: /^\/link$/, params: ['url'] },
  { host: /^out\.reddit\.com$/, params: ['url'] },
  { host: /^steamcommunity\.com$/, path: /^\/linkfilter\/?$/, params: ['u', 'url'] },
  { host: /^duckduckgo\.com$/, path: /^\/l\/?$/, params: ['uddg'] },
  { host: /^t\.umblr\.com$/, path: /^\/redirect$/, params: ['z'] },
]

/**
 * Words that mark a link as part of a sign-in, verification, confirmation,
 * unsubscribe, reset or similar flow. A link whose host, path or parameters
 * contain one is left exactly as it is: the whole address can be the token.
 * Matched as a substring, in lower case, so a false match only means a link is
 * left alone.
 */
export const flowWords: RegExp =
  /verif|confirm|activat|unsub|opt-?out|subscri|prefer|reset|recover|passw|magic|log-?in|sign-?in|auth|token|invite|accept|otp|redeem|claim|callback|session|sso|validate|approve|two-?factor/
