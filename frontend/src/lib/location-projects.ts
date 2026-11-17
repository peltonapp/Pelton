// location-projects.ts lets Projects take part in back and forward: the open
// project, the tab it is on and the card open on its board.

import { get } from 'svelte/store'
import { registerLocationProvider, oneSideEmpty, type ProjectsLocation } from './locations'
import {
  board,
  closeProject,
  loadProjects,
  openCard,
  openCardUid,
  openProject,
  openProjectId,
  projectTab,
  projectTabs,
  projects,
  type ProjectTab,
} from '../stores/projects'
import { openSpace } from '../stores/space'
import { t } from './i18n'

function isTab(tab: string | undefined): tab is ProjectTab {
  return (projectTabs as string[]).includes(tab ?? '')
}

registerLocationProvider('projects', {
  watch: [openProjectId, projectTab, openCardUid, projects, board],
  describe(): ProjectsLocation {
    const id = get(openProjectId)
    if (id === null) {
      return { space: 'projects' }
    }
    const tab = get(projectTab)
    const card = tab === 'board' ? get(openCardUid) : null
    return { space: 'projects', project: id, tab, card: card ?? undefined }
  },
  label(loc) {
    if (loc.project === undefined) {
      return ''
    }
    const name = get(projects).find((p) => p.id === loc.project)?.name ?? ''
    const b = get(board)
    const card = loc.card && b?.project.id === loc.project ? b.cards.find((c) => c.task.uid === loc.card)?.task.title : undefined
    if (card) {
      return name ? `${name}: ${card}` : card
    }
    if (isTab(loc.tab) && loc.tab !== 'board' && name) {
      return `${name}: ${get(t)(`projects.tab.${loc.tab}`)}`
    }
    return name
  },
  async restore(loc) {
    if (loc.project === undefined) {
      closeProject()
      openSpace('projects')
      return true
    }
    if (get(projects).length === 0) {
      await loadProjects()
    }
    if (!get(projects).some((p) => p.id === loc.project)) {
      return false
    }
    if (loc.card) {
      openCard(loc.project, loc.card)
    } else {
      openProject(loc.project, isTab(loc.tab) ? loc.tab : 'board')
    }
    return true
  },
  refines(from, to) {
    return from.project === to.project && from.tab === to.tab && oneSideEmpty(from.card, to.card)
  },
})
