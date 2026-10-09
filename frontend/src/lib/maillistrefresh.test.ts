import { describe, expect, it } from 'vitest'
import { shouldReplaceListOnMailNew } from './maillistrefresh'

describe('shouldReplaceListOnMailNew', () => {
  it('keeps the list during body sync', () => {
    expect(
      shouldReplaceListOnMailNew({ syncPhase: 'bodies', paginated: false }),
    ).toBe(false)
    expect(
      shouldReplaceListOnMailNew({ syncPhase: 'bodies', paginated: true }),
    ).toBe(false)
  })

  it('reloads on stub sync when only the first page is loaded', () => {
    expect(
      shouldReplaceListOnMailNew({ syncPhase: 'stubs', paginated: false }),
    ).toBe(true)
    expect(shouldReplaceListOnMailNew({ syncPhase: '', paginated: false })).toBe(true)
  })

  it('merges instead of replacing once the user has paged ahead', () => {
    expect(
      shouldReplaceListOnMailNew({ syncPhase: 'stubs', paginated: true }),
    ).toBe(false)
    expect(shouldReplaceListOnMailNew({ syncPhase: '', paginated: true })).toBe(false)
  })
})
