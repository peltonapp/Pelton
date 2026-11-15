// the icon of each built-in unified view, shared by the sidebar's list of them
// and the rail's pinned views.

import type { ComponentType } from 'svelte'
import {
  IconInbox,
  IconMail,
  IconFlag,
  IconSend,
  IconFile,
  IconArchive,
  IconAlertTriangle,
  IconTrash,
  IconFolder,
} from '@tabler/icons-svelte'

const icons: Record<string, ComponentType> = {
  inbox: IconInbox,
  unread: IconMail,
  flagged: IconFlag,
  sent: IconSend,
  drafts: IconFile,
  archive: IconArchive,
  junk: IconAlertTriangle,
  trash: IconTrash,
}

/** The icon for a unified view's key, a folder for a key not known here. */
export function unifiedViewIcon(key: string): ComponentType {
  return icons[key] ?? IconFolder
}
