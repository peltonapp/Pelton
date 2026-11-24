import { describe, expect, it } from 'vitest'
import { get } from 'svelte/store'
import { matchesQuery, registerSpaceSearch, spaceSearchProviders, type SpaceSearchProvider } from './spacesearch'

const provider = (): SpaceSearchProvider => ({
  placeholderKey: 'x',
  emptyKey: 'y',
  search: () => [],
  open: () => {},
})

describe('registerSpaceSearch', () => {
  it('adds a provider and takes it away again', () => {
    const off = registerSpaceSearch('contacts', provider())
    expect(get(spaceSearchProviders).contacts).toBeDefined()
    off()
    expect(get(spaceSearchProviders).contacts).toBeUndefined()
  })

  it('leaves a newer provider alone when an older one unregisters', () => {
    const offOld = registerSpaceSearch('calendar', provider())
    const newer = provider()
    const offNew = registerSpaceSearch('calendar', newer)
    offOld()
    expect(get(spaceSearchProviders).calendar).toBe(newer)
    offNew()
  })
})

describe('matchesQuery', () => {
  it('wants every word, in any text, ignoring case', () => {
    expect(matchesQuery('ada love', ['Ada Lovelace', 'ada@example.org'])).toBe(true)
    expect(matchesQuery('ada example', ['Ada Lovelace', 'ada@example.org'])).toBe(true)
    expect(matchesQuery('ada babbage', ['Ada Lovelace'])).toBe(false)
  })
})
