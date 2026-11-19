// commands.ts turns the app's state into the flat list of things the command
// palette (#134) can run. It is pure: every input arrives in a CommandContext
// and every entry's `run` calls back into a handler the caller supplied, so
// this module imports no stores and can be reasoned about on its own.
//
// Actions come from menuActionCatalog rather than being restated here, so an
// action added to the menu bar shows up in the palette for free.

import type { ComponentType } from 'svelte'
import {
  IconFolder,
  IconInbox,
  IconBookmark,
  IconSettings,
  IconMailbox,
  IconSun,
  IconMoon,
  IconDeviceDesktop,
  IconBrush,
  IconUsers,
  IconMail,
  IconMailOpened,
} from '@tabler/icons-svelte'
import { menuActionCatalog, type MenuActionId } from './menuactions'
import { settingsCategories } from './settingscategories'
import { flagColors } from '../theme/flagcolors'
import { accountLabel } from './format'
import type { Account, Folder, UnifiedView, View, MessageSummary, ThemeInfo, Profile } from './types'

/** Which section of the palette an entry belongs to. */
export type CommandGroup = 'action' | 'navigate' | 'setting' | 'mail' | 'calendar' | 'contacts' | 'tasks' | 'projects'

/**
 * A second stage of the palette: picking a folder for "Rename mailbox", a
 * colour for "Flag colour", a theme for "Apply theme". Returning one from
 * `run` keeps the palette open on that list instead of closing.
 */
export interface CommandStep {
  /** Shown as a chip in front of the input. */
  label: string
  /** Placeholder while the step is active. */
  placeholder: string
  items: PaletteCommand[]
}

/** One entry in the palette. */
export interface PaletteCommand {
  /** Stable across restarts; the key usage boosting is recorded under. */
  id: string
  group: CommandGroup
  /** Localized, already resolved. What the fuzzy matcher scores against. */
  label: string
  /** Extra localized text that matches but is not displayed. */
  keywords?: string
  /** Right-aligned muted text: a shortcut combo, an account address. */
  hint?: string
  icon: ComponentType
  /** A colour dot rendered instead of the icon, for the flag colours. */
  swatch?: string
  danger?: boolean
  /** Runs the entry, or returns the step to descend into. */
  run: () => CommandStep | void | Promise<void>
}

/** Everything the builders need from the running app. */
export interface CommandContext {
  t: (key: string) => string
  /** Runs a catalog action through App.svelte's dispatcher. */
  dispatch: (action: MenuActionId) => void
  /**
   * Display-ready shortcut hint for an action, or '' when it has no key. The
   * caller resolves it, because a fixed catalog hint and a live rebindable one
   * both have to come out formatted for the platform.
   */
  hintFor: (action: MenuActionId) => string
  accounts: Account[]
  foldersByAccount: Record<number, Folder[]>
  unifiedViews: UnifiedView[]
  savedViews: View[]
  themes: ThemeInfo[]
  /** Every profile, for the switcher step. Empty hides the entry. */
  profiles: Profile[]
  /** Whether each profile also gets its own top-level palette entry. */
  paletteProfiles: boolean
  /** The message in the reading pane, or null. */
  openMessage: MessageSummary | null
  /** The message list's multi-selection, empty when there is none. */
  selected: MessageSummary[]
  selectFolder: (folder: Folder) => void
  selectUnified: (key: string, label: string) => void
  selectSavedView: (id: number, name: string) => void
  openSettings: (category: string) => void
  /** Single-message operations, already bound to their target. */
  onMessage: (op: MessageOp, item: MessageSummary, color?: number) => void
  /** The same operations over the whole selection. */
  onBulk: (op: MessageOp, items: MessageSummary[], color?: number) => void
  /** Folder-scoped operations that open a dialog. */
  onFolder: (op: FolderOp, folder: Folder) => void
  /** Opens the view editor on an existing saved view. */
  editView: (view: View) => void
  applyTheme: (themeId: string) => void
  setBaseTheme: (theme: 'light' | 'dark' | 'system') => void
  /** Switches to another profile. */
  switchProfile: (id: number) => void
}

/** A message operation the palette can run on one message or on a selection. */
export type MessageOp =
  | 'mark-read'
  | 'mark-unread'
  | 'flag'
  | 'unflag'
  | 'flag-color'
  | 'delete'
  | 'archive'
  | 'done'
  | 'download-offline'
  | 'remove-offline'

/** A folder operation the palette can run once a folder is picked. */
export type FolderOp = 'rename' | 'delete' | 'new-subfolder' | 'empty-trash' | 'toggle-pin'

