// menuactions.ts is the pure, store-free source of truth for the in-app menu
// bar: the catalog of actions a menu item can trigger, the layout data model
// the user customizes, and the default layout that seeds it. The store
// (stores/menubar.ts) and the render/editor components build on this module;
// keeping it free of stores lets it be reasoned about and imported anywhere.

import type { ComponentType } from 'svelte'
import {
  IconInfoCircle,
  IconSettings,
  IconEyeOff,
  IconPower,
  IconX,
  IconPencil,
  IconFileTypePdf,
  IconRefresh,
  IconPlus,
  IconMailbox,
  IconAddressBook,
  IconArrowBackUp,
  IconMailOpened,
  IconMail,
  IconFlag,
  IconArchive,
  IconAlertTriangle,
  IconTrash,
  IconMaximize,
  IconBatteryEco,
  IconCornerUpLeft,
  IconCornerUpLeftDouble,
  IconArrowForwardUp,
  IconAlarmSnooze,
  IconDownload,
  IconMailX,
  IconSearch,
  IconBookmarkPlus,
  IconBookmarkEdit,
  IconStar,
  IconFolderSymlink,
  IconPalette,
  IconDownloadOff,
  IconFolderPlus,
  IconFolderX,
  IconTrashX,
  IconPin,
  IconBrush,
  IconCommand,
} from '@tabler/icons-svelte'
import type { ShortcutAction } from './shortcuts'
import { isMac } from './i18n'

// MenuActionId is every action the in-app menu bar can dispatch: the shortcut
// actions plus the menu-only ones App.svelte's dispatcher handles. It must stay
// in sync with App.svelte's MenuAction type.
export type MenuActionId =
  | ShortcutAction
  | 'about'
  | 'undo'
  | 'toggle-low-power'
  | 'open-mailboxes'
  | 'open-contacts'
  | 'hide-window'

// MenuActionDef describes one assignable action: how it labels and renders, and
// the constraints the bar applies (message-required, danger styling, platform).
export interface MenuActionDef {
  action: MenuActionId
  // labelKey is the i18n key for the item's default label.
  labelKey: string
  // iconName is the tabler name (Icon prefix stripped, kebab) a theme can
  // override; icon is the bundled fallback component.
  iconName: string
  icon: ComponentType
  // needsMessage items are disabled while no message is open.
  needsMessage?: boolean
  // danger items render in the danger color.
  danger?: boolean
  // macOnly items only exist on macOS (hidden from the bar and the editor
  // elsewhere, so a layout imported from a Mac stays valid).
  macOnly?: boolean
  // hint is a fixed shortcut hint; items without one look their hint up in the
  // live rebindable shortcut map under their action name.
  hint?: string
}

