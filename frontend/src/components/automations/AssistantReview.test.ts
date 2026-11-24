import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import { scriptFixture } from '../../test/scriptfixtures'
import type { ScriptProposal } from '../../lib/assistantScripts'

const api = vi.hoisted(() => ({
  scriptProposal: vi.fn(),
  acceptScriptProposal: vi.fn(),
  rejectScriptProposal: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const { default: AssistantReview } = await import('./AssistantReview.svelte')
const { toasts } = await import('../../stores/toast')

function proposal(over: Partial<ScriptProposal> = {}): ScriptProposal {
  return {
    scriptId: 1,
    exists: true,
    when: 'today',
    diff: [{ op: '-', text: 'trigger: schedule.every {"minutes":60}' }, { op: '+', text: 'trigger: schedule.every {"minutes":1}' }],
    addedPermissions: [],
    removedPermissions: [],
    resetsApproval: true,
    resetReasons: ['triggerParams', 'placements'],
    problem: '',
    ...over,
  }
}

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  toasts.set([])
})

describe('AssistantReview', () => {
  it('says why accepting switches the script off', async () => {
    api.scriptProposal.mockResolvedValue(proposal())
    api.acceptScriptProposal.mockResolvedValue(scriptFixture({ enabled: false }))
    render(AssistantReview, { props: { script: scriptFixture({ enabled: true }) } })
    expect(await screen.findByText(/Accepting switches the script off/)).toBeInTheDocument()
    expect(screen.getByText('It changes: the settings of what starts it, such as the folder or the schedule, where it is offered in the app.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Accept change' }))
    expect(api.acceptScriptProposal).toHaveBeenCalledWith(1)
    expect(get(toasts).map((x) => x.message)).toContain('Change accepted. The script is off until you approve it again.')
  })

  it('keeps quiet about approval for a change that keeps it', async () => {
    api.scriptProposal.mockResolvedValue(proposal({ resetsApproval: false, resetReasons: [] }))
    api.acceptScriptProposal.mockResolvedValue(scriptFixture({ enabled: true }))
    render(AssistantReview, { props: { script: scriptFixture({ enabled: true }) } })
    await screen.findByRole('button', { name: 'Accept change' })
    expect(screen.queryByText(/Accepting switches the script off/)).toBeNull()
    expect(screen.queryByText(/It changes:/)).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Accept change' }))
    expect(get(toasts).map((x) => x.message)).toContain('Change accepted.')
  })
})
