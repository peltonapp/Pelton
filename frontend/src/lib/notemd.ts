// notemd.ts reads the Markdown of a note the way the notes space needs it:
// which lines are front matter, code, headings, lists, quotes, callouts and
// tables, and where the inline marks, links and tags sit in a line. It knows
// nothing about the editor, so the live preview, the outline and the tests
// all share one reading. Offsets are string indices into the line or text.

/** A property of the front matter: a value, or the items of a list. */
export interface NoteProperty {
  key: string
  value: string
  list: string[] | null
}

/** Where the front matter is and what it holds; end is the offset after its closing line. */
export interface FrontMatter {
  /** Index of the closing line. */
  lastLine: number
  end: number
  props: NoteProperty[]
}

const bom = String.fromCharCode(0xfeff)

/** Reads the YAML block at the top of a note, or returns null when there is none. */
export function parseFrontMatter(text: string): FrontMatter | null {
  const start = text.startsWith(bom) ? 1 : 0
  const lines = text.slice(start).split('\n')
  if (lines.length < 2 || lines[0].replace(/[ \t\r]+$/, '') !== '---') {
    return null
  }
  let offset = start + lines[0].length + 1
  const body: string[] = []
  for (let i = 1; i < lines.length; i++) {
    const trimmed = lines[i].replace(/[ \t\r]+$/, '')
    if (trimmed === '---' || trimmed === '...') {
      const end = offset + lines[i].length + (i < lines.length - 1 ? 1 : 0)
      return { lastLine: i, end, props: parseYaml(body) }
    }
    body.push(lines[i].replace(/\r$/, ''))
    offset += lines[i].length + 1
  }
  return null
}

function unquote(s: string): string {
  if (s.length >= 2 && ((s[0] === '"' && s.endsWith('"')) || (s[0] === "'" && s.endsWith("'")))) {
    return s.slice(1, -1)
  }
  return s
}

function parseYaml(lines: string[]): NoteProperty[] {
  const out: NoteProperty[] = []
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i]
    if (!l.trim() || l.trim().startsWith('#') || /^[ \t-]/.test(l)) {
      continue
    }
    const colon = l.indexOf(':')
    if (colon < 0) {
      continue
    }
    const key = l.slice(0, colon).trim()
    const value = l.slice(colon + 1).trim()
    if (value.startsWith('[') && value.endsWith(']')) {
      out.push({ key, value: '', list: value.slice(1, -1).split(',').map((x) => unquote(x.trim())).filter(Boolean) })
      continue
    }
    if (value === '') {
      const items: string[] = []
      const raw: string[] = []
      while (i + 1 < lines.length && /^[ \t-]/.test(lines[i + 1])) {
        i++
        const t = lines[i].trim()
        raw.push(t)
        if (t === '-' || t.startsWith('- ')) {
          const item = unquote(t.slice(1).trim())
          if (item) {
            items.push(item)
          }
        }
      }
      out.push(items.length ? { key, value: '', list: items } : { key, value: raw.join(' '), list: null })
      continue
    }
    out.push({ key, value: unquote(value), list: null })
  }
  return out
}

// --- blocks -----------------------------------------------------------------------

/** What one line of a note is. */
export type LineInfo =
  | { type: 'frontmatter'; first: boolean; last: boolean }
  | { type: 'fence'; open: boolean; lang: string; block: number }
  | { type: 'code'; lang: string; block: number }
  | { type: 'heading'; level: number; markEnd: number }
  | { type: 'list'; ordered: boolean; markFrom: number; markEnd: number; task: { checked: boolean; from: number } | null }
  | { type: 'quote'; markEnd: number; callout: Callout | null; block: number }
  | { type: 'hr' }
  | { type: 'table'; block: number; delimiter: boolean }
  | { type: 'text' }
  | { type: 'blank' }

/** A callout's kind, its title and whether it folds. */
export interface Callout {
  kind: string
  title: string
  fold: '' | '+' | '-'
  /** The line that opens it; the others continue it. */
  head: boolean
}

