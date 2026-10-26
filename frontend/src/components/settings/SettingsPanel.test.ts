import { describe, expect, it } from 'vitest'

// every catalog, so a translation cannot drift back to the old meaning.
const catalogs = Object.entries(
  import.meta.glob<{ default: Record<string, string> }>('../../lib/locales/*.ts', { eager: true }),
).map(([file, mod]) => [file.replace(/^.*\/|\.ts$/g, ''), mod.default] as const)

const syncKeys = [
  'settingsPanel.hint.syncMessageLimit',
  'settingsPanel.hint.syncMaxParallel',
  'settingsPanel.label.syncFullReconcileDays',
  'settingsPanel.hint.syncFullReconcileDays',
  'mailboxes.syncParallel.useDefaultNamed',
]

describe('sync settings copy', () => {
  it('covers every language', () => {
    expect(catalogs).toHaveLength(11)
  })

  it.each(catalogs)('%s: the message limit hint names the slider option, not a number', (_locale, cat) => {
    const hint = cat['settingsPanel.hint.syncMessageLimit']
    // the slider shows "All", so the hint must not explain it as 0.
    expect(hint).toContain(cat['settingsPanel.syncLimit.all'])
    expect(hint).not.toMatch(/0\s*=/)
  })

  // only the newest messages get list rows; older ones come from the server at
  // the end of the list, so the hint must not promise them in the list already.
  it('the message limit hint does not promise older mail in the list', () => {
    const hint = Object.fromEntries(catalogs)['en']['settingsPanel.hint.syncMessageLimit']
    expect(hint).not.toMatch(/still appear/i)
    expect(hint).toMatch(/end of the list/)
  })

  it.each(catalogs)('%s: sync hints use no internal jargon', (_locale, cat) => {
    for (const key of syncKeys) {
      expect(cat[key], key).not.toMatch(/stub|push|slot/i)
    }
  })

  // when the server forces the count down to 1, new mail waits for the batch
  // in hand, so no language may promise that it never waits.
  const never: Record<string, RegExp> = {
    en: /\bnever\b/i,
    de: /\bnie\b/i,
    fr: /\bjamais\b/i,
    nl: /\bnooit\b/i,
    es: /\bnunca\b/i,
    it: /\bmai\b/i,
    pl: /\bnigdy\b/i,
    tr: /hiçbir zaman|asla/i,
    pt: /\bnunca\b/i,
    ar: /أبد/,
    'zh-CN': /从不|永不/,
  }

  it.each(catalogs)('%s: new mail is not promised to never wait', (locale, cat) => {
    expect(never[locale], locale).toBeDefined()
    expect(cat['settingsPanel.hint.syncMaxParallel']).not.toMatch(never[locale])
  })

  // the user picks 2 to 10 with 3 as the default; the old 1 to 5 range and
  // its N=1 explanation are gone from every language.
  it.each(catalogs)('%s: the connections hint recommends 3 and drops the old range', (_locale, cat) => {
    const hint = cat['settingsPanel.hint.syncMaxParallel']
    expect(hint).toMatch(/(^|\D)3(\D|$)/)
    expect(hint).not.toMatch(/(^|\D)[15](\D|$)/)
    expect(hint).toContain('Pelton')
  })

  // manual Sync re-checks only the folders not checked in the last 24 hours.
  it.each(catalogs)('%s: the full check hint names the 24 hours of manual Sync', (_locale, cat) => {
    expect(cat['settingsPanel.hint.syncFullReconcileDays']).toMatch(/\b24\b/)
  })

  it.each(catalogs)('%s: the sync category has one name', (_locale, cat) => {
    expect(cat['settingsPanel.category.power']).toBe(cat['settingsPanel.category.powerSync'])
  })

  it('keeps English words out of the Arabic sync copy', () => {
    const ar = Object.fromEntries(catalogs)['ar']
    for (const key of syncKeys) {
      // product and protocol names stay as they are, and so do placeholders.
      expect(ar[key].replace(/Pelton|IMAP|\{\w+\}/g, ''), key).not.toMatch(/[A-Za-z]/)
    }
  })
})
