// projecticons.ts is the set of icons a project can be given. a project stores
// a name and, for a tabler icon, its geometry; a name without geometry is one
// of the older phosphor icons below, and an unknown or empty name draws the
// default.

import type { IconNode } from './menuactions'

import kanban from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
import folder from '@phosphor-icons/core/assets/duotone/folder-simple-duotone.svg?raw'
import briefcase from '@phosphor-icons/core/assets/duotone/briefcase-duotone.svg?raw'
import rocket from '@phosphor-icons/core/assets/duotone/rocket-launch-duotone.svg?raw'
import target from '@phosphor-icons/core/assets/duotone/target-duotone.svg?raw'
import lightbulb from '@phosphor-icons/core/assets/duotone/lightbulb-duotone.svg?raw'
import book from '@phosphor-icons/core/assets/duotone/book-open-duotone.svg?raw'
import house from '@phosphor-icons/core/assets/duotone/house-line-duotone.svg?raw'
import code from '@phosphor-icons/core/assets/duotone/code-duotone.svg?raw'
import globe from '@phosphor-icons/core/assets/duotone/globe-duotone.svg?raw'
import wrench from '@phosphor-icons/core/assets/duotone/wrench-duotone.svg?raw'
import palette from '@phosphor-icons/core/assets/duotone/palette-duotone.svg?raw'
import flag from '@phosphor-icons/core/assets/duotone/flag-banner-duotone.svg?raw'
import heart from '@phosphor-icons/core/assets/duotone/heart-duotone.svg?raw'
import cube from '@phosphor-icons/core/assets/duotone/cube-duotone.svg?raw'
import chart from '@phosphor-icons/core/assets/duotone/chart-line-up-duotone.svg?raw'

/** A tabler icon offered as a quick pick, with the geometry it is stored by. */
export interface SuggestedProjectIcon {
  name: string
  nodes: IconNode[]
}

