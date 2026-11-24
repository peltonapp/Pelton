import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { SieveCaps, SieveRule } from '../../../lib/types'

const confirm = vi.hoisted(() => ({ askConfirm: vi.fn(async () => true) }))
vi.mock('../../../stores/confirm', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../stores/confirm')>()),
  ...confirm,
}))

import RuleList from './RuleList.svelte'

const caps: SieveCaps = {
  implementation: 'test',
  extensions: [],
  vacation: true,
  dates: true,
  fileInto: true,
  flags: true,
  envelope: false,
  include: false,
}

function rule(name: string): SieveRule {
  return {
    name,
    enabled: true,
    matchAny: false,
    conditions: [{ field: 'subject', header: '', op: 'contains', negate: false, value: name.toLowerCase() }],
    actions: [{ kind: 'discard', arg: '' }],
  }
}

function renderList(saved: SieveRule[] = [rule('First'), rule('Second')]) {
  const onSave = vi.fn<(rules: SieveRule[]) => void>()
  render(RuleList, {
    props: { saved, caps, folders: [] },
    events: { save: (e: CustomEvent<SieveRule[]>) => onSave(e.detail) },
  })
  return { onSave, saved: () => onSave.mock.calls.at(-1)?.[0] as SieveRule[] }
}

describe('RuleList', () => {
  it('summarizes each rule', () => {
    renderList()
    expect(screen.getByText('Subject contains first: Delete')).toBeInTheDocument()
  })

  it('switches a rule off and saves the list', async () => {
    const { saved } = renderList()
    await userEvent.click(screen.getAllByRole('switch', { name: 'Rule on' })[1])
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(saved().map((r) => r.enabled)).toEqual([true, false])
  })

  it('duplicates a rule right below it', async () => {
    const { saved } = renderList()
    await userEvent.click(screen.getAllByRole('button', { name: 'Duplicate' })[0])
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(saved().map((r) => r.name)).toEqual(['First', 'First copy', 'Second'])
  })

  it('deletes a rule after asking', async () => {
    const { saved } = renderList()
    await userEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    expect(confirm.askConfirm).toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(saved().map((r) => r.name)).toEqual(['Second'])
  })

  it('moves a rule with alt and an arrow on its grip', async () => {
    const { saved } = renderList()
    const grip = screen.getAllByRole('button', { name: /Drag to reorder/ })[1]
    await fireEvent.keyDown(grip, { key: 'ArrowUp', altKey: true })
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(saved().map((r) => r.name)).toEqual(['Second', 'First'])
  })

  it('opens a new rule for editing and holds the save until it is complete', async () => {
    renderList([])
    expect(screen.getByText('No rules yet.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Rule 1')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.getAllByText(/Fill in the value to compare with/).length).toBeGreaterThan(0)
  })

  it('discards changes back to the saved rules', async () => {
    renderList()
    await userEvent.click(screen.getAllByRole('button', { name: 'Duplicate' })[0])
    await userEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(screen.getAllByRole('switch', { name: 'Rule on' })).toHaveLength(2)
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })
})
