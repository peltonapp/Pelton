import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  chatDiscover: vi.fn(),
  chatLoginPassword: vi.fn(),
  chatLoginSSO: vi.fn(),
  chatCancelSSO: vi.fn(),
  chatLoginToken: vi.fn(),
  chatStatus: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import ChatSignIn from './ChatSignIn.svelte'

const server = {
  homeserver: 'https://matrix.example.org',
  serverName: 'example.org',
  user: 'arne',
  password: true,
  sso: false,
  token: false,
  providers: [],
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.chatDiscover.mockResolvedValue(server)
  api.chatLoginPassword.mockResolvedValue(undefined)
  api.chatStatus.mockResolvedValue({ state: 'signedOut', encryption: {} })
})

describe('ChatSignIn', () => {
  it('finds the server and signs in with the homeserver it found', async () => {
    const user = userEvent.setup()
    const signedin = vi.fn()
    render(ChatSignIn, { events: { signedin } })
    await user.type(screen.getByLabelText('Server or Matrix ID'), 'example.org')
    await user.click(screen.getByRole('button', { name: 'Continue' }))
    expect(api.chatDiscover).toHaveBeenCalledWith('example.org')
    const name = await screen.findByLabelText('User name')
    expect(name).toHaveValue('arne')
    await user.type(screen.getByLabelText('Password'), 'hunter2')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(api.chatLoginPassword).toHaveBeenCalledWith('https://matrix.example.org', 'arne', 'hunter2')
    await vi.waitFor(() => expect(signedin).toHaveBeenCalled())
  })

  it('shows a failed sign-in inline', async () => {
    api.chatLoginPassword.mockRejectedValue(new Error('chat: wrong user name or password'))
    const user = userEvent.setup()
    const signedin = vi.fn()
    render(ChatSignIn, { events: { signedin } })
    await user.type(screen.getByLabelText('Server or Matrix ID'), 'example.org')
    await user.click(screen.getByRole('button', { name: 'Continue' }))
    await user.type(await screen.findByLabelText('Password'), 'nope')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Wrong user name or password')
    expect(signedin).not.toHaveBeenCalled()
  })

  it('shows a lookup error and stays on the first step', async () => {
    api.chatDiscover.mockRejectedValue(new Error('chat: no server found'))
    const user = userEvent.setup()
    render(ChatSignIn)
    await user.type(screen.getByLabelText('Server or Matrix ID'), 'nowhere')
    await user.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('No server found')
    expect(screen.getByLabelText('Server or Matrix ID')).toBeInTheDocument()
  })
})
