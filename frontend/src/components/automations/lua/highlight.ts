// highlight.ts colours Lua in the script editor. It drives the tokenizer of
// @codemirror/legacy-modes' Lua mode with a small stream of its own, since the
// stream language wrapper lives in a package the app does not ship, and marks
// the tokens on screen with classes the editor's theme colours from the app's
// tokens. A name that starts with a registered module, such as mail.move,
// gets a class of its own.

import { RangeSetBuilder, type Extension } from '@codemirror/state'
import { Decoration, ViewPlugin, type DecorationSet, type EditorView, type ViewUpdate } from '@codemirror/view'
import { lua } from '@codemirror/legacy-modes/mode/lua'

/** The token kinds the editor colours. */
export type LuaToken = 'keyword' | 'string' | 'number' | 'comment' | 'builtin' | 'module'

type State = Record<string, unknown>

interface Parser {
  startState: () => State
  token: (stream: LineStream, state: State) => string | null
}

const parser = lua as unknown as Parser

/** The part of CodeMirror's string stream the Lua mode reads. */
class LineStream {
  pos = 0
  start = 0

  constructor(readonly string: string) {}

  eol(): boolean {
    return this.pos >= this.string.length
  }

  sol(): boolean {
    return this.pos === 0
  }

  peek(): string | undefined {
    return this.string.charAt(this.pos) || undefined
  }

  next(): string | undefined {
    if (this.pos < this.string.length) {
      return this.string.charAt(this.pos++)
    }
    return undefined
  }

  eat(match: string | RegExp | ((ch: string) => boolean)): string | undefined {
    const ch = this.string.charAt(this.pos)
    const ok = typeof match === 'string' ? ch === match : ch && (match instanceof RegExp ? match.test(ch) : match(ch))
    if (ok) {
      this.pos++
      return ch
    }
    return undefined
  }

  eatWhile(match: string | RegExp | ((ch: string) => boolean)): boolean {
    const from = this.pos
    while (this.eat(match)) {
      // keep eating
    }
    return this.pos > from
  }

  eatSpace(): boolean {
    const from = this.pos
    while (/[\s ]/.test(this.string.charAt(this.pos))) {
      this.pos++
    }
    return this.pos > from
  }

  skipToEnd(): void {
    this.pos = this.string.length
  }

  backUp(n: number): void {
    this.pos -= n
  }

  current(): string {
    return this.string.slice(this.start, this.pos)
  }
}

const kinds: Record<string, LuaToken> = {
  keyword: 'keyword',
  string: 'string',
  number: 'number',
  comment: 'comment',
  builtin: 'builtin',
}

/** One coloured token: where it starts and ends in its line, and its kind. */
export interface LineToken {
  from: number
  to: number
  kind: LuaToken
}

/**
 * Tokenizes lines of Lua in order, carrying the tokenizer's state across
 * them so long strings and comments span lines. modules are the global
 * names of the registered modules.
 */
export function tokenizeLines(lines: string[], modules: ReadonlySet<string>): LineToken[][] {
  let state = parser.startState()
  return lines.map((text) => {
    const out: LineToken[] = []
    const stream = new LineStream(text)
    state = { ...state }
    while (!stream.eol()) {
      stream.start = stream.pos
      const style = parser.token(stream, state)
      if (stream.pos === stream.start) {
        stream.pos++
        continue
      }
      let kind = style ? kinds[style] : undefined
      if (style === 'variable' && modules.has(stream.current().split('.')[0])) {
        kind = 'module'
      }
      if (kind) {
        out.push({ from: stream.start, to: stream.pos, kind })
      }
    }
    return out
  })
}

const marks = new Map<LuaToken, Decoration>(
  (['keyword', 'string', 'number', 'comment', 'builtin', 'module'] as LuaToken[]).map((k) => [k, Decoration.mark({ class: `cm-lua-${k}` })]),
)

function decorate(view: EditorView, modules: ReadonlySet<string>): DecorationSet {
  const doc = view.state.doc
  const last = view.visibleRanges.length > 0 ? doc.lineAt(view.visibleRanges[view.visibleRanges.length - 1].to).number : doc.lines
  const lines: string[] = []
  for (let n = 1; n <= last; n++) {
    lines.push(doc.line(n).text)
  }
  const tokens = tokenizeLines(lines, modules)
  const builder = new RangeSetBuilder<Decoration>()
  for (const { from, to } of view.visibleRanges) {
    const first = doc.lineAt(from).number
    const end = doc.lineAt(to).number
    for (let n = first; n <= end; n++) {
      const start = doc.line(n).from
      for (const tok of tokens[n - 1] ?? []) {
        builder.add(start + tok.from, start + tok.to, marks.get(tok.kind)!)
      }
    }
  }
  return builder.finish()
}

/** Lua syntax colouring; modules are the registered module names. */
export function luaHighlight(modules: ReadonlySet<string>): Extension {
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet

      constructor(view: EditorView) {
        this.decorations = decorate(view, modules)
      }

      update(u: ViewUpdate): void {
        if (u.docChanged || u.viewportChanged) {
          this.decorations = decorate(u.view, modules)
        }
      }
    },
    { decorations: (v) => v.decorations },
  )
}
