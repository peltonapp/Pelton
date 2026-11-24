import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Project, ProjectDraft } from '../../lib/types'

const api = vi.hoisted(() => ({
  createProject: vi.fn(),
  updateProject: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('tabler-nodes-outline', () => ({
  default: {
    rocket: [['path', { d: 'M5 5' }]],
    'rocket-off': [['path', { d: 'M6 6' }]],
    mail: [['path', { d: 'M2 2' }]],
  },
}))

Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}
Element.prototype.getAnimations ??= function () {
  return []
}

import ProjectDialog from './ProjectDialog.svelte'
import { suggestedProjectIcons } from '../../lib/projecticons'

function project(over: Partial<Project> = {}): Project {
  return {
    id: 4,
    uid: 'p4',
    name: 'Garden',
    color: '',
    icon: '',
    iconNodes: '',
    description: '',
    status: 'active',
    startDate: '',
    dueDate: '',
    position: 0,
    listId: 0,
    createdAt: '',
    updatedAt: '',
    archivedAt: '',
    cards: 0,
    done: 0,
    overdue: 0,
    nextDue: '',
    mail: 0,
    events: 0,
    notes: 0,
    people: [],
    ...over,
  }
}

function lastDraft(): ProjectDraft {
  const call = api.updateProject.mock.calls.at(-1) ?? api.createProject.mock.calls.at(-1)
  return call?.[0] as ProjectDraft
}

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  api.createProject.mockImplementation(async (d: ProjectDraft) => project({ ...d, id: 9, iconNodes: d.iconNodes }))
  api.updateProject.mockImplementation(async (d: ProjectDraft) => project({ ...d, iconNodes: d.iconNodes }))
})

describe('the icon of the project dialog', () => {
  it('offers a quick pick for each old icon and a default', () => {
    render(ProjectDialog, { props: { project: null } })
    expect(screen.getByRole('button', { name: 'Default', pressed: true })).toBeTruthy()
    expect(suggestedProjectIcons).toHaveLength(16)
    for (const s of suggestedProjectIcons) {
      expect(screen.getByRole('button', { name: s.name, pressed: false })).toBeTruthy()
    }
  })

  it('saves the name and geometry of a quick pick', async () => {
    const user = userEvent.setup()
    render(ProjectDialog, { props: { project: null } })
    await user.type(screen.getByRole('textbox', { name: 'Name' }), 'Launch')
    await user.click(screen.getByRole('button', { name: 'rocket' }))
    await user.click(screen.getByRole('button', { name: 'Create project' }))
    await waitFor(() => expect(api.createProject).toHaveBeenCalled())
    const draft = lastDraft()
    const rocket = suggestedProjectIcons.find((s) => s.name === 'rocket')
    expect(draft.icon).toBe('rocket')
    expect(JSON.parse(draft.iconNodes)).toEqual(rocket?.nodes)
  })

  it('searches the full set in a popover and saves the pick', async () => {
    const user = userEvent.setup()
    render(ProjectDialog, { props: { project: null } })
    await user.type(screen.getByRole('textbox', { name: 'Name' }), 'Mailing')
    await user.click(screen.getByRole('button', { name: 'Icon: Default' }))
    expect(screen.getByRole('dialog', { name: 'Icon' })).toBeTruthy()
    expect(await screen.findAllByRole('option')).toHaveLength(3)
    expect(screen.queryByText('Theme icons')).toBeNull()
    await user.keyboard('mai{Enter}')
    expect(screen.queryByRole('dialog', { name: 'Icon' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Icon: mail' })).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Create project' }))
    await waitFor(() => expect(api.createProject).toHaveBeenCalled())
    expect(lastDraft().icon).toBe('mail')
    expect(JSON.parse(lastDraft().iconNodes)).toEqual([['path', { d: 'M2 2' }]])
  })

  it('closes only the picker on Escape', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    render(ProjectDialog, { props: { project: null }, events: { close } })
    await user.click(screen.getByRole('button', { name: 'Icon: Default' }))
    await screen.findAllByRole('option')
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('combobox', { name: 'Search icons' })))
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Icon' })).toBeNull()
    expect(close).not.toHaveBeenCalled()
    expect(screen.getByRole('textbox', { name: 'Name' })).toBeTruthy()
  })

  it('goes back to the default and clears the geometry', async () => {
    const user = userEvent.setup()
    const stored = JSON.stringify([['path', { d: 'M5 5' }]])
    render(ProjectDialog, { props: { project: project({ icon: 'rocket', iconNodes: stored }) } })
    expect(screen.getByRole('button', { name: 'rocket', pressed: true })).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Default' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateProject).toHaveBeenCalled())
    expect(lastDraft().icon).toBe('')
    expect(lastDraft().iconNodes).toBe('')
  })

  it('keeps an older phosphor name as it is', async () => {
    const user = userEvent.setup()
    render(ProjectDialog, { props: { project: project({ icon: 'globe' }) } })
    expect(screen.getByRole('button', { name: 'Icon: globe' })).toBeTruthy()
    await user.type(screen.getByRole('textbox', { name: 'Name' }), ' 2')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateProject).toHaveBeenCalled())
    expect(lastDraft().icon).toBe('globe')
    expect(lastDraft().iconNodes).toBe('')
  })
})