const fenceRe = /^ {0,3}(`{3,}|~{3,})\s*([^`\s]*)/
const headingRe = /^ {0,3}(#{1,6})(?:[ \t]+|$)/
const listRe = /^(\s*)([-*+]|\d{1,9}[.)])([ \t]+|$)/
const taskRe = /^\[([ xX])\](?=\s|$)/
const quoteRe = /^ {0,3}((?:>[ \t]?)+)/
const calloutRe = /^\[!([A-Za-z0-9_-]+)\]([+-]?)[ \t]*(.*)$/
const hrRe = /^ {0,3}([-*_])(?:[ \t]*\1){2,}[ \t]*$/
const delimiterRe = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/

/** Classifies every line of a note, tracking front matter, code fences, callouts and tables. */
export function classifyLines(lines: string[]): LineInfo[] {
  const out: LineInfo[] = new Array(lines.length)
  let i = 0
  const fm = parseFrontMatter(lines.join('\n'))
  if (fm) {
    for (; i <= fm.lastLine; i++) {
      out[i] = { type: 'frontmatter', first: i === 0, last: i === fm.lastLine }
    }
  }
  let fence: { marker: string; lang: string; block: number } | null = null
  let callout: Callout | null = null
  let quoteBlock = -1
  for (; i < lines.length; i++) {
    const line = lines[i].replace(/\r$/, '')
    if (fence) {
      const m = fenceRe.exec(line)
      if (m && m[1][0] === fence.marker[0] && m[1].length >= fence.marker.length && !line.slice(m[0].length).trim() && !m[2]) {
        out[i] = { type: 'fence', open: false, lang: fence.lang, block: fence.block }
        fence = null
      } else {
        out[i] = { type: 'code', lang: fence.lang, block: fence.block }
      }
      continue
    }
    const f = fenceRe.exec(line)
    if (f) {
      fence = { marker: f[1], lang: f[2] ?? '', block: i }
      out[i] = { type: 'fence', open: true, lang: fence.lang, block: i }
      callout = null
      continue
    }
    if (!line.trim()) {
      out[i] = { type: 'blank' }
      callout = null
      quoteBlock = -1
      continue
    }
    const q = quoteRe.exec(line)
    if (q) {
      const rest = line.slice(q[0].length)
      const c = calloutRe.exec(rest)
      if (quoteBlock < 0) {
        quoteBlock = i
      }
      if (c && (callout === null || i === quoteBlock)) {
        callout = { kind: c[1].toLowerCase(), title: c[3], fold: c[2] as Callout['fold'], head: true }
        out[i] = { type: 'quote', markEnd: q[0].length, callout, block: quoteBlock }
      } else {
        out[i] = { type: 'quote', markEnd: q[0].length, callout: callout ? { ...callout, head: false } : null, block: quoteBlock }
      }
      continue
    }
    callout = null
    quoteBlock = -1
    if (hrRe.test(line)) {
      out[i] = { type: 'hr' }
      continue
    }
    const h = headingRe.exec(line)
    if (h) {
      out[i] = { type: 'heading', level: h[1].length, markEnd: h[0].length }
      continue
    }
    const l = listRe.exec(line)
    if (l) {
      const markFrom = l[1].length
      const markEnd = l[0].length
      const t = taskRe.exec(line.slice(markEnd))
      out[i] = {
        type: 'list',
        ordered: /\d/.test(l[2]),
        markFrom,
        markEnd,
        task: t ? { checked: t[1] !== ' ', from: markEnd } : null,
      }
      continue
    }
    if (line.trimStart().startsWith('|') && (delimiterRe.test(lines[i + 1] ?? '') || (i > 0 && out[i - 1]?.type === 'table'))) {
      const prev = out[i - 1]
      const block = prev?.type === 'table' ? prev.block : i
      out[i] = { type: 'table', block, delimiter: block !== i && delimiterRe.test(line) }
      continue
    }
    out[i] = { type: 'text' }
  }
  return out
}

