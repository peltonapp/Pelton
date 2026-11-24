// re2.ts checks a regular expression the way the backend will run it: Go's
// RE2, which has no backreferences or lookaround. JavaScript's own engine
// parses the rest after RE2-only syntax is mapped to its nearest equivalent.

/** Why a pattern does not work in RE2, as a locale key, or '' when it is fine. */
export function re2Problem(pattern: string): string {
  if (/\\[1-9]|\\k</.test(stripClasses(pattern))) {
    return 'scripts.blocks.regex.backref'
  }
  if (/\(\?<?[=!]/.test(stripClasses(pattern))) {
    return 'scripts.blocks.regex.lookaround'
  }
  try {
    new RegExp(toJS(pattern))
  } catch {
    return 'scripts.blocks.regex.invalid'
  }
  return ''
}

// escaped characters and bracket classes can hold anything; they are
// blanked so the checks above only look at real syntax.
function stripClasses(p: string): string {
  return p.replace(/\\\\/g, '').replace(/\[(?:\\.|[^\]\\])*\]/g, '')
}

function toJS(p: string): string {
  return p
    .replace(/^\(\?[imsU]+\)/, '')
    .replace(/\(\?[imsU]+:/g, '(?:')
    .replace(/\(\?P</g, '(?<')
    .replace(/\\A/g, '^')
    .replace(/\\z/g, '$')
    .replace(/\[\[:[a-z]+:\]\]/g, '\\w')
    .replace(/\[:[a-z]+:\]/g, 'a')
    .replace(/\\[pP]\{?[A-Za-z_]+\}?/g, '\\w')
}
