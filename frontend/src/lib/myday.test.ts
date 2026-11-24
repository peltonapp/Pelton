import { describe, expect, it } from 'vitest'
import { myDayFits, myDayTasks } from './myday'
import type { Task } from './types'

// Monday 16 November 2026, 10:00.
const now = new Date(2026, 10, 16, 10)

let nextId = 1
function task(title: string, due: string, done = false): Task {
  return { id: nextId++, title, due, done } as Task
}

describe('myDayFits', () => {
  it('folds the panel away when the reader would get too narrow', () => {
    expect(myDayFits(1600, 264, 380, false, 300)).toBe(true)
    expect(myDayFits(1300, 264, 380, false, 300)).toBe(false)
  })

  it('does not count the list when it sits above the reader', () => {
    expect(myDayFits(1100, 264, 380, true, 300)).toBe(true)
    expect(myDayFits(1100, 264, 380, false, 300)).toBe(false)
  })

  it('fits before the width is measured', () => {
    expect(myDayFits(0, 264, 380, false, 300)).toBe(true)
  })
})

describe('myDayTasks', () => {
  const list = [
    task('later today', new Date(2026, 10, 16, 15).toISOString()),
    task('today, any time', '2026-11-16'),
    task('this morning, missed', new Date(2026, 10, 16, 8).toISOString()),
    task('done today', '2026-11-16', true),
    task('last week', '2026-11-09'),
    task('yesterday', '2026-11-15'),
    task('done long ago', '2026-11-01', true),
    task('tomorrow', '2026-11-17'),
    task('no date', ''),
  ]

  it('lists the overdue tasks first, then today with the done ones last', () => {
    const shown = myDayTasks(list, new Date(2026, 10, 16), now)
    expect(shown.map((x) => x.task.title)).toEqual(['last week', 'yesterday', 'this morning, missed', 'later today', 'today, any time', 'done today'])
    expect(shown.map((x) => x.overdue)).toEqual([true, true, true, false, false, false])
  })

  it('lists only what is due on another day, nothing overdue', () => {
    const shown = myDayTasks(list, new Date(2026, 10, 17), now)
    expect(shown.map((x) => x.task.title)).toEqual(['tomorrow'])
    expect(shown[0].overdue).toBe(false)
  })

  it('is empty with nothing due', () => {
    expect(myDayTasks([task('no date', '')], new Date(2026, 10, 16), now)).toEqual([])
  })
})