/** Splits a table row into its cells, trimming the outer pipes. Escaped pipes stay in the cell. */
export function tableCells(row: string): string[] {
  let s = row.trim()
  if (s.startsWith('|')) {
    s = s.slice(1)
  }
  if (s.endsWith('|') && !s.endsWith('\\|')) {
    s = s.slice(0, -1)
  }
  const cells: string[] = []
  let cur = ''
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '\\' && s[i + 1] === '|') {
      cur += '\\|'
      i++
      continue
    }
    if (s[i] === '|') {
      cells.push(cur.trim())
      cur = ''
      continue
    }
    cur += s[i]
  }
  cells.push(cur.trim())
  return cells
}

/** The column alignments a delimiter row asks for. */
export function tableAligns(row: string): ('left' | 'center' | 'right' | '')[] {
  return tableCells(row).map((c) => {
    const l = c.startsWith(':')
    const r = c.endsWith(':')
    return l && r ? 'center' : r ? 'right' : l ? 'left' : ''
  })
}

// --- inline -----------------------------------------------------------------------

/** The kinds of inline span the live preview draws. */
export type InlineKind =
  | 'strong'
  | 'em'
  | 'strike'
  | 'highlight'
  | 'code'
  | 'wiki'
  | 'embed'
  | 'link'
  | 'image'
  | 'tag'
  | 'url'
  | 'comment'

/** One inline span. marks are the syntax ranges hidden when the line is not being edited. */
export interface InlineToken {
  kind: InlineKind
  from: number
  to: number
  marks: [number, number][]
  /** What a link points at, without its subpath. */
  target?: string
  subpath?: string
  /** The visible text of a link. */
  label?: string
}

/** Reads a wiki link body ("Note#Heading|alias") into its parts. */
export function parseWikiBody(body: string): { target: string; subpath: string; alias: string } {
  let target = body
  let alias = ''
  const bar = body.indexOf('|')
  if (bar >= 0) {
    target = body.slice(0, bar).replace(/\\$/, '')
    alias = body.slice(bar + 1).trim()
  }
  let subpath = ''
  const hash = target.indexOf('#')
  if (hash >= 0) {
    subpath = target.slice(hash + 1).trim()
    target = target.slice(0, hash)
  }
  return { target: target.trim(), subpath, alias }
}

const tagRune = /[\p{L}\p{N}\p{Mn}_/-]/u

function isSpace(c: string | undefined): boolean {
  return c === undefined || /\s/.test(c)
}

/** True for a tag body Obsidian accepts: tag characters, and not only digits. */
export function validTag(tag: string): boolean {
  return tag.length > 0 && [...tag].every((c) => tagRune.test(c)) && !/^\d+$/.test(tag)
}

const delims: { mark: string; kind: InlineKind }[] = [
  { mark: '**', kind: 'strong' },
  { mark: '__', kind: 'strong' },
  { mark: '~~', kind: 'strike' },
  { mark: '==', kind: 'highlight' },
  { mark: '%%', kind: 'comment' },
]

