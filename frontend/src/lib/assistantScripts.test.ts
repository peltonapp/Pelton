import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { scriptFixture } from '../test/scriptfixtures'
import { assistantToastKey, type ScriptProposal } from './assistantScripts'

const api = vi.hoisted(() => ({
  scriptProposal: vi.fn(),
  acceptScriptProposal: vi.fn(),
  rejectScriptProposal: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

const { default: AssistantReview } = await import('../components/automations/AssistantReview.svelte')

function proposal(over: Partial<ScriptProposal> = {}): ScriptProposal {
  return {
    scriptId: 1,
    exists: true,
    when: 'today',
    diff: [
      { op: ' ', text: 'name: Rules' },
      { op: '-', text: "print('two')" },
      { op: '+', text: "print('three')" },
    ],
    addedPermissions: ['mail.send'],
    removedPermissions: [],
    resetsApproval: true,
    problem: '',
    ...over,
  }
}

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  api.scriptProposal.mockResolvedValue(proposal({ exists: false, diff: [] }))
})

describe('assistantToastKey', () => {
  it('names a toast for what an assistant did, and none for what the user decided', () => {
    expect(assistantToastKey({ scriptId: 1, name: 'x', action: 'created' })).toBe('scripts.assistant.toast.created')
    expect(assistantToastKey({ scriptId: 1, name: 'x', action: 'proposed' })).toBe('scripts.assistant.toast.proposed')
    expect(assistantToastKey({ scriptId: 1, name: 'x', action: 'updated' })).toBe('scripts.assistant.toast.updated')
    expect(assistantToastKey({ scriptId: 1, name: 'x', action: 'accepted' })).toBeNull()
    expect(assistantToastKey({ scriptId: 1, name: 'x', action: 'rejected' })).toBeNull()
  })
})

describe('AssistantReview', () => {
  it('asks for a review of a script an assistant wrote', async () => {
    render(AssistantReview, { props: { script: scriptFixture({ id: 1, origin: 'assistant' }) } })
    expect(screen.getByText('Created by an assistant. Review the code and permissions before switching it on.')).toBeInTheDocument()
  })

  it('shows nothing for the user\'s own script without a proposal', async () => {
    render(AssistantReview, { props: { script: scriptFixture({ id: 1, origin: 'user' }) } })
    await vi.waitFor(() => expect(api.scriptProposal).toHaveBeenCalledWith(1))
    expect(screen.queryByText(/Created by an assistant/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Accept change' })).not.toBeInTheDocument()
  })

  it('shows a proposed change as a diff and accepts it', async () => {
    api.scriptProposal.mockResolvedValue(proposal())
    api.acceptScriptProposal.mockResolvedValue(scriptFixture({ id: 1 }))
    render(AssistantReview, { props: { script: scriptFixture({ id: 1, origin: 'user' }) } })
    expect(await screen.findByText(/An assistant proposed a change to this script/)).toBeInTheDocument()
    expect(screen.getByText('Adds permissions: mail.send')).toBeInTheDocument()
    expect(screen.getByText(/Accepting switches the script off/)).toBeInTheDocument()
    expect(screen.getByText("- print('two')", { exact: false })).toBeInTheDocument()
    expect(screen.getByText("+ print('three')", { exact: false })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Accept change' }))
    expect(api.acceptScriptProposal).toHaveBeenCalledWith(1)
    await vi.waitFor(() => expect(screen.queryByRole('button', { name: 'Accept change' })).not.toBeInTheDocument())
  })

  it('rejects a proposal that can no longer be applied', async () => {
    api.scriptProposal.mockResolvedValue(proposal({ problem: 'unknown trigger', diff: [] }))
    api.rejectScriptProposal.mockResolvedValue(undefined)
    render(AssistantReview, { props: { script: scriptFixture({ id: 1, origin: 'user' }) } })
    expect(await screen.findByRole('alert')).toHaveTextContent('unknown trigger')
    expect(screen.getByRole('button', { name: 'Accept change' })).toBeDisabled()
    await userEvent.click(screen.getByRole('button', { name: 'Reject' }))
    expect(api.rejectScriptProposal).toHaveBeenCalledWith(1)
  })
})
