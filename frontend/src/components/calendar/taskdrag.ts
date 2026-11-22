// taskdrag.ts is dragging a task from the plan pane onto the time grid. Like
// dragging mail it is pointer based: the pane arms a drag on press, the drag
// starts once the pointer moved far enough, the grid shows where the block
// would go and takes the drop. Escape or leaving the window cancels.

import { get, writable } from 'svelte/store'
import type { Task } from '../../lib/types'

/** A task being dragged, and how long its block would be. */
export interface TaskDrag {
  task: Task
  minutes: number
  /** Pointer position in client pixels. */
  x: number
  y: number
}

/** The drag in progress, or null. */
export const taskDrag = writable<TaskDrag | null>(null)

/** A place that takes a dropped task: it returns true when the drop was its. */
export type TaskDropTarget = (drag: TaskDrag) => boolean

const targets = new Set<TaskDropTarget>()
const startDistance = 6

/** Registers a place that takes dropped tasks. Returns the function that removes it. */
export function onTaskDrop(target: TaskDropTarget): () => void {
  targets.add(target)
  return () => targets.delete(target)
}

/**
 * Arms a drag from a press on a task: once the pointer moves far enough the
 * drag starts, and releasing it hands the task to the place under the
 * pointer. A plain click stays a click.
 */
export function armTaskDrag(event: PointerEvent, task: Task, minutes: number): void {
  if (event.button !== 0 || event.pointerType === 'touch') {
    return
  }
  const startX = event.clientX
  const startY = event.clientY
  let started = false

  const swallowClick = (e: MouseEvent): void => {
    e.stopPropagation()
    e.preventDefault()
  }
  const stop = (): void => {
    window.removeEventListener('pointermove', onMove, true)
    window.removeEventListener('pointerup', onUp, true)
    window.removeEventListener('keydown', onKey, true)
    window.removeEventListener('blur', cancel)
    document.body.classList.remove('task-dragging')
    taskDrag.set(null)
  }
  const cancel = (): void => stop()
  const onMove = (e: PointerEvent): void => {
    if (!started && Math.hypot(e.clientX - startX, e.clientY - startY) < startDistance) {
      return
    }
    if (!started) {
      started = true
      document.body.classList.add('task-dragging')
      window.addEventListener('click', swallowClick, { capture: true, once: true })
    }
    taskDrag.set({ task, minutes, x: e.clientX, y: e.clientY })
  }
  const onUp = (e: PointerEvent): void => {
    const drag = get(taskDrag)
    stop()
    if (started && drag) {
      const dropped = { ...drag, x: e.clientX, y: e.clientY }
      for (const target of targets) {
        if (target(dropped)) {
          break
        }
      }
      setTimeout(() => window.removeEventListener('click', swallowClick, true), 0)
    }
  }
  const onKey = (e: KeyboardEvent): void => {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      stop()
    }
  }
  window.addEventListener('pointermove', onMove, true)
  window.addEventListener('pointerup', onUp, true)
  window.addEventListener('keydown', onKey, true)
  window.addEventListener('blur', cancel)
}
