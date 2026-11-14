<script lang="ts">
  // the unified cross-account views at the top of the sidebar. unified inbox is
  // the default selection. flagged shows a total (flagged are not "unread"), the
  // others show unread counts. rows drag to reorder; the order is stored as a
  // list of view keys, since the views themselves are computed, not rows.
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
  import SidebarRow from './SidebarRow.svelte'
  import type { UnifiedView, ViewKey } from '../../lib/types'
  import { selection, selectView } from '../../stores/selection'
  import { prefs } from '../../stores/prefs'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { refreshSidebar } from '../../stores/accounts'
  import { reorderUnifiedViews } from '../../lib/api'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  export let views: UnifiedView[]

  async function onReorder(event: CustomEvent<ReorderDetail>): Promise<void> {
    try {
      await reorderUnifiedViews(event.detail.ids)
    } catch (err) {
      toastError(errorMessage(err))
    }
    // reload either way, so the rows end up showing what was actually stored.
    await refreshSidebar()
  }

  const viewIcons: Record<string, typeof IconFolder> = {
    inbox: IconInbox,
    unread: IconMail,
    flagged: IconFlag,
    sent: IconSend,
    drafts: IconFile,
    archive: IconArchive,
    junk: IconAlertTriangle,
    trash: IconTrash,
  }

  // flagged and drafts read better as totals; the rest show unread. the flagged
  // count (and its bold styling, which follows count > 0) can be hidden while the
  // entry stays, via the setting.
  function badgeCount(view: UnifiedView, showFlagged: boolean): number {
    if (view.key === 'flagged') {
      return showFlagged ? view.totalCount : 0
    }
    return view.key === 'drafts' ? view.totalCount : view.unreadCount
  }

  // the built-in view names are localized here by key; the backend's English
  // label is only a fallback for an unexpected key. inbox reads "Unified Inbox"
  // to match its role as the merged, cross-account default.
  function viewLabel(view: UnifiedView): string {
    const key = view.key === 'inbox' ? 'sidebar.unifiedInbox' : `sidebar.view.${view.key}`
    const translated = $t(key)
    return translated === key ? view.label : translated
  }

  // the cast lives in script; inline ts casts in markup confuse the parser.
  function choose(view: UnifiedView): void {
    selectView(view.key as ViewKey, viewLabel(view))
  }
</script>

<nav class="views" aria-label={$t('sidebar.unifiedViews.ariaLabel')}>
  <header class="group-head">{$t('sidebar.unifiedViews.heading')}</header>
  <div class="list" use:reorder on:reorder={onReorder}>
    {#each views as view (view.key)}
      <SidebarRow
        label={viewLabel(view)}
        count={badgeCount(view, $prefs.showFlaggedCount)}
        active={$selection.kind === 'view' && $selection.view === view.key}
        reorderable
        reorderId={view.key}
        on:select={() => choose(view)}
      >
        <svelte:component this={viewIcons[view.key] ?? IconFolder} size={15} stroke={1.6} />
      </SidebarRow>
    {/each}
  </div>
</nav>

<style>
  .views {
    display: flex;
    flex-direction: column;
    gap: 1px;
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
