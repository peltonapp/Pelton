import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  accountLabel,
  avatarColor,
  displayName,
  formatBytes,
  formatRelative,
  formatStatusClock,
  initials,
  linkifySegments,
} from './format'

describe('formatBytes', () => {
  it('renders whole bytes without a decimal', () => {
    expect(formatBytes(1)).toBe('1 B')
    expect(formatBytes(999)).toBe('999 B')
  })

  it('steps up a unit at a time', () => {
    expect(formatBytes(1024)).toBe('1 KB')
    expect(formatBytes(1024 * 1024)).toBe('1 MB')
    expect(formatBytes(1024 * 1024 * 1024)).toBe('1 GB')
  })

  it('rounds to one decimal above bytes', () => {
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(1024 * 1024 * 2.25)).toBe('2.3 MB')
  })

  it('moves up a unit instead of showing 1024 of the smaller one', () => {
    expect(formatBytes(1024 * 1024 - 1)).toBe('1 MB')
    expect(formatBytes(1024 * 1024 * 1024 - 1)).toBe('1 GB')
    expect(formatBytes(1023)).toBe('1023 B')
  })

  it('stops at GB rather than inventing a larger unit', () => {
    expect(formatBytes(1024 ** 4)).toBe('1024 GB')
  })

  it('treats zero and negatives as no bytes', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-5)).toBe('0 B')
  })
})

describe('formatStatusClock', () => {
  it('shows midnight as 00 on the 24 hour clock', () => {
    const text = formatStatusClock(new Date(2026, 0, 1, 0, 5), '24')
    expect(text).toContain('00:05')
    expect(text).not.toContain('24:05')
  })
})

describe('formatRelative', () => {
  // the key itself stands in for the translation, so a test reads as the
  // wording branch that was taken.
  const t = (key: string): string => key

  afterEach(() => {
    vi.useRealTimers()
  })

  function at(msAgo: number): string {
    vi.useFakeTimers()
    const now = new Date('2026-09-14T12:00:00Z')
    vi.setSystemTime(now)
    return formatRelative(now.getTime() - msAgo, t)
  }

  it('says just now below 45 seconds', () => {
    expect(at(0)).toBe('common.time.justNow')
    expect(at(44_000)).toBe('common.time.justNow')
  })

  it('switches to minutes at 45 seconds', () => {
    expect(at(45_000)).toBe('common.time.minutesAgo')
  })

  // the floor of 45 seconds is zero minutes, which would have read as "0m ago".
  it('never reports zero minutes', () => {
    expect(formatRelative(Date.now() - 45_000, (k) => k.replace('common.time.minutesAgo', '{n}'))).toBe('1')
  })

  it('switches to hours at 60 minutes', () => {
    expect(at(59 * 60_000)).toBe('common.time.minutesAgo')
    expect(at(60 * 60_000)).toBe('common.time.hoursAgo')
  })

  it('falls back to a date at 24 hours', () => {
    expect(at(23 * 3_600_000)).toBe('common.time.hoursAgo')
    expect(at(24 * 3_600_000)).not.toBe('common.time.hoursAgo')
  })

  it('substitutes the count into the wording', () => {
    vi.useFakeTimers()
    const now = new Date('2026-09-14T12:00:00Z')
    vi.setSystemTime(now)
    expect(formatRelative(now.getTime() - 5 * 60_000, () => '{n}m ago')).toBe('5m ago')
    expect(formatRelative(now.getTime() - 3 * 3_600_000, () => '{n}h ago')).toBe('3h ago')
  })
})

describe('initials', () => {
  it('takes the first letter of the first and last name', () => {
    expect(initials('Ada Lovelace', 'ada@example.com')).toBe('AL')
    expect(initials('Ada Byron Lovelace', 'ada@example.com')).toBe('AL')
  })

  it('takes two letters from a single name', () => {
    expect(initials('Ada', 'ada@example.com')).toBe('AD')
  })

  it('falls back to the email local part with no name', () => {
    expect(initials('', 'ada.lovelace@example.com')).toBe('AL')
    expect(initials('   ', 'ada@example.com')).toBe('AD')
  })

  // a From header often arrives as the whole "Name <addr>" string, and the
  // bracketed half must not become an initial.
  it('strips an address out of the display name', () => {
    expect(initials('Ada Lovelace <ada@example.com>', 'ada@example.com')).toBe('AL')
    expect(initials('<ada@example.com>', 'ada@example.com')).toBe('AD')
  })

  it('strips quotes around a name', () => {
    expect(initials('"Ada Lovelace"', 'ada@example.com')).toBe('AL')
  })

  it('splits on dots, underscores and dashes', () => {
    expect(initials('', 'ada_lovelace@example.com')).toBe('AL')
    expect(initials('', 'ada-lovelace@example.com')).toBe('AL')
  })

  it('falls back to a dot when nothing usable is left', () => {
    expect(initials('', '')).toBe('•')
    expect(initials('!!!', '')).toBe('•')
  })

  it('handles non-latin names', () => {
    expect(initials('Иван Петров', 'i@example.com')).toBe('ИП')
  })
})

