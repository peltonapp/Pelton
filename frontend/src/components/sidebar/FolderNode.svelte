<script lang="ts">
  // a recursive folder tree node. children are the folders whose parentId is this
  // folder's id, so the server-provided hierarchy and per-server delimiter are
  // respected without the frontend parsing paths. expansion state is local.
  //
  // the whole node (row plus children) is one element so the parent's reorder
  // container sees exactly one child per folder. this node's own children are a
  // separate container, which is what keeps a drag inside its sibling group.
  import { slide } from 'svelte/transition'
  import { dur, motion } from '../../lib/motion'
  import {
    IconInbox,
    IconSend,
    IconFile,
    IconTrash,
    IconAlertTriangle,
    IconArchive,
    IconFolder,
    IconFolderPlus,
    IconPencil,
    IconPin,
    IconPinnedOff,
    IconTags,
    IconRefresh,
    IconRefreshOff,
  } from '@tabler/icons-svelte'
  import SidebarRow from './SidebarRow.svelte'
  import { maildrop, maildragSpring } from '../../stores/maildrag'
  import { moveToFolder } from '../../lib/messageactions'
  import type { MessageSummary } from '../../lib/types'
  import Self from './FolderNode.svelte'
  import type { Folder } from '../../lib/types'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { selection, selectFolder } from '../../stores/selection'
  import { openContextMenu, type MenuEntry } from '../../stores/contextmenu'
  import { menuHint } from '../../stores/shortcuts'
  import {
    openCreateFolder,
    openRenameFolder,
    openDeleteFolder,
    openEmptyTrash,
    openFolderRole,
  } from '../../stores/folderdialog'
  import { startFolderDrag, endFolderDrag } from '../../stores/sidebardrag'
  import { refreshSidebar } from '../../stores/accounts'
  import { prefs } from '../../stores/prefs'
  import { reorderFolders, setFolderPinned, setFolderSyncExcluded } from '../../lib/api'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import { openVaultUnlock } from '../../stores/vaultdialog'
  import Ph from '../common/Ph.svelte'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'

  export let folder: Folder
  export let folders: Folder[]
  export let depth: number = 0

  let expanded = true

  $: children = folders.filter((f) => f.parentId === folder.id)
  $: isActive = $selection.kind === 'folder' && $selection.folderId === folder.id
  // special mailboxes are refused by the backend (renaming Sent out from under
  // the app would leave sent mail nowhere to go), so the ui does not offer it.
  $: manageable = folder.role === 'normal'
  // nesting needs a hierarchy delimiter; a flat server has none.
  $: nestable = folder.delimiter !== ''
  // emptying is only offered on the trash, and only when there is something in
  // it, so the menu never carries an action that would do nothing.
  $: emptiable = folder.role === 'trash' && folder.totalCount > 0

  // a folder of a locked vault: the classic design leaves it out, and so does
  // the new one when the vault asks to be hidden; otherwise it is a locked
  // entry without counts that opens the unlock dialog.
  $: locked = folder.vaultLocked
  $: hiddenLocked = locked && ($prefs.design === 'classic' || folder.vaultHidden)

  function protect(): void {
    import('../../stores/mailvault')
      .then((m) => m.protectWithVault({ accountId: folder.accountId, folderId: folder.id }))
      .catch((err) => toastError(errorMessage(err)))
  }

  async function onReorder(event: CustomEvent<ReorderDetail>): Promise<void> {
    try {
      await reorderFolders(event.detail.ids.map(Number))
    } catch (err) {
      toastError(errorMessage(err))
    }
    // reload either way, so the rows end up showing what was actually stored.
    await refreshSidebar()
  }

  async function togglePinned(): Promise<void> {
    try {
      await setFolderPinned(folder.id, !folder.pinned)
      await refreshSidebar()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // unchecking a folder stops it syncing. It keeps whatever is already cached,
  // so this is reversible and costs nothing but a resync (#173).
  async function toggleSync(): Promise<void> {
    try {
      await setFolderSyncExcluded(folder.id, !folder.syncExcluded)
      await refreshSidebar()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function onContext(event: MouseEvent): void {
    if (locked) {
      openContextMenu(event.clientX, event.clientY, [{ label: $t('mailvault.unlock'), icon: IconFolder, action: () => openVaultUnlock(folder.vaultId ?? 0) }])
      return
    }
    const entries: MenuEntry[] = [
      {
        label: folder.pinned ? $t('folders.unpin') : $t('folders.pin'),
        icon: folder.pinned ? IconPinnedOff : IconPin,
        hint: menuHint('toggle-pin-folder'),
        action: () => void togglePinned(),
      },
      // offered on every folder, including the inbox: a mailbox the server
      // flagged wrongly is exactly the one that needs correcting.
      {
        label: $t('folders.roleAction'),
        icon: IconTags,
        action: () => openFolderRole(folder),
      },
      {
        label: folder.syncExcluded ? $t('folders.resumeSync') : $t('folders.stopSync'),
        icon: folder.syncExcluded ? IconRefresh : IconRefreshOff,
        action: () => void toggleSync(),
      },
    ]
    if (emptiable) {
      entries.push('separator')
      entries.push({
        label: $t('folders.emptyTrash'),
        icon: IconTrash,
        danger: true,
        hint: menuHint('empty-trash'),
        action: () => openEmptyTrash(folder),
      })
    }
    if (nestable) {
      entries.push('separator')
      entries.push({
        label: $t('folders.newSubfolder'),
        icon: IconFolderPlus,
        hint: menuHint('new-folder'),
        action: () => openCreateFolder(folder.accountId, folder),
      })
    }
    if (manageable) {
      if (!nestable) {
        entries.push('separator')
      }
      entries.push({
        label: $t('folders.rename'),
        icon: IconPencil,
        hint: menuHint('rename-folder'),
        action: () => openRenameFolder(folder),
      })
      entries.push({
        label: $t('folders.delete'),
        icon: IconTrash,
        danger: true,
        hint: menuHint('delete-folder'),
        action: () => openDeleteFolder(folder),
      })
    }
    if ($prefs.design === 'new' && !folder.vaultId) {
      entries.push('separator')
      entries.push({ label: $t('mailvault.protect'), icon: IconFolder, action: protect })
    }
    openContextMenu(event.clientX, event.clientY, entries)
  }

  // pick an icon by role so special folders read at a glance.
  const roleIcons: Record<string, typeof IconFolder> = {
    inbox: IconInbox,
    sent: IconSend,
    drafts: IconFile,
    trash: IconTrash,
    junk: IconAlertTriangle,
    archive: IconArchive,
    normal: IconFolder,
  }
  $: Icon = roleIcons[folder.role] ?? IconFolder

  // a folder the user took out of sync looks identical to one that simply has
  // no new mail, which is how somebody forgets they excluded it. The marker is
  // a setting because a mailbox with many excluded folders does not need a row
  // of icons repeating what its owner already knows.
  $: unsynced = folder.syncExcluded && $prefs.showUnsyncedFolder

  // mail dragged from the list lands here when it belongs to this folder's
  // account and is not all in the folder already.
  $: mailDrop = {
    accepts: (items: MessageSummary[]) =>
      items.every((m) => m.accountId === folder.accountId) && items.some((m) => m.folderId !== folder.id),
    onDrop: (items: MessageSummary[]) => void moveToFolder(items, folder),
    hint: () => $t('folders.drop.move').replace('{folder}', folder.name),
  }

  // mail held over a closed folder opens it, so a subfolder can take the drop.
  function springOpen(): void {
    if (children.length > 0) {
      expanded = true
    }
  }
</script>

<div class="node" data-reorder-id={folder.id}>
  {#if locked}
    {#if !hiddenLocked}
      <SidebarRow
        label={folder.name}
        title={$t('mailvault.lockedFolder')}
        {depth}
        expandable={children.length > 0}
        {expanded}
        on:select={() => openVaultUnlock(folder.vaultId ?? 0)}
        on:toggle={() => (expanded = !expanded)}
        on:contextmenu={(e) => onContext(e.detail)}
      >
        <Ph svg={lockIcon} size={15} />
      </SidebarRow>
    {/if}
  {:else}
  <div use:maildrop={mailDrop} use:maildragSpring={springOpen}>
  <SidebarRow
    label={folder.name}
    title={unsynced ? $t('folders.notSyncing') : ''}
    count={folder.unreadCount}
    active={isActive}
    {depth}
    expandable={children.length > 0}
    {expanded}
    reorderable
    on:select={() => selectFolder(folder)}
    on:toggle={() => (expanded = !expanded)}
    on:contextmenu={(e) => onContext(e.detail)}
  >
    <svelte:component this={Icon} size={15} stroke={1.6} />
    <span slot="badge" class="unsynced" aria-hidden="true">
      {#if unsynced}
        <IconRefreshOff size={13} stroke={1.6} />
      {/if}
    </span>
  </SidebarRow>
  </div>
  {/if}

  {#if expanded && children.length > 0}
    <div
      transition:slide|local={{ duration: dur(motion.base) }}
      use:reorder
      on:reorderstart={() => startFolderDrag(folder.accountId)}
      on:reorderend={endFolderDrag}
      on:reorder={onReorder}
    >
      {#each children as child (child.id)}
        <Self folder={child} {folders} depth={depth + 1} />
      {/each}
    </div>
  {/if}
</div>

<style>
  /* sits between the folder name and its unread count, in the muted tone the
     other secondary marks use. */
  .unsynced {
    display: inline-flex;
    align-items: center;
    color: var(--text-tertiary);
    flex-shrink: 0;
  }
</style>
