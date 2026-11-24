import { describe, expect, it } from 'vitest'
import { expandPlaceholders, filterSnippets, matchTrigger, placeholdersIn, recipientNames, snippetFields, type SnippetContext } from './snippets'
import type { Snippet } from './types'

function snippet(id: number, over: Partial<Snippet> = {}): Snippet {
  return { id, name: `Snippet ${id}`, body: '', subject: '', to: '', cc: '', trigger: '', position: id, ...over }
}

const ctx: SnippetContext = { firstName: 'Ada', fullName: 'Ada Lovelace', myName: 'Charles', date: '15/11/2026' }

describe('expandPlaceholders', () => {
  it('fills the built-in placeholders', () => {
    expect(expandPlaceholders('Hi {first name}, {full name}. {my name}, {date}', ctx)).toBe('Hi Ada, Ada Lovelace. Charles, 15/11/2026')
  })

  it('reads them without regard to case or spacing', () => {
    expect(expandPlaceholders('{First Name} {  my   name }', ctx)).toBe('Ada Charles')
  })

  it('leaves free placeholders and ones with nothing to fill them', () => {
    expect(expandPlaceholders('{first name}, see you {day}', { ...ctx, firstName: '' })).toBe('{first name}, see you {day}')
  })

  it('leaves braces across lines and empty braces alone', () => {
    expect(expandPlaceholders('{}\n{a\nb}', ctx)).toBe('{}\n{a\nb}')
  })
})

describe('placeholdersIn', () => {
  it('lists what is left to fill, in order', () => {
    expect(placeholdersIn('On {day} at {time}, {} {x\ny}')).toEqual(['{day}', '{time}'])
  })
})

describe('recipientNames', () => {
  const cases: [string, string, string][] = [
    ['Ada Lovelace <ada@example.com>', 'Ada', 'Ada Lovelace'],
    ['"Lovelace, Ada" <ada@example.com>', 'Ada', 'Lovelace, Ada'],
    ['ada.lovelace@example.com', 'Ada', ''],
    ['bob@example.com, Carol <carol@x>', 'Bob', ''],
    ['', '', ''],
  ]
  for (const [to, first, full] of cases) {
    it(`reads ${JSON.stringify(to)}`, () => {
      expect(recipientNames(to)).toEqual({ firstName: first, fullName: full })
    })
  }
})

describe('matchTrigger', () => {
  const list = [snippet(1, { trigger: 'thanks' }), snippet(2, { trigger: 'addr' }), snippet(3)]

  it('finds a trigger at the start of the line or after a space', () => {
    expect(matchTrigger(';thanks', list)).toEqual({ snippet: list[0], length: 7 })
    expect(matchTrigger('Hello ;addr', list)).toEqual({ snippet: list[1], length: 5 })
  })

  it('ignores case', () => {
    expect(matchTrigger(';THANKS', list)?.snippet.id).toBe(1)
  })

  it('needs the trigger right before the caret and on its own', () => {
    expect(matchTrigger(';thanks ', list)).toBeNull()
    expect(matchTrigger('word;thanks', list)).toBeNull()
    expect(matchTrigger(';thank', list)).toBeNull()
    expect(matchTrigger('thanks', list)).toBeNull()
  })

  it('never matches a snippet without a trigger', () => {
    expect(matchTrigger(';', list)).toBeNull()
  })

  it('takes letters beyond ascii', () => {
    expect(matchTrigger(';grüße', [snippet(4, { trigger: 'grüße' })])?.snippet.id).toBe(4)
  })
})

describe('filterSnippets', () => {
  const list = [
    snippet(1, { name: 'Thanks', trigger: 'ty', body: 'Thanks a lot' }),
    snippet(2, { name: 'Address', trigger: 'addr', body: 'Main street 1' }),
    snippet(3, { name: 'Meeting', body: 'Shall we talk on {day}?' }),
  ]

  it('keeps the order for an empty query', () => {
    expect(filterSnippets(list, ' ').map((s) => s.id)).toEqual([1, 2, 3])
  })

  it('matches the name, the trigger and the text', () => {
    expect(filterSnippets(list, 'addr').map((s) => s.id)[0]).toBe(2)
    expect(filterSnippets(list, ';ty').map((s) => s.id)).toContain(1)
    expect(filterSnippets(list, 'talk').map((s) => s.id)).toEqual([3])
  })

  it('drops what does not match', () => {
    expect(filterSnippets(list, 'zzz')).toEqual([])
  })
})

describe('snippetFields', () => {
  const full = snippet(1, { subject: 'Invoice', to: 'billing@x', cc: 'boss@x' })

  it('fills empty fields', () => {
    expect(snippetFields(full, { subject: '', to: ' ', cc: '' })).toEqual({ subject: 'Invoice', to: 'billing@x', cc: 'boss@x', showCc: true })
  })

  it('never overwrites what was typed', () => {
    expect(snippetFields(full, { subject: 'Mine', to: 'ann@x', cc: 'bob@x' })).toEqual({})
  })

  it('changes nothing for a snippet without fields', () => {
    expect(snippetFields(snippet(2), { subject: '', to: '', cc: '' })).toEqual({})
  })
})