/** Finds the inline spans of one line: code, links, embeds, tags, bare urls and emphasis. */
export function inlineTokens(line: string, offset = 0, end = line.length): InlineToken[] {
  const out: InlineToken[] = []
  let i = offset
  while (i < end) {
    const c = line[i]
    if (c === '\\') {
      i += 2
      continue
    }
    if (c === '`') {
      let n = 0
      while (line[i + n] === '`') {
        n++
      }
      const close = line.indexOf('`'.repeat(n), i + n)
      if (close >= 0 && close < end) {
        out.push({ kind: 'code', from: i, to: close + n, marks: [[i, i + n], [close, close + n]] })
        i = close + n
        continue
      }
      i += n
      continue
    }
    if (c === '[' && line[i + 1] === '[') {
      const close = line.indexOf(']]', i + 2)
      if (close > i + 2 && close < end && !line.slice(i + 2, close).includes('[[')) {
        const embed = line[i - 1] === '!'
        const from = embed ? i - 1 : i
        const body = line.slice(i + 2, close)
        const { target, subpath, alias } = parseWikiBody(body)
        const bar = body.indexOf('|')
        const marks: [number, number][] = bar >= 0 ? [[from, i + 3 + bar], [close, close + 2]] : [[from, i + 2], [close, close + 2]]
        out.push({ kind: embed ? 'embed' : 'wiki', from, to: close + 2, marks, target, subpath, label: alias || (target ? (subpath ? `${target} > ${subpath}` : target) : subpath) })
        i = close + 2
        continue
      }
    }
    if (c === '[') {
      const tok = mdLink(line, i, end)
      if (tok) {
        out.push(tok)
        i = tok.to
        continue
      }
    }
    if (c === '#' && (i === 0 || isSpace(line[i - 1]) || line[i - 1] === '(' || line[i - 1] === ',')) {
      let j = i + 1
      while (j < end && tagRune.test(line[j])) {
        j++
      }
      const tag = line.slice(i + 1, j)
      if (validTag(tag)) {
        out.push({ kind: 'tag', from: i, to: j, marks: [], target: tag })
        i = j
        continue
      }
    }
    if ((c === 'h' || c === 'p') && (i === 0 || isSpace(line[i - 1]) || line[i - 1] === '(')) {
      const m = /^(?:https?|pelton):\/\/[^\s<>()]+[^\s<>().,;:!?'"]/.exec(line.slice(i, end))
      if (m) {
        out.push({ kind: 'url', from: i, to: i + m[0].length, marks: [], target: m[0], label: m[0] })
        i += m[0].length
        continue
      }
    }
    if ((c === '*' || c === '_') && line.startsWith(c.repeat(3), i) && !isSpace(line[i + 3])) {
      const close = line.indexOf(c.repeat(3), i + 3)
      if (close > i + 3 && close < end) {
        out.push({ kind: 'strong', from: i, to: close + 3, marks: [[i, i + 2], [close + 1, close + 3]] })
        out.push({ kind: 'em', from: i + 2, to: close + 1, marks: [[i + 2, i + 3], [close, close + 1]] })
        out.push(...inlineTokens(line, i + 3, close))
        i = close + 3
        continue
      }
    }
    const d = delims.find((x) => line.startsWith(x.mark, i))
    if (d && !isSpace(line[i + 2])) {
      const close = findClose(line, d.mark, i + 2, end)
      if (close > i + 2) {
        out.push({ kind: d.kind, from: i, to: close + 2, marks: [[i, i + 2], [close, close + 2]] })
        if (d.kind !== 'comment') {
          out.push(...inlineTokens(line, i + 2, close))
        }
        i = close + 2
        continue
      }
    }
    if ((c === '*' || c === '_') && line[i + 1] !== c && !isSpace(line[i + 1]) && (c === '*' || i === 0 || !/[\p{L}\p{N}]/u.test(line[i - 1]))) {
      const close = findSingle(line, c, i + 1, end)
      if (close > i + 1) {
        out.push({ kind: 'em', from: i, to: close + 1, marks: [[i, i + 1], [close, close + 1]] })
        out.push(...inlineTokens(line, i + 1, close))
        i = close + 1
        continue
      }
    }
    i++
  }
  return out
}

function findClose(line: string, mark: string, from: number, end: number): number {
  let j = line.indexOf(mark, from)
  while (j >= 0 && j < end) {
    if (!isSpace(line[j - 1]) && line[j - 1] !== '\\') {
      return j
    }
    j = line.indexOf(mark, j + 1)
  }
  return -1
}

function findSingle(line: string, c: string, from: number, end: number): number {
  for (let j = from; j < end; j++) {
    if (line[j] === '\\') {
      j++
      continue
    }
    if (line[j] === '`') {
      const close = line.indexOf('`', j + 1)
      if (close > 0) {
        j = close
        continue
      }
    }
    if (line[j] === c && line[j + 1] !== c && line[j - 1] !== c && !isSpace(line[j - 1])) {
      if (c === '_' && /[\p{L}\p{N}]/u.test(line[j + 1] ?? '')) {
        continue
      }
      return j
    }
  }
  return -1
}

function mdLink(line: string, i: number, end: number): InlineToken | null {
  let depth = 0
  let j = i
  for (; j < end; j++) {
    if (line[j] === '\\') {
      j++
      continue
    }
    if (line[j] === '[') {
      depth++
    } else if (line[j] === ']') {
      depth--
      if (depth === 0) {
        break
      }
    }
  }
  if (j >= end || line[j + 1] !== '(') {
    return null
  }
  const open = j + 2
  let raw: string
  let close: number
  if (line[open] === '<') {
    const gt = line.indexOf('>', open)
    if (gt < 0) {
      return null
    }
    raw = line.slice(open + 1, gt)
    close = line.indexOf(')', gt)
  } else {
    let k = open
    let paren = 0
    while (k < end) {
      if (line[k] === '(') {
        paren++
      } else if (line[k] === ')') {
        if (paren === 0) {
          break
        }
        paren--
      } else if (line[k] === ' ') {
        break
      }
      k++
    }
    raw = line.slice(open, k)
    close = line.indexOf(')', k)
  }
  if (close < 0 || close >= end || !raw) {
    return null
  }
  const image = line[i - 1] === '!'
  const from = image ? i - 1 : i
  let target = raw
  let subpath = ''
  const lower = raw.toLowerCase()
  if (!/^[a-z][a-z0-9+.-]*:/.test(lower) && !lower.startsWith('#')) {
    const hash = raw.indexOf('#')
    if (hash >= 0) {
      subpath = safeDecode(raw.slice(hash + 1))
      target = raw.slice(0, hash)
    }
    target = safeDecode(target)
  }
  return {
    kind: image ? 'image' : 'link',
    from,
    to: close + 1,
    marks: [[from, i + 1], [j, close + 1]],
    target,
    subpath,
    label: line.slice(i + 1, j),
  }
}

function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

const hashComments = new Set(['python', 'py', 'sh', 'bash', 'zsh', 'shell', 'yaml', 'yml', 'toml', 'ruby', 'rb', 'r', 'perl', 'make', 'makefile', 'dockerfile', 'conf', 'ini', 'nix'])
const keywords = new Set(
  'and as async await break case catch class const continue def default defer del do elif else enum export extends false fn for from func function go if impl import in interface is let match mod module new nil none not null or package pass pub return self static struct super switch this throw true try type typeof use var void while with yield'.split(' '),
)

/** One highlighted span of a code line. */
export interface CodeToken {
  from: number
  to: number
  kind: 'comment' | 'string' | 'number' | 'keyword'
}

/**
 * Highlights one line of a fenced code block with a small set of rules most
 * languages share: comments, strings, numbers and common keywords. A comment
 * or string that spans lines is not followed onto the next one.
 */
export function codeTokens(line: string, lang: string): CodeToken[] {
  const out: CodeToken[] = []
  const hash = hashComments.has(lang.toLowerCase())
  const re = hash
    ? /(#.*$)|("(?:[^"\\]|\\.)*"?|'(?:[^'\\]|\\.)*'?)|(\b\d[\d_]*(?:\.\d+)?\b)|([A-Za-z_][A-Za-z0-9_]*)/g
    : /(\/\/.*$|\/\*.*?(?:\*\/|$)|--\s.*$)|("(?:[^"\\]|\\.)*"?|'(?:[^'\\]|\\.)*'?|`(?:[^`\\]|\\.)*`?)|(\b\d[\d_]*(?:\.\d+)?\b)|([A-Za-z_][A-Za-z0-9_]*)/g
  for (const m of line.matchAll(re)) {
    const from = m.index ?? 0
    const to = from + m[0].length
    if (m[1]) {
      out.push({ from, to, kind: 'comment' })
    } else if (m[2]) {
      out.push({ from, to, kind: 'string' })
    } else if (m[3]) {
      out.push({ from, to, kind: 'number' })
    } else if (m[4] && keywords.has(m[4])) {
      out.push({ from, to, kind: 'keyword' })
    }
  }
  return out
}

/**
 * What pressing Enter at the end of a list item, task or quote line starts
 * the next line with. empty is set when the item itself holds no text, which
 * ends the list instead.
 */
export function nextListPrefix(line: string): { prefix: string; empty: boolean; markerLength: number } | null {
  const m = /^(\s*)(?:([-*+])|(\d{1,9})([.)]))([ \t]+)(\[[ xX]\][ \t]+)?/.exec(line)
  if (m) {
    const marker = m[2] ?? `${Number(m[3]) + 1}${m[4]}`
    return {
      prefix: `${m[1]}${marker}${m[5]}${m[6] ? '[ ] ' : ''}`,
      empty: line.slice(m[0].length).trim() === '',
      markerLength: m[0].length,
    }
  }
  const q = /^(\s*(?:>[ \t]?)+)/.exec(line)
  if (q) {
    return { prefix: q[1].endsWith(' ') ? q[1] : `${q[1]} `, empty: line.slice(q[0].length).trim() === '', markerLength: q[0].length }
  }
  return null
}

