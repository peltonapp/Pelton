<script lang="ts">
  // one account's section in the sidebar: a header with the address and the full
  // folder tree below it. the header chevron collapses the whole account. root
  // folders are those without a parent; the rest nest via FolderNode recursion.
  import { IconChevronRight, IconFolderPlus, IconAlertTriangle, IconCloudOff } from '@tabler/icons-svelte'
  import { slide } from 'svelte/transition'
  import { dur, motion } from '../../lib/motion'
  import FolderNode from './FolderNode.svelte'
  import DragGrip from './DragGrip.svelte'
  import { prefs } from '../../stores/prefs'
  import { collapsedAccounts, toggleAccountCollapsed } from '../../stores/sidebarstate'
  import { openCreateFolder } from '../../stores/folderdialog'
  import { folderDragAccount, startFolderDrag, endFolderDrag } from '../../stores/sidebardrag'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { maildragSpring } from '../../stores/maildrag'
  import { refreshSidebar } from '../../stores/accounts'
  import { missingPassword, askForPassword } from '../../stores/passwordprompt'
  import { failedSyncs, showSyncFailure } from '../../stores/syncfailures'
  import { reorderFolders } from '../../lib/api'
  import { accountLabel } from '../../lib/format'
  import { toastError, errorMessage } from '../../stores/toast'
  import type { Account, Folder } from '../../lib/types'
  import { t } from '../../lib/i18n'
  import { lockVaultsNow, openVaultUnlock } from '../../stores/vaultdialog'
  import { openContextMenu } from '../../stores/contextmenu'
  import SidebarRow from './SidebarRow.svelte'
  import Ph from '../common/Ph.svelte'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/lock-simple-open-duotone.svg?raw'

  export let account: Account
  export let folders: Folder[]

  // the account section is expanded by default; the collapsed set (persisted) is
  // the source of truth so the choice survives restarts.
  $: expanded = !$collapsedAccounts.has(account.id)

  // roots have no parent. loose equality catches both null and undefined.
  $: roots = folders.filter((f) => f.parentId == null)
  // a folder never moves between accounts, so a drag elsewhere dims this section
  // to show where the drop can land.
  $: dimmed = $folderDragAccount !== null && $folderDragAccount !== account.id

  async function onReorder(event: CustomEvent<ReorderDetail>): Promise<void> {
    try {
      await reorderFolders(event.detail.ids.map(Number))
    } catch (err) {
      toastError(errorMessage(err))
    }
    // reload either way, so the rows end up showing what was actually stored.
    await refreshSidebar()
  }

  // the header shows what the user calls the mailbox, or the email when they
  // prefer it. Local Folders has no real address, so it is named rather than
  // addressed.
  $: label = account.local
    ? $t('sidebar.localFolders')
    : $prefs.showAccountEmail
      ? account.email
      : accountLabel(account)

  // an account with nothing in the keyring cannot sync at all. The prompt can be
  // dismissed (#290), so this marker is what is left saying so.
  $: needsPassword = $missingPassword.has(account.id)

  // an account whose last sync failed. Not the password triangle: that one says
  // "give me a password", this one says "the last sync did not work", and only
  // one of the two is the user's to fix. A missing password already has its own
  // mark, so this stands down for it rather than showing two (#322).
  $: syncFailure = needsPassword ? undefined : $failedSyncs.find((s) => s.accountId === account.id)

  // mail held over a collapsed account opens it, so its folders can take the drop.
  function springOpen(): void {
    if (!expanded) {
      toggleAccountCollapsed(account.id)
    }
  }

  // one row per vault with folders here: unlock where its folders are hidden
  // (always in the classic design), and lock now while it is open.
  $: vaultRows = [...new Map(folders.filter((f) => f.vaultId).map((f) => [f.vaultId, f])).values()]
    .filter((f) => !f.vaultLocked || $prefs.design === 'classic' || f.vaultHidden)
    .map((f) => ({ id: f.vaultId ?? 0, locked: f.vaultLocked ?? false }))
  // the vault store clears the screen on lock, so it loads once there is a
  // vault to speak of.
  $: if (folders.some((f) => f.vaultId)) {
    void import('../../stores/mailvault')
  }

  function onAccountContext(event: MouseEvent): void {
    if ($prefs.design !== 'new' || account.local || folders.some((f) => f.vaultId)) {
      return
    }
    event.preventDefault()
    const member = { accountId: account.id, folderId: 0 }
    openContextMenu(event.clientX, event.clientY, [
      {
        label: $t('mailvault.protectAccount'),
        icon: IconFolderPlus,
        action: () =>
          void import('../../stores/mailvault').then((m) => m.protectWithVault(member)).catch((err) => toastError(errorMessage(err))),
      },
    ])
  }
