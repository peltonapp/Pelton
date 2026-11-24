import { describe, expect, it, vi } from 'vitest'
import { cleanLink, resolveLink } from './linkclean'

const url = (raw: string) => cleanLink(raw).url
const untouched = (raw: string) => {
  const out = cleanLink(raw)
  expect(out).toEqual({ url: raw, changed: false, unwrapped: false, removed: [] })
}

describe('tracking parameters', () => {
  it('removes every utm_ parameter and keeps the rest in order', () => {
    expect(url('https://shop.example.com/p?id=7&utm_source=news&utm_medium=email&color=red&utm_campaign=x')).toBe(
      'https://shop.example.com/p?id=7&color=red',
    )
  })

  it('drops the question mark when nothing is left', () => {
    expect(url('https://example.com/page?utm_source=a&fbclid=b')).toBe('https://example.com/page')
    expect(url('https://example.com/?gclid=1')).toBe('https://example.com/')
  })

  it.each([
    'fbclid',
    'gclid',
    'dclid',
    'msclkid',
    'mc_eid',
    'mc_cid',
    'igshid',
    '_hsenc',
    '_hsmi',
    'vero_id',
    'oly_enc_id',
    'oly_anon_id',
    'yclid',
    'twclid',
  ])('removes %s', (name) => {
    expect(url(`https://example.com/a?keep=1&${name}=abc123`)).toBe('https://example.com/a?keep=1')
  })

  it('matches parameter names ignoring case', () => {
    expect(url('https://example.com/a?UTM_Source=x&FbClid=y&n=1')).toBe('https://example.com/a?n=1')
  })

  it('reports what it removed', () => {
    expect(cleanLink('https://example.com/a?utm_source=x&id=1&gclid=2').removed).toEqual(['utm_source', 'gclid'])
  })

  it('keeps the fragment and does not re-encode what stays', () => {
    expect(url('https://example.com/a?q=a%20b+c&utm_term=t#section-2')).toBe('https://example.com/a?q=a%20b+c#section-2')
    expect(url('https://example.com/a?utm_term=t#frag?utm_source=x')).toBe('https://example.com/a#frag?utm_source=x')
  })

  it('removes a repeated parameter every time it appears', () => {
    expect(url('https://example.com/?utm_source=a&x=1&utm_source=b')).toBe('https://example.com/?x=1')
  })

  it('removes a parameter that has no value', () => {
    expect(url('https://example.com/?utm_source&x=1')).toBe('https://example.com/?x=1')
  })

  it('never touches a parameter that is not on the list', () => {
    untouched('https://example.com/a?ref=newsletter&source=email&campaign=spring&id=9')
    untouched('https://example.com/a?utmx=1&xutm_source=2&gclidx=3')
  })

  it('leaves a link with nothing to clean exactly as it came', () => {
    untouched('https://example.com/a/b?x=1#top')
    untouched('http://example.com')
  })
})