// actions the palette owns outright: they need a target, so the catalog entry
// is replaced by a builder that opens a picker step instead of dispatching.
const steppedActions = new Set<MenuActionId>([
  'move-to',
  'flag-color',
  'rename-folder',
  'delete-folder',
  'new-folder',
  'empty-trash',
  'toggle-pin-folder',
  'apply-theme',
  'edit-view',
  'switch-profile',
])

/** Every folder across every account, flattened, with its account attached. */
function allFolders(ctx: CommandContext): { folder: Folder; account: Account }[] {
  const out: { folder: Folder; account: Account }[] = []
  for (const account of ctx.accounts) {
    for (const folder of ctx.foldersByAccount[account.id] ?? []) {
      out.push({ folder, account })
    }
  }
  return out
}

/** Builds a folder picker step; `filter` narrows which folders qualify. */
function folderStep(
  ctx: CommandContext,
  op: FolderOp,
  label: string,
  filter: (folder: Folder) => boolean = () => true,
): CommandStep {
  return {
    label,
    placeholder: ctx.t('palette.step.pickFolder'),
    items: allFolders(ctx)
      .filter(({ folder }) => filter(folder))
      .map(({ folder, account }) => ({
        id: `folder-op:${op}:${folder.id}`,
        group: 'action' as const,
        label: folder.name,
        hint: account.email,
        icon: IconFolder,
        danger: op === 'delete' || op === 'empty-trash',
        run: () => ctx.onFolder(op, folder),
      })),
  }
}

/** Builds the colour picker step, shared by the single and bulk entries. */
function colorStep(ctx: CommandContext, apply: (color: number) => void): CommandStep {
  return {
    label: ctx.t('palette.action.flagColor'),
    placeholder: ctx.t('palette.step.pickColor'),
    items: [
      {
        id: 'flag-color:0',
        group: 'action' as const,
        label: ctx.t('palette.color.none'),
        icon: IconBrush,
        run: () => apply(0),
      },
      ...flagColors.map((c) => ({
        id: `flag-color:${c.index}`,
        group: 'action' as const,
        label: ctx.t(`flagColor.${c.name.toLowerCase()}`),
        icon: IconBrush,
        swatch: c.hex,
        run: () => apply(c.index),
      })),
    ],
  }
}

/** Actions: the menu catalog, minus what needs a message that is not open. */
function actionCommands(ctx: CommandContext): PaletteCommand[] {
  const out: PaletteCommand[] = []
  for (const def of menuActionCatalog) {
    if (def.needsMessage && !ctx.openMessage) {
      continue
    }
    if (steppedActions.has(def.action)) {
      continue
    }
    out.push({
      id: `action:${def.action}`,
      group: 'action',
      label: ctx.t(def.labelKey),
      hint: ctx.hintFor(def.action),
      icon: def.icon,
      danger: def.danger,
      run: () => ctx.dispatch(def.action),
    })
  }
  return out
}

