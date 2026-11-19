// projecticons.ts is the set of icons a project can be given, by the name the
// backend stores. an unknown or empty name draws the default.

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

/** The icons a project can have, in picker order. The first is the default. */
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