describe('redirect wrappers', () => {
  it('unwraps a google redirect', () => {
    expect(url('https://www.google.com/url?q=https%3A%2F%2Fexample.org%2Fpost&sa=D&usg=AOvVaw0')).toBe('https://example.org/post')
    expect(url('https://www.google.de/url?sa=t&url=https%3A%2F%2Fexample.org%2F')).toBe('https://example.org/')
  })

  it('unwraps facebook, outlook safelinks, linkedin, reddit and duckduckgo redirects', () => {
    expect(url('https://l.facebook.com/l.php?u=https%3A%2F%2Fexample.org%2Fa&h=AT0')).toBe('https://example.org/a')
    expect(url('https://eur02.safelinks.protection.outlook.com/?url=https%3A%2F%2Fexample.org%2Fa&data=05')).toBe('https://example.org/a')
    expect(url('https://www.linkedin.com/redir/redirect?url=https%3A%2F%2Fexample.org%2Fa&urlhash=ab')).toBe('https://example.org/a')
    expect(url('https://out.reddit.com/t3_abc?url=https%3A%2F%2Fexample.org%2Fa')).toBe('https://example.org/a')
    expect(url('https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org%2Fa&rut=9')).toBe('https://example.org/a')
  })

  it('leaves a wrapper that carries a token of its own', () => {
    untouched('https://www.youtube.com/redirect?q=https%3A%2F%2Fexample.org%2Fa&redir_token=x')
  })

  it('also strips tracking from the destination', () => {
    const out = cleanLink('https://l.facebook.com/l.php?u=https%3A%2F%2Fexample.org%2Fa%3Fid%3D1%26utm_source%3Dfb%26fbclid%3Dzz')
    expect(out).toEqual({ url: 'https://example.org/a?id=1', changed: true, unwrapped: true, removed: ['utm_source', 'fbclid'] })
  })

  it('unwraps a wrapper inside a wrapper', () => {
    const inner = encodeURIComponent('https://example.org/a')
    const outer = encodeURIComponent(`https://l.facebook.com/l.php?u=${inner}`)
    expect(url(`https://www.google.com/url?q=${outer}`)).toBe('https://example.org/a')
  })

  it('leaves a wrapper whose parameter is not an absolute http address', () => {
    untouched('https://www.google.com/url?q=%2Fsearch%3Fq%3Dx')
    untouched('https://www.google.com/url?q=javascript%3Aalert(1)')
    untouched('https://www.google.com/url?q=ftp%3A%2F%2Fexample.org%2Ff')
    untouched('https://www.google.com/url?sa=D')
  })

  it('does not unwrap a host that is not on the list', () => {
    untouched('https://tracker.example.net/click?url=https%3A%2F%2Fexample.org%2Fa')
    untouched('https://example.net/?u=https%3A%2F%2Fexample.org%2Fa&q=https%3A%2F%2Fexample.org')
  })

  it('does not unwrap a known host on another path', () => {
    untouched('https://www.google.com/search?q=https%3A%2F%2Fexample.org%2Fa')
    untouched('https://www.youtube.com/watch?q=https%3A%2F%2Fexample.org%2Fa')
  })

  it('reads the parameter a rule names and no other', () => {
    untouched('https://l.facebook.com/l.php?q=https%3A%2F%2Fexample.org%2Fa')
  })

  it('does not unwrap to a sign-in or verification destination', () => {
    untouched(`https://www.google.com/url?q=${encodeURIComponent('https://example.org/account/verify?t=abc')}`)
  })
})

describe('sign-in, verification and unsubscribe links', () => {
  it('leaves the owner example untouched', () => {
    untouched('https://verify.example.com?c=fhue389ew')
    untouched('https://verify.example.com/?c=fhue389ew&utm_source=mail')
  })

  it.each([
    'https://example.com/confirm?id=1&utm_source=mail',
    'https://example.com/unsubscribe?u=42&utm_medium=email',
    'https://example.com/account/reset-password/ab12?utm_source=x',
    'https://example.com/magic-link?code=zz&utm_campaign=x',
    'https://example.com/verification/ab12?fbclid=1',
    'https://example.com/login?next=%2Finbox&utm_source=x',
    'https://auth.example.com/cb?state=1&gclid=2',
    'https://example.com/a?token=abc&utm_source=x',
    'https://example.com/a?action=unsubscribe&utm_source=x',
    'https://example.com/activate/ab12?utm_source=x',
    'https://example.com/accept-invite?id=1&utm_source=x',
    'https://example.com/sso?utm_source=x',
    'https://example.com/otp?utm_source=x',
    'https://example.com/optout?utm_source=x',
    'https://example.com/preferences?utm_source=x',
  ])('leaves %s alone', (raw) => {
    untouched(raw)
  })

  it('does not let a campaign name make a link look like a flow', () => {
    expect(url('https://example.com/blog/post?utm_campaign=password-manager-promo')).toBe('https://example.com/blog/post')
  })
})

describe('not a web link', () => {
  it.each([
    'mailto:a@example.com?utm_source=x',
    'javascript:alert(1)',
    'ftp://example.com/?utm_source=x',
    'tel:+4912345',
    '/relative/path?utm_source=x',
    'not a link',
    '',
  ])('returns %j unchanged', (raw) => {
    untouched(raw)
  })

  it('does not crash on malformed escapes', () => {
    expect(url('https://example.com/a?x=%E0%A4%A&utm_source=1')).toBe('https://example.com/a?x=%E0%A4%A')
  })
})

describe('purity', () => {
  it('never reaches for the network', () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)
    cleanLink('https://www.google.com/url?q=https%3A%2F%2Fexample.org%2F')
    cleanLink('https://t.co/abc123')
    cleanLink('https://bit.ly/abc')
    vi.unstubAllGlobals()
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('leaves shorteners and opaque redirectors as they are', () => {
    untouched('https://t.co/AbCdEf')
    untouched('https://bit.ly/3xyz')
    untouched('https://click.mailer.example.net/ls/click?upn=u001.abcd')
  })

  it('resolveLink only cleans when asked to', () => {
    const raw = 'https://example.com/a?utm_source=x'
    expect(resolveLink(raw, false)).toEqual({ url: raw, changed: false, unwrapped: false, removed: [] })
    expect(resolveLink(raw, true).url).toBe('https://example.com/a')
  })
})
