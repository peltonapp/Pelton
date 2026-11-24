// scripticons.ts is the set of Phosphor icons a script can wear where it is
// placed: on a toolbar button, a row button or a result action. Icons are
// named, so a script stores and exports a name and every place draws the
// same icon from it. An unknown name falls back to the lightning bolt.

import lightning from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
import star from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
import copy from '@phosphor-icons/core/assets/duotone/copy-duotone.svg?raw'
import reply from '@phosphor-icons/core/assets/duotone/arrow-bend-up-left-duotone.svg?raw'
import note from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
import task from '@phosphor-icons/core/assets/duotone/check-square-duotone.svg?raw'
import tag from '@phosphor-icons/core/assets/duotone/tag-duotone.svg?raw'
import robot from '@phosphor-icons/core/assets/duotone/robot-duotone.svg?raw'
import wand from '@phosphor-icons/core/assets/duotone/magic-wand-duotone.svg?raw'
import sparkle from '@phosphor-icons/core/assets/duotone/sparkle-duotone.svg?raw'
import play from '@phosphor-icons/core/assets/duotone/play-duotone.svg?raw'
import rocket from '@phosphor-icons/core/assets/duotone/rocket-duotone.svg?raw'
import gear from '@phosphor-icons/core/assets/duotone/gear-duotone.svg?raw'
import funnel from '@phosphor-icons/core/assets/duotone/funnel-duotone.svg?raw'
import bookmark from '@phosphor-icons/core/assets/duotone/bookmark-simple-duotone.svg?raw'
import send from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
import translate from '@phosphor-icons/core/assets/duotone/translate-duotone.svg?raw'
import calendar from '@phosphor-icons/core/assets/duotone/calendar-plus-duotone.svg?raw'
import user from '@phosphor-icons/core/assets/duotone/user-duotone.svg?raw'
import archive from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
import folder from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
import flag from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
import envelope from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
import code from '@phosphor-icons/core/assets/duotone/code-duotone.svg?raw'
import terminal from '@phosphor-icons/core/assets/duotone/terminal-window-duotone.svg?raw'
import checklist from '@phosphor-icons/core/assets/duotone/list-checks-duotone.svg?raw'
import scissors from '@phosphor-icons/core/assets/duotone/scissors-duotone.svg?raw'
import file from '@phosphor-icons/core/assets/duotone/file-text-duotone.svg?raw'
import globe from '@phosphor-icons/core/assets/duotone/globe-duotone.svg?raw'
import bell from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
import kanban from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'

/** Every icon a script can pick, by name, in the order the picker shows them. */
export const scriptIcons: Readonly<Record<string, string>> = {
  lightning,
  star,
  sparkle,
  wand,
  robot,
  play,
  rocket,
  gear,
  tag,
  flag,
  bookmark,
  funnel,
  archive,
  folder,
  envelope,
  send,
  reply,
  copy,
  note,
  task,
  checklist,
  calendar,
  user,
  translate,
  globe,
  bell,
  kanban,
  file,
  scissors,
  code,
  terminal,
}

/** The name of the icon a script wears when it picked none. */
export const defaultScriptIcon = 'lightning'

/** The svg of a named script icon, or the default one's. */
export function scriptIcon(name: unknown): string {
  return (typeof name === 'string' && scriptIcons[name]) || scriptIcons[defaultScriptIcon]
}
