// location-tasks.ts lets Tasks take part in back and forward: the task open on
// the Tasks screen.

import { get } from 'svelte/store'
import { registerLocationProvider, oneSideEmpty, type TasksLocation } from './locations'
import { loadTasks, openTaskId, taskFocus, tasks } from '../stores/tasks'
import { openSpace } from '../stores/space'

registerLocationProvider('tasks', {
  watch: [openTaskId, tasks],
  describe(): TasksLocation {
    const id = get(openTaskId)
    return { space: 'tasks', task: id ?? undefined }
  },
  label(loc) {
    return loc.task === undefined ? '' : (get(tasks).find((x) => x.id === loc.task)?.title ?? '')
  },
  async restore(loc) {
    if (loc.task !== undefined) {
      if (get(tasks).length === 0) {
        await loadTasks()
      }
      if (!get(tasks).some((x) => x.id === loc.task)) {
        return false
      }
      taskFocus.set(loc.task)
    }
    openSpace('tasks')
    return true
  },
  refines(from, to) {
    return oneSideEmpty(from.task, to.task)
  },
})
