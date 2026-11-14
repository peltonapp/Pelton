import { describe, expect, it } from 'vitest'
import { classifyConnectionError } from './connectionError'

describe('classifyConnectionError', () => {
  it.each([
    ['dial tcp: lookup imap.nowhere.example: no such host', 'host'],
    ['x509: certificate has expired or is not yet valid', 'tls'],
    ['tls: first record does not look like a TLS handshake', 'tls'],
    ['dial tcp 10.0.0.1:993: connect: connection refused', 'unreachable'],
    ['dial tcp 10.0.0.1:993: i/o timeout', 'unreachable'],
    ['[AUTHENTICATIONFAILED] Invalid credentials (Failure)', 'auth'],
    ['LOGIN failed.', 'auth'],
    ['something else entirely', 'unknown'],
  ])('reads %s as %s', (message, kind) => {
    expect(classifyConnectionError(message)).toBe(kind)
  })
})
