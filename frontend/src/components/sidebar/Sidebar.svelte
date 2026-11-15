<script lang="ts">
  // column 1: the sidebar. a compose action at the top, then the unified views,
  // then every account's full folder tree, with a settings entry pinned to the
  // bottom. it renders explicit loading, error and empty (no accounts) states.
  import { createEventDispatcher } from 'svelte'
  import { IconPencil, IconRefresh, IconMailbox, IconPlus } from '@tabler/icons-svelte'
  import UnifiedViews from './UnifiedViews.svelte'
  import AccountTree from './AccountTree.svelte'
  import PinnedFolders from './PinnedFolders.svelte'
  import SavedViews from './SavedViews.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ErrorState from '../common/ErrorState.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { sidebar, loadSidebar } from '../../stores/accounts'
  import { views, openViewEditor, editViewInEditor } from '../../stores/views'
  import { syncing } from '../../stores/outbox'
  import { prefs } from '../../stores/prefs'
  import { selection } from '../../stores/selection'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { reorderAccounts } from '../../lib/api'
  import { toastError, errorMessage } from '../../stores/toast'
  import type { View } from '../../lib/types'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ compose: void; sync: void; addMailbox: void }>()

  // saved-Views placement: 'hidden' (off), 'sidebar' (a group), or 'tab' (a
  // Mail/Views toggle swapping the sidebar body).
  $: placement = $prefs.viewsPlacement
  // in tab mode, which pane the sidebar body shows. It follows where the
  // selection lands, so a saved view opened from the command palette or a
  // shortcut shows up under Views and a folder under Mail.
  let tab: 'mail' | 'views' = 'mail'
  $: if (placement === 'tab') {
    tab = $selection.kind === 'savedView' ? 'views' : 'mail'
  }

  // account email by id, so the Pinned group can say which mailbox a folder
  // belongs to.
  $: accountEmails = Object.fromEntries(($sidebar.data?.accounts ?? []).map((a) => [a.id, a.email]))

  async function onReorderAccounts(event: CustomEvent<ReorderDetail>): Promise<void> {
    try {
      await reorderAccounts(event.detail.ids.map(Number))
    } catch (err) {
      toastError(errorMessage(err))
    }
    // reload either way, so the sections end up showing what was actually stored.
    await loadSidebar()
  }

  function openNew(): void {
    openViewEditor()
  }
  function openEdit(v: View): void {
    editViewInEditor(v)
  }
</script>

<aside class="sidebar">
  <div class="top">
    <button type="button" class="compose-btn" on:click={() => dispatch('compose')}>
      <IconPencil size={16} stroke={1.8} />
      <span>{$t('action.compose')}</span>
    </button>
    <button
      type="button"
      class="sync-btn"
      class:spinning={$syncing}
      aria-label={$t('shortcut.sync')}
      title={$t('shortcut.sync')}
      on:click={() => dispatch('sync')}
    >
      <IconRefresh size={16} stroke={1.8} />
    </button>
  </div>

  <div class="scroll">
    {#if $sidebar.status === 'loading' && !$sidebar.data}
      <Spinner label={$t('sidebar.loading.mailboxes')} />
    {:else if $sidebar.status === 'error'}
      <ErrorState message={$sidebar.error} onRetry={loadSidebar} />
    {:else if $sidebar.data}
      {#if $sidebar.data.accounts.length === 0}
        <EmptyState
          title={$t('sidebar.empty.title')}
          detail={$t('sidebar.empty.detail')}
        >
          <IconMailbox size={28} stroke={1.4} />
          <button slot="action" type="button" class="add-mailbox" on:click={() => dispatch('addMailbox')}>
            <IconPlus size={15} stroke={1.8} />
            {$t('addMailbox.cta')}
          </button>
        </EmptyState>
      {:else}
        {#if placement === 'tab'}
          <div class="tabs" role="tablist">
            <button type="button" role="tab" class:on={tab === 'mail'} aria-selected={tab === 'mail'} on:click={() => (tab = 'mail')}>
              {$t('views.tab.mail')}
            </button>
            <button type="button" role="tab" class:on={tab === 'views'} aria-selected={tab === 'views'} on:click={() => (tab = 'views')}>
              {$t('views.tab.views')}
            </button>
          </div>
        {/if}
        {#if placement === 'tab' && tab === 'views'}
          <SavedViews views={$views} on:new={openNew} on:edit={(e) => openEdit(e.detail)} />
        {:else}
          <PinnedFolders folders={$sidebar.data.pinned} {accountEmails} />
          <UnifiedViews views={$sidebar.data.views} />
          {#if placement === 'sidebar'}
            <SavedViews views={$views} on:new={openNew} on:edit={(e) => openEdit(e.detail)} />
          {/if}
          <div use:reorder on:reorder={onReorderAccounts}>
            {#each $sidebar.data.accounts as account (account.id)}
              <AccountTree {account} folders={$sidebar.data.foldersByAccount[account.id] ?? []} />
            {/each}
          </div>
        {/if}
      {/if}
    {/if}
  </div>
</aside>

<style>
  .sidebar {
    display: grid;
    grid-template-rows: auto 1fr;
    height: 100%;
    background: var(--surface-base);
    border-inline-end: var(--hairline) solid var(--border-default);
    min-width: 0;
  }

  .top {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .compose-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    flex: 1;
    height: var(--control-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }

  .compose-btn:hover {
    background: var(--surface-hover);
  }

  .sync-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .sync-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .sync-btn.spinning :global(svg) {
    /* svg transform-origin defaults differ across the webviews wails embeds
       per platform (webkit vs webview2); pin it explicitly so the icon spins
       in place instead of wobbling around an off-center pivot on some os. */
    transform-box: border-box;
    transform-origin: 50% 50%;
    animation: spin 0.8s linear infinite;
  }

  /* spin counter-clockwise so the refresh arrows read as rewinding/reloading,
     which is the direction that looked right. */
  @keyframes spin {
    to {
      transform: rotate(-360deg);
    }
  }

  .scroll {
    /* min-height:0 lets the 1fr middle track scroll rather than push the footer
       off the bottom when there are many folders. */
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-3) var(--space-2);
  }

  .tabs {
    display: flex;
    gap: var(--space-1);
    padding: 0 var(--space-2) var(--space-2);
    margin-bottom: var(--space-1);
  }

  .tabs button {
    flex: 1;
    padding: var(--space-2);
    border: var(--hairline) solid transparent;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .tabs button:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tabs button.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .add-mailbox {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .add-mailbox:hover {
    background: var(--surface-hover);
  }
</style>