</script>

<section class="account" class:dimmed data-reorder-id={account.id}>
  <div class="account-row" use:maildragSpring={springOpen}>
    <button
      type="button"
      class="account-head"
      class:open={expanded}
      title={account.local ? $t('sidebar.localFoldersHint') : account.email}
      aria-expanded={expanded}
      on:click={() => toggleAccountCollapsed(account.id)}
      on:contextmenu={onAccountContext}
    >
      <span class="account-caret" aria-hidden="true"><IconChevronRight size={13} stroke={1.9} /></span>
      <span class="account-name">{label}</span>
    </button>
    {#if needsPassword}
      <button
        type="button"
        class="needs-password"
        title={$t('mailboxes.passwordPrompt.marker')}
        aria-label={$t('mailboxes.passwordPrompt.marker')}
        on:click={() => askForPassword(account)}
      >
        <IconAlertTriangle size={14} stroke={1.9} />
      </button>
    {/if}
    {#if syncFailure}
      <button
        type="button"
        class="sync-failed"
        title={$t('syncFailure.marker')}
        aria-label={$t('syncFailure.marker')}
        on:click={() => showSyncFailure(syncFailure)}
      >
        <IconCloudOff size={14} stroke={1.9} />
      </button>
    {/if}
    <button
      type="button"
      class="new-folder"
      title={$t('folders.newFolder')}
      aria-label={$t('folders.newFolder')}
      on:click={() => openCreateFolder(account.id)}
    >
      <IconFolderPlus size={14} stroke={1.7} />
    </button>
    <DragGrip />
  </div>
  {#if expanded}
    {#if roots.length === 0}
      <p class="empty-note">{$t('sidebar.account.noFolders')}</p>
    {:else}
      <div
        transition:slide|local={{ duration: dur(motion.base) }}
        use:reorder
        on:reorderstart={() => startFolderDrag(account.id)}
        on:reorderend={endFolderDrag}
        on:reorder={onReorder}
      >
        {#each roots as folder (folder.id)}
          <FolderNode {folder} {folders} depth={0} />
        {/each}
      </div>
    {/if}
    {#each vaultRows as v (v.id)}
      <SidebarRow
        label={v.locked ? $t('mailvault.unlock') : $t('mailvault.lockNow')}
        on:select={() => (v.locked ? openVaultUnlock(v.id) : lockVaultsNow())}
      >
        <Ph svg={v.locked ? lockIcon : openIcon} size={15} />
      </SidebarRow>
    {/each}
  {/if}
</section>

<style>
  .account {
    margin-top: var(--space-4);
  }

  /* a folder drag can only land among its own siblings, so every other account
     fades back while one is in progress. pointer-events stay on: dnd needs to
     keep tracking the pointer, it just cannot drop here. */
  .account.dimmed {
    opacity: 0.4;
    transition: opacity 0.12s ease;
  }

  /* the header and its new-folder button share a row; the button only appears
     on hover so the sidebar stays quiet at rest. */
  .account-row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-control);
  }

  .account-head {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
    padding: var(--space-2) var(--space-3);
    border: none;
    background: transparent;
    cursor: var(--cursor-action);
    text-align: start;
    border-radius: var(--radius-control);
  }

  .account-row:hover {
    background: var(--surface-hover);
  }

  /* unlike the new-folder button this one is always visible: it reports a state
     the user has to be able to see without hovering. */
  .needs-password {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    padding: var(--space-1);
    border: none;
    background: transparent;
    color: var(--warning);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .needs-password:hover {
    background: var(--surface-sunken);
  }

  /* danger rather than the password mark's warning: this one is not a question
     being asked, it is something that already went wrong. */
  .sync-failed {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    padding: var(--space-1);
    border: none;
    background: transparent;
    color: var(--danger);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }
  .sync-failed:hover {
    background: var(--surface-sunken);
  }

  .new-folder {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    padding: var(--space-1);
    margin-inline-end: var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
    opacity: 0;
  }

  .account-row:hover .new-folder,
  .new-folder:focus-visible {
    opacity: 1;
  }

  .new-folder:hover {
    color: var(--text-primary);
    background: var(--surface-sunken);
  }

  .account-caret {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--text-tertiary);
    flex-shrink: 0;
  }

  .account-caret :global(svg) {
    transition: transform 0.12s ease;
  }

  .account-head.open .account-caret :global(svg) {
    transform: rotate(90deg);
  }

  .account-name {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: block;
  }

  .empty-note {
    margin: 0;
    padding: var(--space-2) var(--space-3) var(--space-2) var(--space-5);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