/** The stepped actions, each opening its own picker. */
function steppedCommands(ctx: CommandContext): PaletteCommand[] {
  const out: PaletteCommand[] = []
  const msg = ctx.openMessage

  if (msg) {
    out.push({
      id: 'action:flag-color',
      group: 'action',
      label: ctx.t('palette.action.flagColor'),
      icon: IconBrush,
      run: () => colorStep(ctx, (color) => ctx.onMessage('flag-color', msg, color)),
    })
  }

  out.push({
    id: 'action:new-folder',
    group: 'action',
    label: ctx.t('palette.action.newFolder'),
    icon: IconFolder,
    run: () => folderStep(ctx, 'new-subfolder', ctx.t('palette.action.newFolder'), (f) => f.delimiter !== ''),
  })
  out.push({
    id: 'action:rename-folder',
    group: 'action',
    label: ctx.t('palette.action.renameFolder'),
    icon: IconFolder,
    run: () => folderStep(ctx, 'rename', ctx.t('palette.action.renameFolder'), (f) => f.role === 'normal'),
  })
  out.push({
    id: 'action:delete-folder',
    group: 'action',
    label: ctx.t('palette.action.deleteFolder'),
    icon: IconFolder,
    danger: true,
    run: () => folderStep(ctx, 'delete', ctx.t('palette.action.deleteFolder'), (f) => f.role === 'normal'),
  })
  out.push({
    id: 'action:toggle-pin-folder',
    group: 'action',
    label: ctx.t('palette.action.pinFolder'),
    icon: IconFolder,
    run: () => folderStep(ctx, 'toggle-pin', ctx.t('palette.action.pinFolder')),
  })

  const trashes = allFolders(ctx).filter(({ folder }) => folder.role === 'trash' && folder.totalCount > 0)
  if (trashes.length > 0) {
    out.push({
      id: 'action:empty-trash',
      group: 'action',
      label: ctx.t('folders.emptyTrash'),
      icon: IconFolder,
      danger: true,
      run: () =>
        folderStep(ctx, 'empty-trash', ctx.t('folders.emptyTrash'), (f) => f.role === 'trash' && f.totalCount > 0),
    })
  }

  if (ctx.savedViews.length > 0) {
    out.push({
      id: 'action:edit-view',
      group: 'action',
      label: ctx.t('palette.action.editView'),
      icon: IconBookmark,
      run: () => ({
        label: ctx.t('palette.action.editView'),
        placeholder: ctx.t('palette.step.pickView'),
        items: ctx.savedViews.map((v) => ({
          id: `edit-view:${v.id}`,
          group: 'action' as const,
          label: v.name,
          icon: IconBookmark,
          run: () => ctx.editView(v),
        })),
      }),
    })
  }

  if (ctx.profiles.length > 1) {
    out.push({
      id: 'action:switch-profile',
      group: 'action',
      label: ctx.t('profiles.switch'),
      icon: IconUsers,
      run: () => profileStep(ctx),
    })
    // with the setting on, each profile is its own entry, so activating one is
    // a keystroke instead of a picker step. Off by default: the entries are
    // only worth their place in the list if you switch often.
    if (ctx.paletteProfiles) {
      for (const profile of ctx.profiles) {
        if (profile.active) {
          continue
        }
        out.push({
          id: `action:profile-${profile.id}`,
          group: 'action',
          label: ctx.t('profiles.switchToName').replace('{name}', profile.name),
          keywords: ctx.t('profiles.switch'),
          icon: IconUsers,
          run: () => ctx.switchProfile(profile.id),
        })
      }
    }
  }

  out.push({
    id: 'action:apply-theme',
    group: 'action',
    label: ctx.t('palette.action.applyTheme'),
    icon: IconBrush,
    run: () => themeStep(ctx),
  })

  return out
}

/** The profile picker. The one you are in is listed but does nothing. */
function profileStep(ctx: CommandContext): CommandStep {
  return {
    label: ctx.t('profiles.switch'),
    placeholder: ctx.t('profiles.pick'),
    items: ctx.profiles.map((profile) => ({
      id: `profile:${profile.id}`,
      group: 'action' as const,
      label: profile.name,
      hint: profile.active ? ctx.t('profiles.currentTag') : undefined,
      icon: IconUsers,
      run: () => ctx.switchProfile(profile.id),
    })),
  }
}

/** The theme picker: the three built-in bases, then every installed theme. */
function themeStep(ctx: CommandContext): CommandStep {
  return {
    label: ctx.t('palette.action.applyTheme'),
    placeholder: ctx.t('palette.step.pickTheme'),
    items: [
      {
        id: 'theme:light',
        group: 'action' as const,
        label: ctx.t('onboarding.theme.light'),
        icon: IconSun,
        run: () => ctx.setBaseTheme('light'),
      },
      {
        id: 'theme:dark',
        group: 'action' as const,
        label: ctx.t('onboarding.theme.dark'),
        icon: IconMoon,
        run: () => ctx.setBaseTheme('dark'),
      },
      {
        id: 'theme:system',
        group: 'action' as const,
        label: ctx.t('onboarding.theme.system'),
        icon: IconDeviceDesktop,
        run: () => ctx.setBaseTheme('system'),
      },
      ...ctx.themes.map((theme) => ({
        id: `theme:${theme.id}`,
        group: 'action' as const,
        label: theme.name,
        hint: theme.author,
        icon: IconBrush,
        run: () => ctx.applyTheme(theme.id),
      })),
    ],
  }
}

/**
 * Bulk entries, offered alongside the single-message ones whenever the list has
 * a multi-selection. The count is in the label so the two never look alike.
 */
