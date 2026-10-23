// bodyfetcherror.ts recognises the backend's "could not download this message"
// error. Opening a stub fetches its body first, so a server or network hiccup
// can fail the load of a message that still exists: that is a retry, not a
// message gone from the server, and must not mark a reading tab stale.

const bodyFetchFailure = /could not be downloaded from the server/i

/** Reports whether a detail load error is a failed body fetch, not a missing message. */
export function isBodyFetchError(message: string | null | undefined): boolean {
  return !!message && bodyFetchFailure.test(message)
}
