// projectpicker.ts files mail under a project from wherever mail is: the
// add-to-project shortcut, the command palette and a drop on the Projects
// entry all open the palette on a list of projects to pick from. it also
// holds the suggestion the reader is showing, so the shortcut can confirm it
// with one key.

import { get, writable } from 'svelte/store'
import { IconLayoutKanban, IconPlus } from '@tabler/icons-svelte'
import { t } from './i18n'
import { projectColor } from './projectlogic'
import type { CommandStep } from './commands'
import { openPaletteStep } from '../stores/palette'
import { activeProjects, fileMail, newProjectRequest } from '../stores/projects'
import { openSpace } from '../stores/space'
import { toastInfo } from '../stores/toast'

/** The suggestion the reader shows: file this conversation under this project. */
export interface ShownSuggestion {
  threadId: number
  projectId: number
  messageIds: number[]
}

/** The suggestion on screen in the reader, or null. */
export const shownSuggestion = writable<ShownSuggestion | null>(null)

/** The palette step that files these messages' conversations under a project. */
export function fileMailStep(messageIds: number[], tr: (key: string) => string = get(t)): CommandStep {
  return {
    label: tr('projects.addTo'),
    placeholder: tr('projects.pickPlaceholder'),
    items: [
      ...get(activeProjects).map((p) => ({
        id: `projects:file:${p.id}`,
        group: 'projects' as const,
        label: p.name,
        keywords: p.description,
        icon: IconLayoutKanban,
        swatch: projectColor(p),
        run: () => void fileMail(p.id, messageIds),
      })),
      {
        id: 'projects:file:new',
        group: 'projects' as const,
        label: tr('projects.new'),
        icon: IconPlus,
        run: () => {
          newProjectRequest.set(true)
          openSpace('projects')
        },
      },
    ],
  }
}

/**
 * Asks which project these messages go to. The suggestion the reader shows
 * for the same conversation is taken without asking.
 */
export function pickProjectFor(messageIds: number[], threadId = 0): void {
  if (messageIds.length === 0) {
    toastInfo(get(t)('app.toast.openMessageFirst'))
    return
  }
  const shown = get(shownSuggestion)
  if (shown && threadId !== 0 && shown.threadId === threadId) {
    void fileMail(shown.projectId, shown.messageIds)
    shownSuggestion.set(null)
    return
  }
  openPaletteStep(fileMailStep(messageIds))
}