// the catalog: one entry per assignable action. Order here is the order the
// custom-entry action picker lists them in.
export const menuActionCatalog: MenuActionDef[] = [
  { action: 'about', labelKey: 'menu.about', iconName: 'info-circle', icon: IconInfoCircle },
  { action: 'preferences', labelKey: 'menu.preferences', iconName: 'settings', icon: IconSettings },
  { action: 'hide-window', labelKey: 'menu.hide', iconName: 'eye-off', icon: IconEyeOff, macOnly: true, hint: 'mod+h' },
  { action: 'quit', labelKey: 'menu.quit', iconName: 'power', icon: IconPower, hint: isMac ? 'mod+q' : undefined },
  { action: 'compose', labelKey: 'menu.compose', iconName: 'pencil', icon: IconPencil },
  { action: 'export-pdf', labelKey: 'menu.exportPdf', iconName: 'file-type-pdf', icon: IconFileTypePdf },
  { action: 'close-window', labelKey: 'menu.closeWindow', iconName: 'x', icon: IconX, hint: isMac ? 'mod+w' : undefined },
  { action: 'sync', labelKey: 'menu.sync', iconName: 'refresh', icon: IconRefresh },
  { action: 'add-mailbox', labelKey: 'menu.addMailbox', iconName: 'plus', icon: IconPlus },
  { action: 'open-mailboxes', labelKey: 'menu.manageMailboxes', iconName: 'mailbox', icon: IconMailbox },
  { action: 'open-contacts', labelKey: 'menu.contacts', iconName: 'address-book', icon: IconAddressBook },
  { action: 'search', labelKey: 'shortcut.search', iconName: 'search', icon: IconSearch },
  { action: 'command-palette', labelKey: 'shortcut.commandPalette', iconName: 'command', icon: IconCommand },
  { action: 'undo', labelKey: 'menu.undo', iconName: 'arrow-back-up', icon: IconArrowBackUp, hint: 'mod+z' },
  { action: 'reply', labelKey: 'shortcut.reply', iconName: 'corner-up-left', icon: IconCornerUpLeft, needsMessage: true },
  { action: 'reply-all', labelKey: 'shortcut.replyAll', iconName: 'corner-up-left-double', icon: IconCornerUpLeftDouble, needsMessage: true },
  { action: 'forward', labelKey: 'shortcut.forward', iconName: 'arrow-forward-up', icon: IconArrowForwardUp, needsMessage: true },
  { action: 'mark-read', labelKey: 'menu.markRead', iconName: 'mail-opened', icon: IconMailOpened, needsMessage: true },
  { action: 'mark-unread', labelKey: 'menu.markUnread', iconName: 'mail', icon: IconMail, needsMessage: true },
  { action: 'flag', labelKey: 'menu.flag', iconName: 'flag', icon: IconFlag, needsMessage: true },
  { action: 'snooze', labelKey: 'shortcut.snooze', iconName: 'alarm-snooze', icon: IconAlarmSnooze, needsMessage: true },
  { action: 'download-offline', labelKey: 'shortcut.downloadOffline', iconName: 'download', icon: IconDownload, needsMessage: true },
  { action: 'archive', labelKey: 'menu.archive', iconName: 'archive', icon: IconArchive, needsMessage: true },
  { action: 'toggle-spam', labelKey: 'shortcut.toggleSpam', iconName: 'alert-triangle', icon: IconAlertTriangle, needsMessage: true },
  { action: 'delete-message', labelKey: 'menu.deleteMessage', iconName: 'trash', icon: IconTrash, needsMessage: true, danger: true },
  { action: 'unsubscribe', labelKey: 'shortcut.unsubscribe', iconName: 'mail-x', icon: IconMailX, needsMessage: true },
  { action: 'new-view', labelKey: 'views.new', iconName: 'bookmark-plus', icon: IconBookmarkPlus },
  { action: 'edit-view', labelKey: 'palette.action.editView', iconName: 'bookmark-edit', icon: IconBookmarkEdit },
  { action: 'toggle-fullscreen', labelKey: 'menu.toggleFullscreen', iconName: 'maximize', icon: IconMaximize },
  { action: 'toggle-low-power', labelKey: 'menu.lowPower', iconName: 'battery-eco', icon: IconBatteryEco },
  { action: 'mark-vip', labelKey: 'palette.action.markVip', iconName: 'star', icon: IconStar, needsMessage: true },
  { action: 'move-to', labelKey: 'messageList.menu.moveTo', iconName: 'folder-symlink', icon: IconFolderSymlink, needsMessage: true },
  { action: 'flag-color', labelKey: 'palette.action.flagColor', iconName: 'palette', icon: IconPalette, needsMessage: true },
  { action: 'remove-offline', labelKey: 'messageList.menu.removeOffline', iconName: 'download-off', icon: IconDownloadOff, needsMessage: true },
  { action: 'new-folder', labelKey: 'palette.action.newFolder', iconName: 'folder-plus', icon: IconFolderPlus },
  { action: 'rename-folder', labelKey: 'palette.action.renameFolder', iconName: 'pencil', icon: IconPencil },
  { action: 'delete-folder', labelKey: 'palette.action.deleteFolder', iconName: 'folder-x', icon: IconFolderX, danger: true },
  { action: 'empty-trash', labelKey: 'folders.emptyTrash', iconName: 'trash-x', icon: IconTrashX, danger: true },
  { action: 'toggle-pin-folder', labelKey: 'palette.action.pinFolder', iconName: 'pin', icon: IconPin },
  { action: 'apply-theme', labelKey: 'palette.action.applyTheme', iconName: 'brush', icon: IconBrush },
]

