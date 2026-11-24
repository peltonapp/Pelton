import { describe, expect, it } from 'vitest'
import { aiPermission, aiTarget, isLocalAddress, newProviderInput, setAIAccess, usesAI } from './ai'

describe('AI permissions', () => {
  it('names the provider, by where its data goes', () => {
    expect(aiPermission({ id: 'openai', local: false })).toBe('ai:openai')
    expect(aiPermission({ id: 'box', local: true })).toBe('ai_local:box')
  })

  it('reads the provider back out of a permission', () => {
    expect(aiTarget('ai:openai')).toBe('openai')
    expect(aiTarget('ai_local:box')).toBe('box')
    expect(aiTarget('ai')).toBe('')
    expect(aiTarget('net:example.org')).toBe('')
  })

  it('switches access to a provider on and off', () => {
    const on = setAIAccess(['mail.read', 'ai', 'ai_local:box'], { id: 'box', local: false }, true)
    expect(on).toEqual(['mail.read', 'ai', 'ai:box'])
    expect(setAIAccess(on, { id: 'box', local: false }, false)).toEqual(['mail.read', 'ai'])
  })

  it('tells whether a script uses AI', () => {
    expect(usesAI(['ai'])).toBe(true)
    expect(usesAI(['ai_local:x'])).toBe(true)
    expect(usesAI(['mail.read', 'net:ai.example'])).toBe(false)
  })
})

describe('isLocalAddress', () => {
  it.each([
    ['http://localhost:11434', true],
    ['http://127.0.0.1:8000/v1', true],
    ['http://[::1]:8000', true],
    ['http://192.168.1.5', true],
    ['http://172.20.0.1', true],
    ['http://10.1.2.3', true],
    ['https://api.openai.com/v1', false],
    ['http://172.40.0.1', false],
    ['http://gpu-box.lan', false],
    ['not a url', false],
  ])('%s is local: %s', (url, want) => {
    expect(isLocalAddress(url)).toBe(want)
  })
})

describe('newProviderInput', () => {
  it('starts from the kind', () => {
    const input = newProviderInput({ id: 'ollama', label: 'Ollama', baseUrl: 'http://localhost:11434', key: 'optional', model: '' })
    expect(input).toMatchObject({ id: '', name: 'Ollama', kind: 'ollama', baseUrl: 'http://localhost:11434', key: '' })
  })
})