/** The quick picks of the project dialog, one for each icon projects had before the full set. */
export const suggestedProjectIcons: SuggestedProjectIcon[] = [
  { name: 'layout-kanban', nodes: [['path', { d: 'M4 4l6 0' }], ['path', { d: 'M14 4l6 0' }], ['path', { d: 'M4 10a2 2 0 0 1 2 -2h2a2 2 0 0 1 2 2v8a2 2 0 0 1 -2 2h-2a2 2 0 0 1 -2 -2l0 -8' }], ['path', { d: 'M14 10a2 2 0 0 1 2 -2h2a2 2 0 0 1 2 2v2a2 2 0 0 1 -2 2h-2a2 2 0 0 1 -2 -2l0 -2' }]] },
  { name: 'folder', nodes: [['path', { d: 'M5 4h4l3 3h7a2 2 0 0 1 2 2v8a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-11a2 2 0 0 1 2 -2' }]] },
  { name: 'briefcase', nodes: [['path', { d: 'M3 9a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v9a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2l0 -9' }], ['path', { d: 'M8 7v-2a2 2 0 0 1 2 -2h4a2 2 0 0 1 2 2v2' }], ['path', { d: 'M12 12l0 .01' }], ['path', { d: 'M3 13a20 20 0 0 0 18 0' }]] },
  { name: 'rocket', nodes: [['path', { d: 'M4 13a8 8 0 0 1 7 7a6 6 0 0 0 3 -5a9 9 0 0 0 6 -8a3 3 0 0 0 -3 -3a9 9 0 0 0 -8 6a6 6 0 0 0 -5 3' }], ['path', { d: 'M7 14a6 6 0 0 0 -3 6a6 6 0 0 0 6 -3' }], ['path', { d: 'M14 9a1 1 0 1 0 2 0a1 1 0 1 0 -2 0' }]] },
  { name: 'target', nodes: [['path', { d: 'M11 12a1 1 0 1 0 2 0a1 1 0 1 0 -2 0' }], ['path', { d: 'M7 12a5 5 0 1 0 10 0a5 5 0 1 0 -10 0' }], ['path', { d: 'M3 12a9 9 0 1 0 18 0a9 9 0 1 0 -18 0' }]] },
  { name: 'bulb', nodes: [['path', { d: 'M3 12h1m8 -9v1m8 8h1m-15.4 -6.4l.7 .7m12.1 -.7l-.7 .7' }], ['path', { d: 'M9 16a5 5 0 1 1 6 0a3.5 3.5 0 0 0 -1 3a2 2 0 0 1 -4 0a3.5 3.5 0 0 0 -1 -3' }], ['path', { d: 'M9.7 17l4.6 0' }]] },
  { name: 'book', nodes: [['path', { d: 'M3 19a9 9 0 0 1 9 0a9 9 0 0 1 9 0' }], ['path', { d: 'M3 6a9 9 0 0 1 9 0a9 9 0 0 1 9 0' }], ['path', { d: 'M3 6l0 13' }], ['path', { d: 'M12 6l0 13' }], ['path', { d: 'M21 6l0 13' }]] },
  { name: 'home', nodes: [['path', { d: 'M5 12l-2 0l9 -9l9 9l-2 0' }], ['path', { d: 'M5 12v7a2 2 0 0 0 2 2h10a2 2 0 0 0 2 -2v-7' }], ['path', { d: 'M9 21v-6a2 2 0 0 1 2 -2h2a2 2 0 0 1 2 2v6' }]] },
  { name: 'code', nodes: [['path', { d: 'M7 8l-4 4l4 4' }], ['path', { d: 'M17 8l4 4l-4 4' }], ['path', { d: 'M14 4l-4 16' }]] },
  { name: 'world', nodes: [['path', { d: 'M3 12a9 9 0 1 0 18 0a9 9 0 0 0 -18 0' }], ['path', { d: 'M3.6 9h16.8' }], ['path', { d: 'M3.6 15h16.8' }], ['path', { d: 'M11.5 3a17 17 0 0 0 0 18' }], ['path', { d: 'M12.5 3a17 17 0 0 1 0 18' }]] },
  { name: 'tool', nodes: [['path', { d: 'M7 10h3v-3l-3.5 -3.5a6 6 0 0 1 8 8l6 6a2 2 0 0 1 -3 3l-6 -6a6 6 0 0 1 -8 -8l3.5 3.5' }]] },
  { name: 'palette', nodes: [['path', { d: 'M12 21a9 9 0 0 1 0 -18c4.97 0 9 3.582 9 8c0 1.06 -.474 2.078 -1.318 2.828c-.844 .75 -1.989 1.172 -3.182 1.172h-2.5a2 2 0 0 0 -1 3.75a1.3 1.3 0 0 1 -1 2.25' }], ['path', { d: 'M7.5 10.5a1 1 0 1 0 2 0a1 1 0 1 0 -2 0' }], ['path', { d: 'M11.5 7.5a1 1 0 1 0 2 0a1 1 0 1 0 -2 0' }], ['path', { d: 'M15.5 10.5a1 1 0 1 0 2 0a1 1 0 1 0 -2 0' }]] },
  { name: 'flag', nodes: [['path', { d: 'M5 5a5 5 0 0 1 7 0a5 5 0 0 0 7 0v9a5 5 0 0 1 -7 0a5 5 0 0 0 -7 0v-9' }], ['path', { d: 'M5 21v-7' }]] },
  { name: 'heart', nodes: [['path', { d: 'M19.5 12.572l-7.5 7.428l-7.5 -7.428a5 5 0 1 1 7.5 -6.566a5 5 0 1 1 7.5 6.572' }]] },
  { name: 'cube', nodes: [['path', { d: 'M21 16.008v-8.018a1.98 1.98 0 0 0 -1 -1.717l-7 -4.008a2.016 2.016 0 0 0 -2 0l-7 4.008c-.619 .355 -1 1.01 -1 1.718v8.018c0 .709 .381 1.363 1 1.717l7 4.008a2.016 2.016 0 0 0 2 0l7 -4.008c.619 -.355 1 -1.01 1 -1.718' }], ['path', { d: 'M12 22v-10' }], ['path', { d: 'M12 12l8.73 -5.04' }], ['path', { d: 'M3.27 6.96l8.73 5.04' }]] },
  { name: 'chart-line', nodes: [['path', { d: 'M4 19l16 0' }], ['path', { d: 'M4 15l4 -6l4 2l4 -5l4 4' }]] },
]

/**
 * Reads the geometry a project stores as json, undefined for none or for
 * anything that is not a list of [tag, attributes] nodes.
 */
export function parseIconNodes(json: string | undefined): IconNode[] | undefined {
  if (!json) {
    return undefined
  }
  try {
    const parsed: unknown = JSON.parse(json)
    if (
      Array.isArray(parsed) &&
      parsed.length > 0 &&
      parsed.every((n) => Array.isArray(n) && n.length === 2 && typeof n[0] === 'string' && typeof n[1] === 'object' && n[1] !== null)
    ) {
      return parsed as IconNode[]
    }
  } catch {
    // a stored value that does not parse draws the name or the default
  }
  return undefined
}

/** The icons a project could have before the Tabler set, in the order the old picker showed them. The first is the default. */
export const projectIcons: { name: string; svg: string }[] = [
  { name: 'kanban', svg: kanban },
  { name: 'folder', svg: folder },
  { name: 'briefcase', svg: briefcase },
  { name: 'rocket', svg: rocket },
  { name: 'target', svg: target },
  { name: 'lightbulb', svg: lightbulb },
  { name: 'book', svg: book },
  { name: 'house', svg: house },
  { name: 'code', svg: code },
  { name: 'globe', svg: globe },
  { name: 'wrench', svg: wrench },
  { name: 'palette', svg: palette },
  { name: 'flag', svg: flag },
  { name: 'heart', svg: heart },
  { name: 'cube', svg: cube },
  { name: 'chart', svg: chart },
]

/** The svg of a project's icon. */
export function projectIcon(name: string): string {
  return (projectIcons.find((i) => i.name === name) ?? projectIcons[0]).svg
}