/** The tags written in a note's text, front matter included, without the '#'. */
export function noteTags(text: string): string[] {
  const seen = new Map<string, string>()
  const add = (tag: string): void => {
    const t = tag.replace(/^#/, '').trim()
    if (validTag(t) && !seen.has(t.toLowerCase())) {
      seen.set(t.toLowerCase(), t)
    }
  }
  const fm = parseFrontMatter(text)
  for (const p of fm?.props ?? []) {
    if (p.key.toLowerCase() === 'tags' || p.key.toLowerCase() === 'tag') {
      for (const item of p.list ?? p.value.split(/[\s,]+/)) {
        add(item)
      }
    }
  }
  const lines = text.split('\n')
  const kinds = classifyLines(lines)
  lines.forEach((line, i) => {
    const k = kinds[i]
    if (k.type === 'code' || k.type === 'fence' || k.type === 'frontmatter') {
      return
    }
    for (const t of inlineTokens(line)) {
      if (t.kind === 'tag' && t.target) {
        add(t.target)
      }
    }
  })
  return [...seen.values()]
}

/** The headings of a note's text, with their line. */
export function noteHeadings(text: string): { level: number; text: string; line: number }[] {
  const lines = text.split('\n')
  const kinds = classifyLines(lines)
  const out: { level: number; text: string; line: number }[] = []
  lines.forEach((line, i) => {
    const k = kinds[i]
    if (k.type === 'heading') {
      out.push({ level: k.level, text: line.slice(k.markEnd).replace(/[ \t]+#+[ \t]*$/, '').replace(/\r$/, '').trim(), line: i })
    }
  })
  return out
}

/** The name a note goes by: its file name without the extension. */
export function noteTitle(path: string): string {
  const base = path.slice(path.lastIndexOf('/') + 1)
  const dot = base.lastIndexOf('.')
  return dot > 0 ? base.slice(0, dot) : base
}

/** The folder of a vault path, '' at the top. */
export function noteFolder(path: string): string {
  const slash = path.lastIndexOf('/')
  return slash < 0 ? '' : path.slice(0, slash)
}

/** Characters a file name may not hold on one of the platforms Pelton runs on. */
const badName = /[\\/:*?"<>|#^[\]]/g

/** Turns a title into a safe file name for a note, without the extension. */
export function safeNoteName(title: string): string {
  return title.replace(badName, ' ').replace(/\s+/g, ' ').trim().replace(/^\.+/, '') || 'Untitled'
}

/** Joins a folder and a name into a vault path. */
export function joinPath(folder: string, name: string): string {
  const f = folder.replace(/^\/+|\/+$/g, '')
  return f ? `${f}/${name}` : name
}

/** True for a path of an image a note can show inline. */
export function isImagePath(path: string): boolean {
  return /\.(png|jpe?g|gif|webp|svg|bmp|avif)$/i.test(path)
}
