import { describe, expect, it } from 'vitest'
import { isEmailAddress, presetForEmail } from './providers'

describe('presetForEmail', () => {
  it.each([
    ['me@gmail.com', 'gmail'],
    ['Me@GoogleMail.com', 'gmail'],
    ['me@hotmail.co.uk', 'outlook'],
    ['me@icloud.com', 'icloud'],
    ['me@ymail.com', 'yahoo'],
    ['me@fastmail.com', 'fastmail'],
    ['me@purelymail.com', 'purelymail'],
  ])('finds the preset for %s', (email, id) => {
    expect(presetForEmail(email)?.id).toBe(id)
  })

  it('finds nothing for a domain no preset hosts', () => {
    expect(presetForEmail('me@uni.example')).toBeNull()
    expect(presetForEmail('not an address')).toBeNull()
  })
})

describe('isEmailAddress', () => {
  it('wants a local part, an @ and a dotted domain', () => {
    expect(isEmailAddress('me@example.com')).toBe(true)
    expect(isEmailAddress('me@example')).toBe(false)
    expect(isEmailAddress('me@example.c')).toBe(false)
    expect(isEmailAddress('@example.com')).toBe(false)
    expect(isEmailAddress('me example@x.com')).toBe(false)
  })
})
