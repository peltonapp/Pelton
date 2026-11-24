import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import VacationForm from './VacationForm.svelte'
import type { SieveVacation } from '../../../lib/types'

const saved: SieveVacation = {
  enabled: true,
  start: '2026-12-20',
  end: '2027-01-06',
  days: 7,
  subject: 'Away',
  from: '',
  addresses: ['me@example.org'],
  body: 'Back in January.',
}

function renderForm(props: Record<string, unknown> = {}) {
  const onSave = vi.fn<(v: SieveVacation) => void>()
  const view = render(VacationForm, {
    props: {
      saved,
      addresses: ['me@example.org', 'alias@example.org'],
      dates: true,
      scriptActive: true,
      today: '2026-12-24',
      ...props,
    },
    events: { save: (e: CustomEvent<SieveVacation>) => onSave(e.detail) },
  })
  return { ...view, onSave }
}

describe('VacationForm', () => {
  it('says the reply is active on the server now', () => {
    renderForm()
    expect(screen.getByText(/^Active on the server now, until/)).toBeInTheDocument()
  })

  it('says when a reply is still to come, has ended or is not running', async () => {
    const { rerender } = renderForm({ today: '2026-12-01' })
    expect(screen.getByText(/^Scheduled: replies start on/)).toBeInTheDocument()
    await rerender({ today: '2027-02-01' })
    expect(screen.getByText(/^Ended on/)).toBeInTheDocument()
    await rerender({ scriptActive: false })
    expect(screen.getByText('Saved, but not active on the server.')).toBeInTheDocument()
    await rerender({ saved: null })
    expect(screen.getByText('Off on the server.')).toBeInTheDocument()
  })

  it('saves nothing until something changed', () => {
    renderForm()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('hands the edited reply to the page on save', async () => {
    const { onSave } = renderForm()
    await fireEvent.input(screen.getByRole('textbox', { name: 'Subject' }), { target: { value: 'On holiday' } })
    await userEvent.click(screen.getByRole('checkbox', { name: 'alias@example.org' }))
    await fireEvent.change(screen.getByRole('spinbutton', { name: 'Days between replies' }), { target: { value: '3' } })
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onSave.mock.calls[0][0]).toEqual({
      ...saved,
      subject: 'On holiday',
      days: 3,
      addresses: ['me@example.org', 'alias@example.org'],
    })
  })

  it('switches the reply off', async () => {
    const { onSave } = renderForm()
    await userEvent.click(screen.getByRole('switch', { name: 'Send automatic replies' }))
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave.mock.calls[0][0].enabled).toBe(false)
  })

  it('refuses an empty message', async () => {
    const { onSave } = renderForm()
    await fireEvent.input(screen.getByRole('textbox', { name: 'Message' }), { target: { value: '  ' } })
    expect(screen.getByText('Write the message to send.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(onSave).not.toHaveBeenCalled()
  })

  it('discards edits back to what is on the server', async () => {
    renderForm()
    const subject = screen.getByRole('textbox', { name: 'Subject' })
    await fireEvent.input(subject, { target: { value: 'Changed' } })
    await userEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(subject).toHaveValue('Away')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('leaves the dates out on a server that cannot bound them', () => {
    renderForm({ dates: false })
    expect(screen.queryByText('First day')).not.toBeInTheDocument()
    expect(screen.getByText('This mail server cannot limit replies to dates.')).toBeInTheDocument()
  })

  it('starts a new reply for the first address only', async () => {
    const { onSave } = renderForm({ saved: null })
    expect(screen.getByRole('checkbox', { name: 'me@example.org' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'alias@example.org' })).not.toBeChecked()
    await fireEvent.input(screen.getByRole('textbox', { name: 'Message' }), { target: { value: 'Away.' } })
    await userEvent.click(screen.getByRole('switch', { name: 'Send automatic replies' }))
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave.mock.calls[0][0]).toMatchObject({ enabled: true, body: 'Away.', addresses: ['me@example.org'], days: 7 })
  })
})
