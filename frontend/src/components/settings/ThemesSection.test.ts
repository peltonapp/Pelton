import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import type { ThemeInfo } from '../../lib/types'

const { themes } = vi.hoisted(() => {
  const theme = (id: string, hasCss: boolean): ThemeInfo => ({
    id,
    name: id,
    author: '',
    version: '',
    description: '',
    base: 'dark',
    hasCss,
    remoteRefs: [],
    preview: '',
    compatWarning: '',
    swatches: [],
  })
  return { themes: [theme('plain', false), theme('styled', true)] }
})

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listThemes: vi.fn().mockResolvedValue(themes),
}))

import ThemesSection from './ThemesSection.svelte'

describe('the themes list', () => {
  it('offers edit on every theme, including ones with custom css', async () => {
    render(ThemesSection)
    expect(await screen.findAllByRole('button', { name: 'Edit colors' })).toHaveLength(2)
  })
})
