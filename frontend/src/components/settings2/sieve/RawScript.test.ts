import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import RawScript from './RawScript.svelte'

const source = 'require "fileinto";\nfileinto "Lists";\n'

function renderRaw(props: Record<string, unknown> = {}) {
  const onCheck = vi.fn<(text: string) => void>()
  const onSave = vi.fn<(text: string) => void>()
  const view = render(RawScript, {
    props: { source, ...props },
    events: {
      check: (e: CustomEvent<string>) => onCheck(e.detail),
      save: (e: CustomEvent<string>) => onSave(e.detail),
    },
  })
  return { ...view, onCheck, onSave }
}

describe('RawScript', () => {
  it('pins the server error to the line it names', () => {
    const { container } = renderRaw({ problem: 'unknown command bogus', line: 2 })
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('Line 2')
    expect(alert).toHaveTextContent('unknown command bogus')
    expect(container.querySelector('.gutter .bad')?.textContent).toBe('2')
    expect(screen.getByRole('textbox', { name: 'Sieve script' })).toHaveAttribute('aria-invalid', 'true')
  })

  it('shows an error without a line under the editor', () => {
    renderRaw({ problem: 'script too large', line: 0 })
    expect(screen.getByRole('alert')).toHaveTextContent('script too large')
  })

  it('checks and saves the edited text', async () => {
    const { onCheck, onSave } = renderRaw()
    expect(screen.getByRole('button', { name: 'Save script' })).toBeDisabled()
    await fireEvent.input(screen.getByRole('textbox', { name: 'Sieve script' }), { target: { value: 'keep;\n' } })
    await userEvent.click(screen.getByRole('button', { name: 'Check' }))
    expect(onCheck).toHaveBeenCalledWith('keep;\n')
    await userEvent.click(screen.getByRole('button', { name: 'Save script' }))
    expect(onSave).toHaveBeenCalledWith('keep;\n')
  })
})
