import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  setSetting: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  setSetting: api.setSetting,
}))

vi.mock('../../settings/NetworkSection.svelte', async () => ({ default: (await import('../../common/Ph.svelte')).default }))

import Privacy from './Privacy.svelte'
import { prefs } from '../../../stores/prefs'

beforeEach(() => {
  api.setSetting.mockReset().mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, cleanLinks: false }))
})

describe('Privacy page link cleaning', () => {
  it('is off by default and says it can break sign-in links', () => {
    render(Privacy)
    expect(screen.getByRole('switch', { name: 'Clean links opened from mail' })).not.toBeChecked()
    expect(screen.getByText(/sign-in, verification or unsubscribe links are never changed/)).toBeInTheDocument()
  })

  it('saves the choice', async () => {
    render(Privacy)
    await userEvent.click(screen.getByRole('switch', { name: 'Clean links opened from mail' }))
    expect(get(prefs).cleanLinks).toBe(true)
    expect(api.setSetting).toHaveBeenCalledWith('clean_links', 'true')
  })
})