describe('avatarColor', () => {
  it('gives the same seed the same colour', () => {
    expect(avatarColor('ada@example.com')).toBe(avatarColor('ada@example.com'))
  })

  it('gives different seeds different colours', () => {
    expect(avatarColor('ada@example.com')).not.toBe(avatarColor('bob@example.com'))
  })

  it('stays inside a legal hue', () => {
    for (const seed of ['', 'a', 'ada@example.com', 'x'.repeat(200)]) {
      const hue = Number(avatarColor(seed).match(/hsl\((\d+)/)![1])
      expect(hue).toBeGreaterThanOrEqual(0)
      expect(hue).toBeLessThan(360)
    }
  })
})

describe('displayName', () => {
  it('prefers the name', () => {
    expect(displayName('Ada', 'ada@example.com')).toBe('Ada')
  })

  it('falls back to the address when the name is blank', () => {
    expect(displayName('', 'ada@example.com')).toBe('ada@example.com')
    expect(displayName('   ', 'ada@example.com')).toBe('ada@example.com')
  })
})

describe('accountLabel', () => {
  const base = { email: 'ada@example.com', displayName: 'Ada Lovelace', localLabel: 'Work', useLocalLabel: false }

  it('uses the From name by default', () => {
    expect(accountLabel(base)).toBe('Ada Lovelace')
  })

  it('uses the local label when it is switched on', () => {
    expect(accountLabel({ ...base, useLocalLabel: true })).toBe('Work')
  })

  it('ignores a local label that is switched on but blank', () => {
    expect(accountLabel({ ...base, useLocalLabel: true, localLabel: '  ' })).toBe('Ada Lovelace')
  })

  it('falls back to the address when there is no name at all', () => {
    expect(accountLabel({ ...base, displayName: '' })).toBe('ada@example.com')
  })
})

describe('linkifySegments', () => {
  it('leaves text with no link as one segment', () => {
    expect(linkifySegments('no links here')).toEqual([{ text: 'no links here' }])
  })

  it('splits a url out of surrounding text', () => {
    expect(linkifySegments('see https://example.com now')).toEqual([
      { text: 'see ' },
      { text: 'https://example.com', href: 'https://example.com' },
      { text: ' now' },
    ])
  })

  it('links mailto references', () => {
    expect(linkifySegments('mail me at mailto:ada@example.com')).toEqual([
      { text: 'mail me at ' },
      { text: 'mailto:ada@example.com', href: 'mailto:ada@example.com' },
    ])
  })

  // a url at the end of a sentence would otherwise swallow the full stop and
  // open a link that 404s.
  it('keeps sentence punctuation out of the link', () => {
    expect(linkifySegments('see https://example.com.')).toEqual([
      { text: 'see ' },
      { text: 'https://example.com', href: 'https://example.com' },
      { text: '.' },
    ])
    expect(linkifySegments('(https://example.com)')).toEqual([
      { text: '(' },
      { text: 'https://example.com', href: 'https://example.com' },
      { text: ')' },
    ])
  })

  it('handles several links in one line', () => {
    const segments = linkifySegments('https://a.example and https://b.example')
    expect(segments.filter((s) => s.href).map((s) => s.href)).toEqual(['https://a.example', 'https://b.example'])
  })

  it('does not link other schemes', () => {
    expect(linkifySegments('javascript:alert(1)')).toEqual([{ text: 'javascript:alert(1)' }])
    expect(linkifySegments('ftp://example.com')).toEqual([{ text: 'ftp://example.com' }])
  })

  it('returns nothing for empty text', () => {
    expect(linkifySegments('')).toEqual([])
  })

  // the segments are the whole rendered body, so they must add back up to it.
  it('reassembles into the original text', () => {
    const text = 'hi, see https://example.com. and mailto:a@b.c thanks'
    expect(
      linkifySegments(text)
        .map((s) => s.text)
        .join(''),
    ).toBe(text)
  })
})
