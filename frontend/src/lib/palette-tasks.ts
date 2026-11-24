// palette-tasks.ts puts tasks into the command palette: going to Tasks and
// to its smart lists, making a task, making one from the open mail, opening
// a task by name, and the task settings. the entries work from any space; one
// that needs the Tasks screen opens it first.

import { get } from 'svelte/store'
import { IconChecklist, IconListCheck, IconMailForward, IconPlus, IconSettings } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { CommandStep, PaletteCommand } from './commands'
import { openSpace, runInSpace } from '../stores/space'
import { openSettingsAt } from '../stores/settingsnav'
import { menuHint } from '../stores/shortcuts'
import { taskFromMailRequest, tasks } from '../stores/tasks'
import { openTask } from './taskmail'

// a step listing the open tasks; picking one shows it.
function tasksStep(t: (key: string) => string): CommandStep {
  return {
    label: t('palette.tasks.find'),
    placeholder: t('palette.tasks.findPlaceholder'),
    items: get(tasks)
      .filter((x) => !x.done)
      .map((x) => ({
        id: `tasks:show:${x.id}`,
        group: 'tasks' as const,
        label: x.title || t('tasks.untitled'),
        icon: IconListCheck,
        swatch: x.listColor || undefined,
        run: () => openTask(x.id),
      })),
  }
}

registerPaletteSource('tasks', (t) => {
  const out: PaletteCommand[] = [
    {
      id: 'tasks:open',
      group: 'tasks',
      label: t('palette.tasks.open'),
      hint: menuHint('go-tasks'),
      icon: IconChecklist,
      run: () => openSpace('tasks'),
    },
    {
      id: 'tasks:new',
      group: 'tasks',
      label: t('tasks.newTask'),
      keywords: t('palette.tasks.newKeywords'),
      icon: IconPlus,
      run: () => runInSpace('tasks', 'compose'),
    },
    {
      id: 'tasks:from-mail',
      group: 'tasks',
      label: t('shortcut.createTask'),
      keywords: t('palette.tasks.fromMailKeywords'),
      hint: menuHint('create-task'),
      icon: IconMailForward,
      run: () => taskFromMailRequest.set(true),
    },
    {
      id: 'tasks:settings',
      group: 'tasks',
      label: t('palette.tasks.settings'),
      icon: IconSettings,
      run: () => openSettingsAt('tasks'),
    },
  ]
  if (get(tasks).some((x) => !x.done)) {
    out.push({
      id: 'tasks:find',
      group: 'tasks',
      label: t('palette.tasks.find'),
      icon: IconListCheck,
      run: () => tasksStep(t),
    })
  }
  return out
})
