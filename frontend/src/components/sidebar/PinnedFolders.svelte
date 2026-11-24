<script lang="ts">
  // the Pinned group at the top of the sidebar. a pinned folder is mirrored here
  // and still sits in its own account's tree, so pinning never makes a folder
  // disappear from where the user last saw it, and pinning across several
  // accounts reads naturally. the group only renders when something is pinned.
  //
  // rows drag to reorder among themselves. right-clicking one offers unpin; the
  // full folder menu stays on the row in the account tree, since acting on a
  // mirror is easy to misread.
  import {
    IconInbox,
    IconSend,
    IconFile,
    IconTrash,
    IconAlertTriangle,
    IconArchive,
    IconFolder,
    IconPinnedOff,
  } from '@tabler/icons-svelte'
  import SidebarRow from './SidebarRow.svelte'
  import type { Folder } from '../../lib/types'
  import { selection, selectFolder } from '../../stores/selection'
  import { openContextMenu } from '../../stores/contextmenu'
  import { menuHint } from '../../stores/shortcuts'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { refreshSidebar } from '../../stores/accounts'
  import { reorderPinnedFolders, setFolderPinned } from '../../lib/api'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  export let folders: Folder[] = []
  // account email per id, so a pinned folder shows which mailbox it came from.
  // two accounts can easily both have an Archive.
  export let accountEmails: Record<number, string> = {}

  // a pinned folder of a locked vault stays in the account tree, as a locked
  // entry or not at all, and out of here.
  $: shown = folders.filter((f) => !f.vaultLocked)

  const roleIcons: Record<string, typeof IconFolder> = {
    inbox: IconInbox,
    sent: IconSend,
    drafts: IconFile,
    trash: IconTrash,
    junk: IconAlertTriangle,
    archive: IconArchive,
    normal: IconFolder,
  }

  async function onReorder(event: CustomEvent<ReorderDetail>): Promise<void> {
    try {
      await reorderPinnedFolders(event.detail.ids.map(Number))
    } catch (err) {
      toastError(errorMessage(err))
    }
    // reload either way, so the rows end up showing what was actually stored.
    await refreshSidebar()
  }

  async function unpin(folder: Folder): Promise<void> {
    try {
      await setFolderPinned(folder.id, false)
      await refreshSidebar()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function onContext(event: MouseEvent, folder: Folder): void {
    openContextMenu(event.clientX, event.clientY, [
      { label: $t('folders.unpin'), icon: IconPinnedOff, hint: menuHint('toggle-pin-folder'), action: () => void unpin(folder) },
    ])
  }
</script>

{#if shown.length > 0}
  <nav class="pinned" aria-label={$t('sidebar.pinned.ariaLabel')}>
    <header class="group-head">{$t('sidebar.pinned.heading')}</header>
    <div class="list" use:reorder on:reorder={onReorder}>
      {#each shown as folder (folder.id)}
        <SidebarRow
          label={folder.name}
          title={accountEmails[folder.accountId] ?? ''}
          count={folder.unreadCount}
          active={$selection.kind === 'folder' && $selection.folderId === folder.id}
          reorderable
          reorderId={folder.id}
          on:select={() => selectFolder(folder)}
          on:contextmenu={(e) => onContext(e.detail, folder)}
        >
          <svelte:component this={roleIcons[folder.role] ?? IconFolder} size={15} stroke={1.6} />
        </SidebarRow>
      {/each}
    </div>
  </nav>
{/if}

<style>
  .pinned {
    display: flex;
    flex-direction: column;
    gap: 1px;
    margin-bottom: var(--space-3);
  }

  .group-head {
    padding: var(--space-2) var(--space-3);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
</style>
