import { describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

vi.mock('../stores/contacts', async () => {
  const { writable } = await import('svelte/store')
  return {
    contacts: writable([
      { id: 7, fullName: 'Ada Lovelace', organization: 'Analytical', emails: [{ value: 'ada@example.org' }], phones: [] },
    ]),
    loadContacts: vi.fn(),
  }
})

import { extraPaletteCommands } from './palettesources'
import { selectedContactId } from '../stores/contactsview'
import { space } from '../stores/space'
import './palette-contacts'
import type { CommandStep } from './commands'

const t = (key: string) => key
const find = (id: string) => extraPaletteCommands(t).find((c) => c.id === id)

describe('palette-contacts', () => {
  it('offers the contacts entries', () => {
    const ids = extraPaletteCommands(t)
      .map((c) => c.id)
      .filter((id) => id.startsWith('contacts:'))
    expect(ids).toEqual(
      expect.arrayContaining(['contacts:open', 'contacts:new', 'contacts:find', 'contacts:view:panes', 'contacts:view:grid', 'contacts:manage']),
    )
  })

  it('opens a contact picked from the find step', () => {
    const step = find('contacts:find')!.run() as CommandStep
    expect(step.items).toHaveLength(1)
    expect(step.items[0].label).toBe('Ada Lovelace')
    void step.items[0].run()
    expect(get(selectedContactId)).toBe(7)
    expect(get(space)).toBe('contacts')
  })
})
