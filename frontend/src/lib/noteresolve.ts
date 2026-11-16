// noteresolve.ts turns what a link names into a file of the notes folder,
// with the same rules the backend uses (internal/notes/resolve.go), which are
// Obsidian's: case does not matter, a note's ".md" may be left off, a bare
// name finds the file of that name wherever it is, and when several share it
// the one beside the linking note wins, then the shortest path.

/** A lookup over every file of the notes folder. */
export interface Resolver {
  byKey: Map<string, string[]>
  byPath: Map<string, string>
}

function isNote(path: string): boolean {
  return /\.md$/i.test(path)
}

function dirOf(path: string): string {
  const i = path.lastIndexOf('/')
  return i < 0 ? '.' : path.slice(0, i)
}

function baseOf(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1)
}

function fileKey(path: string): string {
  const lower = path.toLowerCase()
  return isNote(path) ? lower.slice(0, -3) : lower
}

function targetKey(target: string): string {
  let t = target.replace(/\\/g, '/').trim().toLowerCase()
  t = t.replace(/^\/+/, '').replace(/^\.\//, '')
  return t.endsWith('.md') ? t.slice(0, -3) : t
}

/** Normalises a slash path: drops "." segments and folds "..". */
export function cleanPath(path: string): string {
  const out: string[] = []
  for (const seg of path.split('/')) {
    if (seg === '' || seg === '.') {
      continue
    }
    if (seg === '..') {
      if (out.length && out[out.length - 1] !== '..') {
        out.pop()
      } else {
        out.push('..')
      }
      continue
    }
    out.push(seg)
  }
  return out.join('/')
}

/** Builds a resolver over the given vault paths, notes and other files alike. */
export function createResolver(files: readonly string[]): Resolver {
  const sorted = [...files].sort()
  const byKey = new Map<string, string[]>()
  const byPath = new Map<string, string>()
  for (const f of sorted) {
    const k = fileKey(f)
    byPath.set(k, f)
    const base = baseOf(k)
    const list = byKey.get(base)
    if (list) {
      list.push(f)
    } else {
      byKey.set(base, [f])
    }
  }
  return { byKey, byPath }
}

function pick(hits: string[] | undefined, from: string): string {
  if (!hits || hits.length === 0) {
    return ''
  }
  if (hits.length === 1) {
    return hits[0]
  }
  const dir = dirOf(from)
  let best = ''
  for (const h of hits) {
    if (dirOf(h) === dir) {
      return h
    }
    if (!best || h.length < best.length || (h.length === best.length && h < best)) {
      best = h
    }
  }
  return best
}

/** Returns the vault path a wiki link in note from points at, or '' for none. */
export function resolveLink(r: Resolver, target: string, from: string): string {
  const key = targetKey(target)
  if (!key) {
    return from
  }
  if (key.includes('/')) {
    const exact = r.byPath.get(cleanPath(key))
    if (exact) {
      return exact
    }
    const dir = dirOf(from)
    if (dir !== '.') {
      const rel = r.byPath.get(cleanPath(`${dir.toLowerCase()}/${key}`))
      if (rel) {
        return rel
      }
    }
    const hits: string[] = []
    for (const [k, f] of r.byPath) {
      if (k.endsWith(`/${key}`)) {
        hits.push(f)
      }
    }
    return pick(hits, from)
  }
  return pick(r.byKey.get(key), from)
}

/** Resolves a Markdown link, which is relative to the linking note's folder first. */
export function resolveMarkdownLink(r: Resolver, target: string, from: string): string {
  const key = targetKey(target)
  if (!key) {
    return from
  }
  if (!target.trim().startsWith('/')) {
    const dir = dirOf(from)
    const joined = cleanPath(dir === '.' ? key : `${dir.toLowerCase()}/${key}`)
    const hit = r.byPath.get(joined)
    if (hit) {
      return hit
    }
  }
  return resolveLink(r, target, from)
}

/** The shortest way to name a file in a wiki link: its bare name when unique, else its path. */
export function linkText(r: Resolver, path: string): string {
  const base = baseOf(path)
  const name = isNote(path) ? base.slice(0, -3) : base
  const shared = r.byKey.get(baseOf(fileKey(path)))?.length ?? 0
  if (shared <= 1) {
    return name
  }
  return isNote(path) ? path.slice(0, -3) : path
}
