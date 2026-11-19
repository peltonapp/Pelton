import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  aiProviders: vi.fn(),
  aiProviderKinds: vi.fn(),
  saveAIProvider: vi.fn(),
  deleteAIProvider: vi.fn(),
  setDefaultAIProvider: vi.fn(),
  testAIProvider: vi.fn(),
  fetchAIModels: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/confirm', () => ({ askConfirm: vi.fn().mockResolvedValue(true) }))

import { expectAccessible } from '../../test/a11y'
import AiProviders from './AiProviders.svelte'
import AiAccess from './AiAccess.svelte'
import type { AIKind, AIProvider } from '../../lib/ai'

// jsdom has no Web Animations; a transition finishes as soon as it starts.
Element.prototype.animate = function animate() {
  const animation = { cancel(): void {}, finish(): void {}, pause(): void {}, play(): void {}, currentTime: 0 } as Animation
  let finished: Animation['onfinish'] = null
  Object.defineProperty(animation, 'onfinish', {
    get: () => finished,
    set: (fn: Animation['onfinish']) => {
      finished = fn
      setTimeout(() => fn?.call(animation, new Event('finish') as AnimationPlaybackEvent), 0)
    },
  })
  return animation
}

const kinds: AIKind[] = [
  { id: 'openai', label: 'OpenAI', baseUrl: 'https://api.openai.com/v1', key: 'required', model: '' },
  { id: 'ollama', label: 'Ollama', baseUrl: 'http://localhost:11434', key: 'optional', model: '' },
]

const cloud: AIProvider = {
  id: 'openai', name: 'OpenAI', kind: 'openai', baseUrl: 'https://api.openai.com/v1', model: 'm1', models: [], default: true, hasKey: true, local: false,
}
const box: AIProvider = {
  id: 'box', name: 'Box', kind: 'ollama', baseUrl: 'http://localhost:11434', model: '', models: [], default: false, hasKey: false, local: true,
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  api.aiProviderKinds.mockResolvedValue(kinds)
  api.aiProviders.mockResolvedValue([cloud, box])
  api.saveAIProvider.mockImplementation(async (input) => ({ ...box, ...input, id: input.id || 'new' }))
  api.deleteAIProvider.mockResolvedValue(undefined)
  api.setDefaultAIProvider.mockResolvedValue(undefined)
  api.testAIProvider.mockResolvedValue({ ok: true, message: '', local: true })
  api.fetchAIModels.mockResolvedValue(['llama', 'qwen'])
})

afterEach(() => {
  document.documentElement.removeAttribute('data-reduce-motion')
  vi.clearAllMocks()
})

describe('the AI providers list', () => {
  it('shows where each provider sends data and contacts nothing on opening', async () => {
    render(AiProviders)
    expect(await screen.findByText('Box')).toBeInTheDocument()
    expect(screen.getByText('Leaves this device')).toBeInTheDocument()
    expect(screen.getByText('On your device or network')).toBeInTheDocument()
    expect(screen.getByText('Default')).toBeInTheDocument()
    expectAccessible()
    expect(api.testAIProvider).not.toHaveBeenCalled()
    expect(api.fetchAIModels).not.toHaveBeenCalled()
  })

  it('makes a provider the default and deletes one', async () => {
    render(AiProviders)
    await screen.findByText('Box')
    await userEvent.click(screen.getByRole('button', { name: 'Make default' }))
    expect(api.setDefaultAIProvider).toHaveBeenCalledWith('box')
    await userEvent.click(screen.getByRole('button', { name: 'Delete Box' }))
    await waitFor(() => expect(api.deleteAIProvider).toHaveBeenCalledWith('box'))
  })

  it('adds a provider, fetching models and testing only when asked', async () => {
    render(AiProviders)
    await screen.findByText('Box')
    await userEvent.click(screen.getByRole('button', { name: 'Add provider' }))
    const dialog = await screen.findByRole('dialog')
    const save = within(dialog).getByRole('button', { name: 'Save' })
    expect(save).toBeDisabled()
    expect(within(dialog).getByText('Leaves this device')).toBeInTheDocument()

    await userEvent.type(within(dialog).getByLabelText('API key'), 'sk-1')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Fetch models' }))
    await waitFor(() => expect(api.fetchAIModels).toHaveBeenCalledOnce())
    expect(within(dialog).getByLabelText('Default model')).toHaveValue('llama')
    expect(api.testAIProvider).not.toHaveBeenCalled()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Test connection' }))
    expect((await within(dialog).findAllByText('It answered, from this device or your network.'))[0]).toBeInTheDocument()

    await userEvent.click(save)
    await waitFor(() => expect(api.saveAIProvider).toHaveBeenCalledOnce())
    expect(api.saveAIProvider.mock.calls[0][0]).toMatchObject({ kind: 'openai', key: 'sk-1', model: 'llama' })
  })
})

describe('the AI access picker', () => {
  it('declares a provider by where its data goes', async () => {
    const onChange = vi.fn()
    render(AiAccess, { props: { declared: ['ai', 'mail.read'] }, events: { change: (e: CustomEvent<string[]>) => onChange(e.detail) } })
    await userEvent.click(await screen.findByRole('checkbox', { name: 'Box' }))
    expect(onChange).toHaveBeenLastCalledWith(['ai', 'mail.read', 'ai_local:box'])
    await userEvent.click(screen.getByRole('checkbox', { name: 'OpenAI' }))
    expect(onChange).toHaveBeenLastCalledWith(['ai', 'mail.read', 'ai:openai'])
  })
})