function bulkCommands(ctx: CommandContext): PaletteCommand[] {
  const items = ctx.selected
  if (items.length === 0) {
    return []
  }
  const n = String(items.length)
  const count = (key: string) => ctx.t(key).replace('{n}', n)
  const entry = (
    id: string,
    label: string,
    icon: ComponentType,
    op: MessageOp,
    danger?: boolean,
  ): PaletteCommand => ({
    id: `bulk:${id}`,
    group: 'action',
    label,
    icon,
    danger,
    run: () => ctx.onBulk(op, items),
  })

  const read = menuActionCatalog.find((d) => d.action === 'mark-read')!
  const unread = menuActionCatalog.find((d) => d.action === 'mark-unread')!
  const flag = menuActionCatalog.find((d) => d.action === 'flag')!
  const del = menuActionCatalog.find((d) => d.action === 'delete-message')!
  const arch = menuActionCatalog.find((d) => d.action === 'archive')!
  const done = menuActionCatalog.find((d) => d.action === 'done')!
  const offline = menuActionCatalog.find((d) => d.action === 'download-offline')!

  return [
    entry('mark-read', count('messageList.bulk.markReadCount'), read.icon, 'mark-read'),
    entry('mark-unread', count('messageList.bulk.markUnreadCount'), unread.icon, 'mark-unread'),
    entry('flag', count('messageList.bulk.flagCount'), flag.icon, 'flag'),
    entry('unflag', count('messageList.bulk.unflagCount'), flag.icon, 'unflag'),
    {
      id: 'bulk:flag-color',
      group: 'action',
      label: count('palette.bulk.flagColorCount'),
      icon: IconBrush,
      run: () => colorStep(ctx, (color) => ctx.onBulk('flag-color', items, color)),
    },
    entry('archive', count('palette.bulk.archiveCount'), arch.icon, 'archive'),
    entry('done', count('palette.bulk.doneCount'), done.icon, 'done'),
    entry('download-offline', count('palette.bulk.downloadCount'), offline.icon, 'download-offline'),
    entry('delete', count('messageList.bulk.deleteCount'), del.icon, 'delete', true),
  ]
}

/** Navigation: unified views, saved views, then every account's folders. */
function navigateCommands(ctx: CommandContext): PaletteCommand[] {
  const out: PaletteCommand[] = []

  for (const view of ctx.unifiedViews) {
    out.push({
      id: `unified:${view.key}`,
      group: 'navigate',
      label: view.label,
      icon: IconInbox,
      run: () => ctx.selectUnified(view.key, view.label),
    })
  }
  for (const view of ctx.savedViews) {
    out.push({
      id: `view:${view.id}`,
      group: 'navigate',
      label: view.name,
      keywords: ctx.t('palette.keywords.view'),
      icon: IconBookmark,
      run: () => ctx.selectSavedView(view.id, view.name),
    })
  }
  for (const { folder, account } of allFolders(ctx)) {
    out.push({
      id: `folder:${folder.id}`,
      group: 'navigate',
      label: folder.name,
      hint: account.email,
      icon: IconFolder,
      run: () => ctx.selectFolder(folder),
    })
  }
  return out
}

/** Settings: one entry per category, matching on its localized synonyms too. */
function settingCommands(ctx: CommandContext): PaletteCommand[] {
  const out: PaletteCommand[] = settingsCategories(ctx.t).map((cat) => ({
    id: `settings:${cat.key}`,
    group: 'setting' as const,
    label: cat.label,
    keywords: cat.keywords,
    icon: cat.icon,
    run: () => ctx.openSettings(cat.key),
  }))
  out.push({
    id: 'settings:root',
    group: 'setting',
    label: ctx.t('menu.preferences'),
    icon: IconSettings,
    run: () => ctx.openSettings(''),
  })
  for (const account of ctx.accounts) {
    out.push({
      id: `account:${account.id}`,
      group: 'setting',
      label: accountLabel(account),
      hint: account.email,
      icon: IconMailbox,
      run: () => ctx.openSettings('mailboxes'),
    })
  }
  return out
}

/** Every command currently available, in group order. */
export function buildCommands(ctx: CommandContext): PaletteCommand[] {
  return [
    ...actionCommands(ctx),
    ...steppedCommands(ctx),
    ...bulkCommands(ctx),
    ...navigateCommands(ctx),
    ...settingCommands(ctx),
  ]
}

/**
 * Turns search hits into palette entries. These are not fuzzy ranked: the
 * backend already returned them in relevance order, and re-scoring them against
 * the same query here would only fight it.
 */
export function mailCommands(
  t: (key: string) => string,
  items: MessageSummary[],
  open: (item: MessageSummary) => void,
): PaletteCommand[] {
  return items.map((item) => ({
    id: `mail:${item.id}`,
    group: 'mail' as const,
    label: item.subject || t('messageList.noSubject'),
    hint: item.fromName || item.fromAddress,
    icon: item.seen ? IconMailOpened : IconMail,
    run: () => open(item),
  }))
}
