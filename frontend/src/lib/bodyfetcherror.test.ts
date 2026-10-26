import { describe, expect, it } from 'vitest'
import { isBodyFetchError } from './bodyfetcherror'

describe('isBodyFetchError', () => {
  it('recognises a failed body fetch', () => {
    expect(isBodyFetchError('pelton: this message could not be downloaded from the server: connection reset by peer')).toBe(true)
  })

  it('leaves a missing message and other errors alone', () => {
    expect(isBodyFetchError('storage: message not found')).toBe(false)
    expect(isBodyFetchError('')).toBe(false)
    expect(isBodyFetchError(null)).toBe(false)
  })
})