// catalogByAction resolves an action id to its definition in O(1).
export const catalogByAction: Record<string, MenuActionDef> = Object.fromEntries(
  menuActionCatalog.map((def) => [def.action, def]),
)

// IconNode is a single svg child of a tabler icon: [tag, attributes]. Custom
// items store the geometry of their chosen tabler icon so the always-present
// bar renders it without pulling in the full icon dataset.
export type IconNode = [string, Record<string, string | number>]

// MenuItemLayout is one entry inside a menu or submenu. Built-in 'action' items
// reference the catalog for their label/icon; 'custom' items carry their own
// label, icon name and (for a tabler icon) its geometry; 'separator' is a
// divider; 'submenu' is a nested flyout holding its own items (one level deep,
// so a submenu never contains another submenu).
export interface MenuItemLayout {
  kind: 'action' | 'custom' | 'separator' | 'submenu'
  // id is stable across reorders: the action id for built-ins, a generated uid
  // for custom items, separators and submenus.
  id: string
  // action is the dispatched action for built-in and custom items.
  action?: MenuActionId
  // label is the raw (non-i18n) text of a custom item or submenu.
  label?: string
  // iconName is a custom item's or submenu's chosen icon: a theme override name
  // or a tabler name; empty for no icon.
  iconName?: string
  // iconNodes is the geometry of a chosen tabler icon; unset when the icon is a
  // theme override (rendered live by name) or there is no icon.
  iconNodes?: IconNode[]
  // items holds a submenu's children (action/custom/separator only).
  items?: MenuItemLayout[]
  // hidden items stay in the layout but are not rendered in the bar.
  hidden?: boolean
}

// MenuLayout is one top-level menu. Built-in menus label from labelKey (or, for
// the app menu, the literal brand); custom menus carry a raw label.
export interface MenuLayout {
  id: string
  builtin: boolean
  labelKey?: string
  label?: string
  hidden?: boolean
  items: MenuItemLayout[]
}

// MenuBarLayout is the full customizable bar. version guards the persisted shape.
export interface MenuBarLayout {
  version: number
  menus: MenuLayout[]
}

export const menuBarLayoutVersion = 1

// sep builds a default separator item with a deterministic id so reorders and
// merges stay stable within the built-in layout.
function sep(menuId: string, index: number): MenuItemLayout {
  return { kind: 'separator', id: `sep-${menuId}-${index}` }
}

// action builds a default built-in action item; its id is the action itself.
function action(id: MenuActionId): MenuItemLayout {
  return { kind: 'action', id, action: id }
}

// defaultMenuLayout returns the built-in bar structure. macOS-only items are
// always present so a layout stays valid across installs; the bar and editor
// filter them per platform. This mirrors the historical hardcoded menu.
export function defaultMenuLayout(): MenuBarLayout {
  return {
    version: menuBarLayoutVersion,
    menus: [
      {
        id: 'app',
        builtin: true,
        label: 'Pelton',
        items: [
          action('about'),
          sep('app', 0),
          action('preferences'),
          sep('app', 1),
          action('hide-window'),
          action('quit'),
        ],
      },
      {
        id: 'file',
        builtin: true,
        labelKey: 'menu.file',
        items: [action('compose'), sep('file', 0), action('export-pdf'), sep('file', 1), action('close-window')],
      },
      {
        id: 'mailbox',
        builtin: true,
        labelKey: 'menu.mailbox',
        items: [action('sync'), sep('mailbox', 0), action('add-mailbox'), action('open-mailboxes'), action('open-contacts')],
      },
      {
        id: 'mail',
        builtin: true,
        labelKey: 'menu.mail',
        items: [
          action('undo'),
          sep('mail', 0),
          action('mark-read'),
          action('mark-unread'),
          action('flag'),
          action('archive'),
          action('delete-message'),
        ],
      },
      {
        id: 'view',
        builtin: true,
        labelKey: 'menu.view',
        items: [action('toggle-fullscreen'), sep('view', 0), action('toggle-low-power')],
      },
    ],
  }
}
