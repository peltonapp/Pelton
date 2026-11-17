import { describe, expect, it } from 'vitest'
import type { ScriptCatalog } from '../../../lib/scripting'
import { blockCatalog } from '../../../test/scriptfixtures'
import { completionsAt, docAt, settingNames, signature, syntaxErrorLine, type LuaContext } from './assist'
import { tokenizeLines } from './highlight'
import { referenceSections, searchReference } from './reference'

const catalog: ScriptCatalog = {
  ...blockCatalog,
  modules: [
    {
      id: 'mail',
      labelKey: '',
      label: 'Mail',
      descKey: '',
      description: 'Reads and changes mail.',
      category: 'mail',
      functions: [
        {
          name: 'move',
          descKey: '',
          description: 'Moves a message.',
          params: [
            { name: 'message', type: 'ref', labelKey: '', valueType: 'message' },
            { name: 'folder', type: 'folder', labelKey: '' },
          ],
          permission: 'mail.modify',
          permissions: ['mail.modify'],
          effect: true,
        },
        {
          name: 'send',
          descKey: '',
          params: [{ name: 'draft', type: 'ref', labelKey: '', valueType: 'draft' }],
          returns: [{ name: 'id', type: 'number', labelKey: '' }],
          permissions: ['mail.send'],
          effect: true,
          risky: true,
        },
      ],
    },
  ],
}

const ctx: LuaContext = { catalog, trigger: 'mail.arrived', settings: ['folder', 'days'], libraries: ['dates', 'helpers'], t: (k) => k }

describe('completion', () => {
  it('offers module functions with their signatures', () => {
    const c = completionsAt('local x = mail.mo', ctx)
    expect(c?.back).toBe(2)
    expect(c?.items.map((i) => i.label)).toEqual(['move'])
    expect(c?.items[0].insert).toBe('move(')
    expect(c?.items[0].detail).toBe('mail.move(message: message?, folder: folder?)')
  })

  it('offers the event fields of the trigger and the fields of their value type', () => {
    expect(completionsAt('if event.', ctx)?.items.map((i) => i.label)).toEqual(['message'])
    expect(completionsAt('print(event.message.su', ctx)?.items.map((i) => i.label)).toEqual(['subject'])
    expect(completionsAt('event.message.nothing.', ctx)).toBeNull()
  })

  it('offers setting names and libraries inside their strings', () => {
    expect(completionsAt('local f = settings.get("fo', ctx)?.items.map((i) => i.label)).toEqual(['folder'])
    expect(completionsAt("local d = require('d", ctx)?.items.map((i) => i.label)).toEqual(['dates'])
  })

  it('offers globals and keywords, but nothing in strings or comments', () => {
    const labels = completionsAt('ma', ctx)?.items.map((i) => i.label)
    expect(labels).toContain('mail')
    expect(labels).toContain('math')
    expect(completionsAt('local s = "mai', ctx)).toBeNull()
    expect(completionsAt('-- mai', ctx)).toBeNull()
  })
})

describe('docs', () => {
  it('explains the function under the caret or the call around it', () => {
    const line = 'mail.send(draft)'
    const under = docAt(line, 6, ctx)
    expect(under?.title).toBe('mail.send(draft: draft?) -> {id: number}')
    expect(under?.permissions).toEqual(['mail.send'])
    expect(under?.risk).toBe('high')
    expect(under?.risky).toBe(true)
    expect(docAt(line, 12, ctx)?.title).toContain('mail.send(')
    const move = docAt('mail.move(m, "Archive")', 15, ctx)
    expect(move?.description).toBe('Moves a message.')
    expect(move?.risk).toBe('medium')
    expect(move?.effect).toBe(true)
  })

  it('explains event fields and run globals', () => {
    expect(docAt('event.message.subject', 18, ctx)?.title).toBe('event.message.subject: string')
    expect(docAt('stop()', 2, ctx)?.description).toBe('scripts.lua.global.stop')
    expect(docAt('local x = 1', 3, ctx)).toBeNull()
  })

  it('reads settings and syntax error lines', () => {
    expect(settingNames('SETTINGS = {\n  { name = "days", type = "number" },\n  { name = \'folder\', type = "folder" },\n}\n')).toEqual(['days', 'folder'])
    expect(settingNames('print(1)')).toEqual([])
    expect(syntaxErrorLine("script line:4(column:2) near 'end': syntax error")).toBe(4)
    expect(syntaxErrorLine('broken')).toBe(0)
  })

  it('writes signatures with results', () => {
    expect(signature('mail', catalog.modules[0].functions[1])).toBe('mail.send(draft: draft?) -> {id: number}')
  })
})

describe('highlight', () => {
  it('colours keywords, strings, numbers, comments, builtins and modules', () => {
    const [line] = tokenizeLines(['local n = mail.move(x, "Archive", 3) -- done'], new Set(['mail']))
    const kinds = line.map((t) => t.kind)
    expect(kinds).toEqual(['keyword', 'module', 'string', 'number', 'comment'])
    expect(line[1]).toEqual({ from: 10, to: 19, kind: 'module' })
  })

  it('carries a long comment over lines', () => {
    const lines = tokenizeLines(['--[[ start', 'still comment', ']] print(1)'], new Set())
    expect(lines[1]).toEqual([{ from: 0, to: 13, kind: 'comment' }])
    expect(lines[2].map((t) => t.kind)).toEqual(['comment', 'builtin', 'number'])
  })
})

describe('reference', () => {
  it('lists globals, modules, triggers, value types and permissions, and searches them', () => {
    const sections = referenceSections(catalog, (k) => k)
    expect(sections.map((s) => s.kind)).toEqual(['globals', 'module', 'trigger', 'trigger', 'trigger', 'type', 'permissions'])
    const mail = sections.find((s) => s.key === 'module:mail')!
    expect(mail.entries[1]).toMatchObject({ name: 'mail.send', insert: 'mail.send(', risky: true, risk: 'high' })
    const found = searchReference(sections, 'moves a')
    expect(found).toHaveLength(1)
    expect(found[0].entries.map((e) => e.name)).toEqual(['mail.move'])
    expect(searchReference(sections, 'zzz')).toEqual([])
    const arrived = sections.find((s) => s.key === 'trigger:mail.arrived')!
    expect(arrived.entries[0].insert).toBe('event.message')
  })
})
