import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  scriptLogDays: vi.fn(),
  setScriptLogDays: vi.fn(),
  scriptAskSeconds: vi.fn(),
  setScriptAskSeconds: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const { default: RunLog } = await import('./RunLog.svelte')
const store = await import('../../stores/scripts')

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  api.scriptLogDays.mockResolvedValue(30)
  api.scriptAskSeconds.mockResolvedValue(120)
  api.setScriptAskSeconds.mockResolvedValue(undefined)
  store.runLog.set([])
})

describe('RunLog question timeout', () => {
  it('shows the stored wait and saves a new one', async () => {
    render(RunLog)
    const select = screen.getByRole<HTMLSelectElement>('combobox', { name: 'Wait for an answer' })
    await waitFor(() => expect(select.value).toBe('120'))
    expect(select).toHaveAccessibleDescription(/takes its default or stops/)
    await userEvent.selectOptions(select, '5 minutes')
    expect(api.setScriptAskSeconds).toHaveBeenCalledWith(300)
  })

  it('keeps a stored wait that is not one of the choices', async () => {
    api.scriptAskSeconds.mockResolvedValue(45)
    render(RunLog)
    expect(await screen.findByRole('option', { name: '45 seconds' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '1 hour' })).toBeInTheDocument()
  })
})
