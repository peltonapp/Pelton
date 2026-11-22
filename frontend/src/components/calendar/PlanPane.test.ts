import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import PlanPane from './PlanPane.svelte'
import type { Task, TaskPlan } from '../../lib/types'
import { expectAxeClean } from '../../test/axe'

function task(id: number, over: Partial<Task>): Task {
  return {
    id,
    listId: 1,
    listName: '',
    listColor: '',
    uid: `t${id}`,
    title: `Task ${id}`,
    notes: '',
    url: '',
    due: '',
    start: '',
    timeZone: '',
    done: false,
    completed: '',
    status: '',
    percent: 0,
    priority: 0,
    rrule: '',
    reminders: [],
    parent: '',
    categories: [],
    mailLinks: [],
    contacts: [],
    eventUids: [],
    sortOrder: 0,
    estimate: 0,
    created: '',
    readOnly: false,
    pending: false,
    conflict: false,
    syncError: '',
    ...over,
  }
}

const now = new Date(2026, 10, 14, 10)

function mount(tasks: Task[], plans: TaskPlan[] = [], events: Record<string, (e: CustomEvent) => void> = {}) {
  return render(PlanPane, {
    props: { tasks, plans: new Map(plans.map((p) => [p.taskUid, p])), proposals: [], defaultMinutes: 30, now },
    events,
  })
}

describe('PlanPane', () => {
  it('lists the open tasks earliest due first, with their planned and estimated time', async () => {
    mount(
      [
        task(1, { title: 'Later', due: '2026-11-20' }),
        task(2, { title: 'Overdue', due: '2026-11-10', estimate: 120 }),
        task(3, { title: 'Done', done: true }),
        task(4, { title: 'No date' }),
      ],
      [{ taskUid: 't2', plannedMinutes: 30, aheadMinutes: 30, future: 1 }],
    )
    const names = screen.getAllByRole('listitem').map((li) => li.querySelector('.name')?.textContent)
    expect(names).toEqual(['Overdue', 'Later', 'No date'])
    const overdue = screen.getAllByRole('listitem')[0]
    expect(within(overdue).getByText('30 min of 2 h planned')).toBeInTheDocument()
    expect(within(overdue).getByText(/Overdue since/)).toBeInTheDocument()
    expect(within(screen.getAllByRole('listitem')[1]).getByText('Not planned yet')).toBeInTheDocument()
    await expectAxeClean()
  })

  it('offers Find time and the task menu from the keyboard', async () => {
    const findTime = vi.fn()
    mount([task(1, { title: 'Report' })], [], { findTime })
    screen.getByRole('button', { name: 'Find time for Report' }).focus()
    await userEvent.keyboard('{Enter}')
    expect(findTime).toHaveBeenCalledTimes(1)
    expect(findTime.mock.calls[0][0].detail.title).toBe('Report')
    expect(screen.getByRole('button', { name: 'More for Report' })).toBeInTheDocument()
  })
})
