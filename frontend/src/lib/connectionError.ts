// connectionError.ts sorts the error a connection test came back with into the
// few kinds of problem the add-mailbox wizard has a sentence and a fix for. The
// raw message still travels alongside for whoever wants the server's words.

/** A kind of failed connection test, each with its own explanation and fix. */
export type ConnectionProblem = 'auth' | 'host' | 'unreachable' | 'tls' | 'unknown'

const patterns: [ConnectionProblem, RegExp][] = [
  ['host', /no such host|server misbehaving|name resolution|could not resolve|nodename nor servname/i],
  ['tls', /x509|certificate|tls|handshake|ssl|first record does not look/i],
  ['unreachable', /connection refused|timed? ?out|timeout|unreachable|no route to host|connection reset|eof|deadline exceeded|proxy/i],
  ['auth', /auth|login|logon|credential|password|invalid user|username|not permitted|access denied|application-specific/i],
]

/** classifyConnectionError names the kind of problem an error message describes. */
export function classifyConnectionError(message: string): ConnectionProblem {
  for (const [kind, pattern] of patterns) {
    if (pattern.test(message)) {
      return kind
    }
  }
  return 'unknown'
}
