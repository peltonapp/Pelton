// spaces.ts lists the top-level areas of the app once, so the rail, the sidebar
// footer switcher and the top bar tabs all show the same entries in the same
// order.

import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
import contactsIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
import type { Space } from './types'

/** One entry of the space switcher. */
export interface SpaceEntry {
  key: Space
  /** Localization key of the name. */
  labelKey: string
  /** Raw Phosphor svg. */
  icon: string
}

/** The areas the app offers, in display order. */
export const spaces: SpaceEntry[] = [
  { key: 'mail', labelKey: 'space.mail', icon: mailIcon },
  { key: 'contacts', labelKey: 'space.contacts', icon: contactsIcon },
]
