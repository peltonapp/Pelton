// palette-projects.ts puts projects into the command palette: going to
// Projects, making a project, opening one by name, and filing the open or
// selected mail under one, which the app answers since it knows which mail
// that is. the entries work from any space.

import { get } from 'svelte/store'
import { IconLayoutKanban, IconMailForward, IconPlus } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { projectColor } from './projectlogic'
import type { CommandStep, PaletteCommand } from './commands'
import { openSpace } from '../stores/space'
import { menuHint } from '../stores/shortcuts'
import { activeProjects, fileMailRequest, newProjectRequest, openProject } from '../stores/projects'

// a step listing the active projects; picking one opens it.
function openStep(t: (key: string) => string): CommandStep {
  return {
    label: t('palette.projects.find'),
    placeholder: t('projects.pickPlaceholder'),
    items: get(activeProjects).map((p) => ({
      id: `projects:show:${p.id}`,
      group: 'projects' as const,
      label: p.name,
      keywords: p.description,
      icon: IconLayoutKanban,
      swatch: projectColor(p),
      run: () => openProject(p.id),
    })),
  }
}

registerPaletteSource('projects', (t) => {
  const out: PaletteCommand[] = [
    {
      id: 'projects:open',
      group: 'projects',
      label: t('palette.projects.open'),
      hint: menuHint('go-projects'),
      icon: IconLayoutKanban,
      run: () => openSpace('projects'),
    },
    {
      id: 'projects:new',
      group: 'projects',
      label: t('projects.new'),
      keywords: t('palette.projects.newKeywords'),
      icon: IconPlus,
      run: () => {
        newProjectRequest.set(true)
        openSpace('projects')
      },
    },
  ]
  if (get(activeProjects).length > 0) {
    out.push({
      id: 'projects:find',
      group: 'projects',
      label: t('palette.projects.find'),
      icon: IconLayoutKanban,
      run: () => openStep(t),
    })
  }
  out.push({
    id: 'projects:file',
    group: 'projects',
    label: t('projects.addTo'),
    keywords: t('palette.projects.fileKeywords'),
    hint: menuHint('add-to-project'),
    icon: IconMailForward,
    run: () => fileMailRequest.set(true),
  })
  return out
})
