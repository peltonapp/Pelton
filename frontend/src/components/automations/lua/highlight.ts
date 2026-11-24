// highlight.ts colours Lua in the script editor. CodeMirror parses the text
// with @codemirror/legacy-modes' Lua mode through StreamLanguage, and the
// tokens on screen get classes the editor's theme colours from the app's
// tokens. A name that starts with a registered module, such as mail.move,
// gets a class of its own.

import { StreamLanguage, syntaxTree } from '@codemirror/language'
import { RangeSetBuilder, type EditorState, type Extension } from '@codemirror/state'
import { Decoration, ViewPlugin, type DecorationSet, type EditorView, type ViewUpdate } from '@codemirror/view'
import { lua } from '@codemirror/legacy-modes/mode/lua'

/** The token kinds the editor colours. */
export type LuaToken = 'keyword' | 'string' | 'number' | 'comment' | 'builtin' | 'module'

/** One coloured token: where it starts and ends in the document, and its kind. */
export interface DocToken {
  from: number
  to: number
  kind: LuaToken
}

/** Lua, parsed by the legacy Lua mode. */
export const luaLanguage = StreamLanguage.define(lua)

// node names are the Lua mode's styles after StreamLanguage's renaming,
// e.g. "builtin" becomes "variableName.standard".
const kinds: Record<string, LuaToken> = {
  keyword: 'keyword',
  string: 'string',
  number: 'number',
  comment: 'comment',
  'variableName.standard': 'builtin',
}

/**
 * The coloured tokens between from and to in a state that has luaLanguage,
 * as far as it is parsed. modules are the global names of the registered
 * modules.
 */
export function luaTokens(state: EditorState, modules: ReadonlySet<string>, from = 0, to = state.doc.length): DocToken[] {
  const out: DocToken[] = []
  syntaxTree(state).iterate({
    from,
    to,
    enter: (node) => {
      let kind: LuaToken | undefined = kinds[node.name]
      if (node.name === 'variableName' &&modules.has(state.sliceDoc(node.from, node.to).split('.')[0])) {
        kind = 'module'
      }
      if (kind) {
        out.push({ from: node.from, to: node.to, kind })
      }
    },
  })
  return out
}

const marks = new Map<LuaToken, Decoration>(
  (['keyword', 'string', 'number', 'comment', 'builtin', 'module'] as LuaToken[]).map((k) => [k, Decoration.mark({ class: `cm-lua-${k}` })]),
)

function decorate(view: EditorView, modules: ReadonlySet<string>): DecorationSet {
  const builder = new RangeSetBuilder<Decoration>()
  for (const { from, to } of view.visibleRanges) {
    for (const tok of luaTokens(view.state, modules, from, to)) {
      builder.add(tok.from, tok.to, marks.get(tok.kind)!)
    }
  }
  return builder.finish()
}

/** Lua parsing and syntax colouring; modules are the registered module names. */
export function luaHighlight(modules: ReadonlySet<string>): Extension {
  return [
    luaLanguage,
    ViewPlugin.fromClass(
      class {
        decorations: DecorationSet

        constructor(view: EditorView) {
          this.decorations = decorate(view, modules)
        }

        update(u: ViewUpdate): void {
          if (u.docChanged || u.viewportChanged || syntaxTree(u.startState) !== syntaxTree(u.state)) {
            this.decorations = decorate(u.view, modules)
          }
        }
      },
      { decorations: (v) => v.decorations },
    ),
  ]
}
