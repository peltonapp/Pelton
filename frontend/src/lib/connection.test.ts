import { describe, expect, it } from 'vitest'
import { connectionSummary } from './connection'

const base = { imapHost: '127.0.0.1', imapPort: 993, local: false }

describe('connectionSummary', () => {
  it('shows the IMAP host and port', () => {
    expect(connectionSummary(base, 'Local')).toBe('IMAP · 127.0.0.1:993')
  })

  it('uses the given label for the local account', () => {
    expect(connectionSummary({ ...base, local: true }, 'Local Folders')).toBe('Local Folders')
  })
})
