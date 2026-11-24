import { get } from 'svelte/store'
import { afterAll, describe, expect, it, vi } from 'vitest'
import { loadStrings, setLocale, t } from './i18n'
import { areaOf, type StringArea } from './stringareas'
import en from './locales/en'
import de from './locales/de'

function translatedIn(area: StringArea): string {
  const key = Object.keys(de).find((k) => areaOf(k) === area && de[k] !== en[k])
  if (!key) {
    throw new Error(`no German string in the ${area} area`)
  }
  return key
}

describe('loadStrings', () => {
  afterAll(() => setLocale('en'))

  it('brings in the active language for an area together with English', async () => {
    const settings = translatedIn('settings')
    const onboarding = translatedIn('onboarding')
    setLocale('de')
    await loadStrings(['settings'])
    expect(get(t)(settings)).toBe(de[settings])
    expect(get(t)(onboarding)).toBe(en[onboarding])
  })

  it('loads every area asked for so far when the language changes', async () => {
    const onboarding = translatedIn('onboarding')
    setLocale('en')
    await loadStrings(['onboarding'])
    expect(get(t)(onboarding)).toBe(en[onboarding])
    setLocale('de')
    await vi.waitFor(() => expect(get(t)(onboarding)).toBe(de[onboarding]))
  })
})
