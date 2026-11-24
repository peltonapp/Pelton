import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import RuleEditor from './RuleEditor.svelte'
import type { SieveCaps, SieveRule } from '../../../lib/types'

const caps: SieveCaps = {
  implementation: 'test',
  extensions: ['fileinto', 'imap4flags'],
  vacation: false,
  dates: false,
  fileInto: true,
  flags: true,
  envelope: false,
  include: false,
}

function rule(patch: Partial<SieveRule> = {}): SieveRule {
  return {
    name: 'Lists',
    enabled: true,
    matchAny: false,
    conditions: [{ field: 'from', header: '', op: 'contains', negate: false, value: 'news@' }],
    actions: [{ kind: 'fileinto', arg: 'Lists' }],
    ...patch,
  }
}

const folders = [
  { value: 'INBOX', label: 'INBOX' },
  { value: 'Lists', label: 'Lists' },
  { value: 'Archive/2026', label: 'Archive / 2026' },
]

function renderEditor(props: Partial<{ rule: SieveRule; caps: SieveCaps }> = {}) {
  const onChange = vi.fn<(r: SieveRule) => void>()
  const view = render(RuleEditor, {
    props: { rule: rule(), caps, folders, ...props },
    events: { change: (e: CustomEvent<SieveRule>) => onChange(e.detail) },
  })
  return { ...view, onChange, last: () => onChange.mock.calls.at(-1)?.[0] as SieveRule }
}

describe('RuleEditor', () => {
  it('renames the rule', async () => {
    const { last } = renderEditor()
    await fireEvent.input(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'Newsletters' } })
    expect(last().name).toBe('Newsletters')
  })

  it('adds and removes conditions', async () => {
    const { last, rerender } = renderEditor()
    await userEvent.click(screen.getByRole('button', { name: 'Add condition' }))
    expect(last().conditions).toHaveLength(2)

    await rerender({ rule: last() })
    const second = screen.getByRole('listitem', { name: 'Condition 2' })
    await userEvent.click(within(second).getByRole('button', { name: 'Remove condition' }))
    expect(last().conditions).toHaveLength(1)
  })

  it('negates through the comparison menu', async () => {
    const { last } = renderEditor()
    await userEvent.click(screen.getByRole('combobox', { name: 'Comparison' }))
    await userEvent.click(screen.getByRole('option', { name: 'does not contain' }))
    expect(last().conditions[0]).toMatchObject({ op: 'contains', negate: true })
  })

  it('asks for a header name and offers exists for a header condition', async () => {
    const { last, rerender } = renderEditor()
    await userEvent.click(screen.getByRole('combobox', { name: 'Field' }))
    await userEvent.click(screen.getByRole('option', { name: 'Header' }))
    await rerender({ rule: last() })

    await fireEvent.input(screen.getByRole('textbox', { name: 'Header name' }), { target: { value: 'List-Id' } })
    expect(last().conditions[0]).toMatchObject({ field: 'header', header: 'List-Id' })

    await rerender({ rule: rule({ conditions: [{ field: 'header', header: 'List-Id', op: 'contains', negate: false, value: '' }] }) })
    await userEvent.click(screen.getByRole('combobox', { name: 'Comparison' }))
    await userEvent.click(screen.getByRole('option', { name: 'exists' }))
    await rerender({ rule: last() })
    expect(screen.queryByRole('textbox', { name: 'Value' })).not.toBeInTheDocument()
  })

  it('moves to a folder picked from the account folders', async () => {
    const { last } = renderEditor()
    await userEvent.click(screen.getByRole('combobox', { name: 'Folder' }))
    await userEvent.click(screen.getByRole('option', { name: 'Archive / 2026' }))
    expect(last().actions[0]).toEqual({ kind: 'fileinto', arg: 'Archive/2026' })
  })

  it('forwards to an address', async () => {
    const { last, rerender } = renderEditor()
    await userEvent.click(screen.getByRole('combobox', { name: 'Action' }))
    await userEvent.click(screen.getByRole('option', { name: 'Forward to' }))
    expect(last().actions[0]).toEqual({ kind: 'redirect', arg: '' })
    await rerender({ rule: last() })
    await fireEvent.input(screen.getByRole('textbox', { name: 'Address' }), { target: { value: ' me@example.org ' } })
    expect(last().actions[0]).toEqual({ kind: 'redirect', arg: 'me@example.org' })
  })

  it('adds the next action the rule does not have yet', async () => {
    const { last } = renderEditor()
    await userEvent.click(screen.getByRole('button', { name: 'Add action' }))
    expect(last().actions.map((a) => a.arg)).toEqual(['Lists', '\\Seen'])
  })

  it('hides actions the server cannot run', async () => {
    renderEditor({ caps: { ...caps, fileInto: false, flags: false }, rule: rule({ actions: [{ kind: 'stop', arg: '' }] }) })
    await userEvent.click(screen.getByRole('combobox', { name: 'Action' }))
    const names = screen.getAllByRole('option').map((o) => o.textContent?.trim())
    expect(names).not.toContain('Move to folder')
    expect(names).not.toContain('Mark as read')
    expect(names).toContain('Delete')
  })

  it('says what is missing', () => {
    renderEditor({ rule: rule({ actions: [{ kind: 'fileinto', arg: '' }] }) })
    expect(screen.getByText('Pick a folder.')).toBeInTheDocument()
  })

  it('shows the match choice only with more than one condition', async () => {
    const two = rule({ conditions: [...rule().conditions, { field: 'subject', header: '', op: 'is', negate: false, value: 'hi' }] })
    const { last, rerender } = renderEditor()
    expect(screen.queryByRole('radiogroup', { name: 'When' })).not.toBeInTheDocument()
    await rerender({ rule: two })
    const group = screen.getByRole('radiogroup', { name: 'When' })
    await userEvent.click(within(group).getByRole('radio', { name: 'Any matches' }))
    expect(last().matchAny).toBe(true)
  })
})
