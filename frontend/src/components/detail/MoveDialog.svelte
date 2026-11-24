<script lang="ts">
  // the "Move to folder" dialog. it lists the account's folders and moves the
  // target there, reusing the same undo machinery as archive (cmd+z moves it
  // back). the target is one message from a row menu and the whole selection
  // from a bulk one, so a folder is picked once for the batch. the folder the
  // messages are already in is excluded; a search box filters long trees.
  import { IconFolder, IconSearch } from '@tabler/icons-svelte'
  import Modal from '../common/Modal.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { moveTargets, closeMove } from '../../stores/move'
  import { listFolders } from '../../lib/api'
  import { moveToFolder } from '../../lib/messageactions'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Folder } from '../../lib/types'

  let folders: Folder[] = []
  let query = ''
  let loadedFor = -1
  let busy = false
  let searchInput: HTMLInputElement | null = null
  let list: HTMLUListElement | null = null

  // every target comes from the same list, so the first one names the account
  // and the folder to leave out.
  $: first = $moveTargets[0] ?? null
  $: count = $moveTargets.length

  // load the account's folders when the target changes.
  $: if (first && first.accountId !== loadedFor) {
    loadedFor = first.accountId
    void load(first.accountId)
  }
  $: if (!first) {
    loadedFor = -1
    query = ''
  }

  async function load(accountId: number): Promise<void> {
    try {
      folders = await listFolders(accountId)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // candidate folders: everything except the message's current folder, filtered
  // by the search box.
  $: candidates = folders
    .filter((f) => !first || f.id !== first.folderId)
    .filter((f) => (query.trim() ? f.name.toLowerCase().includes(query.toLowerCase()) : true))

  async function move(folder: Folder): Promise<void> {
    // a selection can span folders, so the picked one may already hold some of
    // the targets. Those are left alone: the move is refused for them, and the
    // rows would have left the list for a move that never happened.
    if (busy) {
      return
    }
    const targets = $moveTargets
    busy = true
    closeMove()
    await moveToFolder(targets, folder)
    busy = false
  }

  function folderButtons(): HTMLButtonElement[] {
    return Array.from(list?.querySelectorAll<HTMLButtonElement>('button.folder') ?? [])
  }

  // Enter in the search box takes the top match, and the arrows step from the
  // box into the list and through it.
  function onSearchKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' && candidates.length > 0) {
      event.preventDefault()
      void move(candidates[0])
    } else if (event.key === 'ArrowDown') {
      event.preventDefault()
      folderButtons()[0]?.focus()
    }
  }

  function onListKeydown(event: KeyboardEvent): void {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') {
      return
    }
    event.preventDefault()
    const buttons = folderButtons()
    const at = buttons.indexOf(document.activeElement as HTMLButtonElement)
    const next = at + (event.key === 'ArrowDown' ? 1 : -1)
    if (next < 0) {
      searchInput?.focus()
    } else if (next < buttons.length) {
      buttons[next].focus()
    }
  }
</script>

{#if first}
  <Modal
    title={count > 1 ? $t('detail.moveDialog.titleCount').replace('{n}', String(count)) : $t('detail.moveDialog.title')}
    size="small"
    on:close={closeMove}
  >
    <div class="search">
      <IconSearch size={15} stroke={1.7} />
      <!-- svelte-ignore a11y-autofocus -->
      <input
        autofocus
        type="search"
        placeholder={$t('detail.moveDialog.searchPlaceholder')}
        aria-label={$t('detail.moveDialog.searchPlaceholder')}
        bind:this={searchInput}
        bind:value={query}
        on:keydown={onSearchKeydown}
      />
    </div>

    <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
    <ul class="list" bind:this={list} on:keydown={onListKeydown}>
      {#each candidates as f (f.id)}
        <li>
          <button type="button" class="folder" disabled={busy} on:click={() => move(f)}>
            <IconFolder size={16} stroke={1.6} />
            <span class="f-name">{f.name}</span>
          </button>
        </li>
      {/each}
      {#if candidates.length === 0}
        <li class="empty" role="presentation">
          <EmptyState
            size="compact"
            kind={query ? 'filtered' : 'empty'}
            title={query ? $t('detail.moveDialog.noMatches') : $t('detail.moveDialog.noOthers')}
          />
        </li>
      {/if}
    </ul>
  </Modal>
{/if}

<style>
  .search {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
  }
  .search input {
    flex: 1;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    outline: none;
  }

  .list {
    list-style: none;
    margin: var(--space-3) 0 0;
    padding: 0;
  }

  .folder {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-primary);
    cursor: var(--cursor-action);
    text-align: start;
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
  }
  .folder:hover,
  .folder:focus-visible {
    background: var(--surface-hover);
  }
  .folder :global(svg) {
    color: var(--text-tertiary);
    flex-shrink: 0;
  }
  .f-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

</style>
