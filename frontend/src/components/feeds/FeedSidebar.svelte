<script lang="ts">
  // the Feeds sidebar: the smart views with their counts, then the feeds that
  // sit in no folder and the folders with theirs. a feed is dragged onto a
  // folder to move it, or onto another feed to put it there; a folder is
  // dragged onto another to reorder them. a feed whose last fetch failed shows
  // a warning with the reason. each feed and folder has a menu, on its button
  // and on right click.
  import { createEventDispatcher, tick } from 'svelte'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import dotsIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import caretIcon from '@phosphor-icons/core/assets/duotone/caret-right-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-simple-duotone.svg?raw'
  import folderPlusIcon from '@phosphor-icons/core/assets/duotone/folder-simple-plus-duotone.svg?raw'
  import unreadIcon from '@phosphor-icons/core/assets/duotone/tray-duotone.svg?raw'
  import todayIcon from '@phosphor-icons/core/assets/duotone/sun-duotone.svg?raw'
  import starIcon from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import laterIcon from '@phosphor-icons/core/assets/duotone/bookmark-simple-duotone.svg?raw'
  import newsIcon from '@phosphor-icons/core/assets/duotone/newspaper-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import SpaceSwitcher from '../shell/SpaceSwitcher.svelte'
  import FeedIcon from './FeedIcon.svelte'
  import { createFeedFolder, deleteFeedFolder, moveFeeds, refreshFeed, renameFeedFolder, reorderFeedFolders } from '../../lib/api'
  import { dropOrder, feedsIn, folderOrder, folderUnread, sameScope, smartViews } from '../../lib/feedlogic'
  import { feedSidebar, loadFeedSidebar, newsletterUnread } from '../../stores/feeds'
  import { askConfirm } from '../../stores/confirm'
  import { openContextMenu, type MenuEntry } from '../../stores/contextmenu'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError } from '../../stores/toast'
  import { openURL } from '../../lib/browser'
  import { t } from '../../lib/i18n'
  import type { Feed, FeedFolder, FeedScope, FeedSmartView } from '../../lib/types'

  /** What the list shows. */
  export let scope: FeedScope

  const dispatch = createEventDispatcher<{
    scope: FeedScope
    add: number
    settings: number
    markRead: FeedScope
  }>()

  const smartIcons: Record<FeedSmartView, string> = {
    unread: unreadIcon,
    today: todayIcon,
    starred: starIcon,
    later: laterIcon,
    newsletters: newsIcon,
  }

  $: side = $feedSidebar
  $: counts = {
    unread: side.counts.unread,
    today: side.counts.today,
    starred: side.counts.starred,
    later: side.counts.readLater,
    newsletters: $newsletterUnread,
  } as Record<FeedSmartView, number>
  $: loose = feedsIn(side.feeds, 0)
  $: folders = [...side.folders].sort((a, b) => a.position - b.position || a.id - b.id)

  // folders folded shut, for this session.
  let closed = new Set<number>()

  function toggleFolder(id: number): void {
    const next = new Set(closed)
    if (!next.delete(id)) {
      next.add(id)
    }
    closed = next
  }

  function isOn(next: FeedScope): boolean {
    return sameScope(next, scope)
  }

  // --- menus -------------------------------------------------------------------------

  function feedMenu(feed: Feed, x: number, y: number): void {
    const entries: MenuEntry[] = [
      { label: $t('feeds.menu.refresh'), action: () => void refreshOne(feed) },
      { label: $t('feeds.markAllRead'), action: () => dispatch('markRead', { kind: 'feed', id: feed.id }) },
    ]
    if (feed.siteUrl) {
      entries.push({ label: $t('feeds.menu.openSite'), action: () => openURL(feed.siteUrl) })
    }
    entries.push('separator', { label: $t('feeds.menu.settings'), action: () => dispatch('settings', feed.id) })
    openContextMenu(x, y, entries)
  }

  function folderMenu(folder: FeedFolder, x: number, y: number, anchor: HTMLElement | null): void {
    openContextMenu(x, y, [
      { label: $t('feeds.markAllRead'), action: () => dispatch('markRead', { kind: 'folder', id: folder.id }) },
      { label: $t('feeds.addToFolder'), action: () => dispatch('add', folder.id) },
      'separator',
      { label: $t('feeds.folder.rename'), action: () => startRename(folder, anchor) },
      { label: $t('feeds.folder.delete'), action: () => void removeFolder(folder), danger: true },
    ])
  }

  function fromButton(event: MouseEvent, run: (x: number, y: number) => void): void {
    const r = (event.currentTarget as HTMLElement).getBoundingClientRect()
    run(r.left, r.bottom)
  }

  async function refreshOne(feed: Feed): Promise<void> {
    try {
      await refreshFeed(feed.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // --- folders -----------------------------------------------------------------------

  let formFor: 'new' | FeedFolder | null = null
  let formAnchor: HTMLElement | null = null
  let formName = ''
  let newAnchor: HTMLElement
  let nameInput: HTMLInputElement

  async function openForm(target: 'new' | FeedFolder, anchor: HTMLElement | null): Promise<void> {
    formFor = target
    formAnchor = anchor ?? newAnchor
    formName = target === 'new' ? '' : target.name
    await tick()
    nameInput?.focus()
    nameInput?.select()
  }

  function startRename(folder: FeedFolder, anchor: HTMLElement | null): void {
    void openForm(folder, anchor ?? document.querySelector<HTMLElement>(`[data-feed-folder="${folder.id}"]`))
  }

  async function submitForm(): Promise<void> {
    const name = formName.trim()
    const target = formFor
    if (!name || !target) {
      return
    }
    formFor = null
    try {
      if (target === 'new') {
        await createFeedFolder(name)
      } else {
        await renameFeedFolder(target.id, name)
      }
      await loadFeedSidebar()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function removeFolder(folder: FeedFolder): Promise<void> {
    const ok = await askConfirm({
      title: $t('feeds.folder.deleteTitle').replace('{name}', folder.name),
      body: $t('feeds.folder.deleteBody'),
      confirmLabel: $t('feeds.folder.delete'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await deleteFeedFolder(folder.id)
      if (scope.kind === 'folder' && scope.id === folder.id) {
        dispatch('scope', { kind: 'unread' })
      }
      await loadFeedSidebar()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // --- dragging ----------------------------------------------------------------------

  const feedType = 'application/x-pelton-feed'
  const folderType = 'application/x-pelton-feed-folder'

  let dragging: { kind: 'feed' | 'folder'; id: number } | null = null
  let over = ''

  function startDrag(event: DragEvent, kind: 'feed' | 'folder', id: number): void {
    dragging = { kind, id }
    event.dataTransfer?.setData(kind === 'feed' ? feedType : folderType, String(id))
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move'
    }
  }

  function endDrag(): void {
    dragging = null
    over = ''
  }

  function allow(event: DragEvent, target: string, accepts: 'feed' | 'any'): void {
    if (!dragging || (accepts === 'feed' && dragging.kind !== 'feed')) {
      return
    }
    event.preventDefault()
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'move'
    }
    over = target
  }

  async function dropOnFeed(event: DragEvent, target: Feed): Promise<void> {
    event.preventDefault()
    const drag = dragging
    endDrag()
    if (!drag || drag.kind !== 'feed' || drag.id === target.id) {
      return
    }
    await applyMove(drag.id, target.folderId, target.id)
  }

  async function dropOnFolder(event: DragEvent, folderId: number): Promise<void> {
    event.preventDefault()
    const drag = dragging
    endDrag()
    if (!drag) {
      return
    }
    if (drag.kind === 'feed') {
      await applyMove(drag.id, folderId, null)
      return
    }
    if (drag.id !== folderId && folderId !== 0) {
      try {
        await reorderFeedFolders(folderOrder(folders.map((f) => f.id), drag.id, folderId))
        await loadFeedSidebar()
      } catch (err) {
        toastError(errorMessage(err))
      }
    }
  }

  async function applyMove(feedId: number, folderId: number, before: number | null): Promise<void> {
    const ids = dropOrder(side.feeds, feedId, folderId, before)
    feedSidebar.update((s) => ({
      ...s,
      feeds: s.feeds.map((f) => (ids.includes(f.id) ? { ...f, folderId, position: ids.indexOf(f.id) } : f)),
    }))
    try {
      await moveFeeds(folderId, ids)
    } catch (err) {
      toastError(errorMessage(err))
    }
    await loadFeedSidebar()
  }
</script>

<aside class="side" aria-label={$t('feeds.sidebar')}>
  <ul class="smart" role="list">
    {#each smartViews as key (key)}
      <li>
        <button
          type="button"
          class="entry"
          class:on={isOn({ kind: key })}
          aria-current={isOn({ kind: key }) ? 'page' : undefined}
          on:click={() => dispatch('scope', { kind: key })}
        >
          <Ph svg={smartIcons[key]} size={16} />
          <span class="name">{$t(`feeds.smart.${key}`)}</span>
          {#if counts[key] > 0}
            <span class="count">{counts[key]}</span>
          {/if}
        </button>
      </li>
    {/each}
  </ul>

  <div class="feeds">
    <h3>
      <span class="label">{$t('feeds.subscriptions')}</span>
      <button type="button" class="cal-icon tiny" aria-label={$t('feeds.folder.new')} title={$t('feeds.folder.new')} bind:this={newAnchor} on:click={() => void openForm('new', newAnchor)}>
        <Ph svg={folderPlusIcon} size={14} />
      </button>
      <button type="button" class="cal-icon tiny" aria-label={$t('feeds.addFeed')} title={$t('feeds.addFeed')} on:click={() => dispatch('add', 0)}>
        <Ph svg={plusIcon} size={13} />
      </button>
    </h3>

    {#if side.feeds.length === 0}
      <p class="hint">{$t('feeds.sidebarEmpty')}</p>
    {/if}

    <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
    <ul
      class="list loose"
      class:over={over === 'folder:0'}
      role="list"
      on:dragover={(e) => allow(e, 'folder:0', 'feed')}
      on:dragleave={() => over === 'folder:0' && (over = '')}
      on:drop={(e) => void dropOnFolder(e, 0)}
    >
      {#each loose as feed (feed.id)}
        {@const on = isOn({ kind: 'feed', id: feed.id })}
        <li
          class="row"
          class:on
          class:over={over === `feed:${feed.id}`}
          class:dragged={dragging?.kind === 'feed' && dragging.id === feed.id}
          draggable="true"
          on:dragstart={(e) => startDrag(e, 'feed', feed.id)}
          on:dragend={endDrag}
          on:dragover|stopPropagation={(e) => allow(e, `feed:${feed.id}`, 'feed')}
          on:drop|stopPropagation={(e) => void dropOnFeed(e, feed)}
          on:contextmenu|preventDefault={(e) => feedMenu(feed, e.clientX, e.clientY)}
        >
          <button type="button" class="entry" aria-current={on ? 'page' : undefined} on:click={() => dispatch('scope', { kind: 'feed', id: feed.id })}>
            <FeedIcon icon={feed.icon} />
            <span class="name" dir="auto">{feed.title}</span>
            {#if feed.error}
              <span class="warn" title={$t('feeds.errorBadge').replace('{reason}', feed.error)} aria-label={$t('feeds.errorBadge').replace('{reason}', feed.error)}>
                <Ph svg={warningIcon} size={13} />
              </span>
            {/if}
            {#if feed.unread > 0}
              <span class="count">{feed.unread}</span>
            {/if}
          </button>
          <button type="button" class="cal-icon more" aria-label={$t('feeds.feedMenu').replace('{name}', feed.title)} on:click={(e) => fromButton(e, (x, y) => feedMenu(feed, x, y))}>
            <Ph svg={dotsIcon} size={14} />
          </button>
        </li>
      {/each}
    </ul>

    {#each folders as folder (folder.id)}
      {@const open = !closed.has(folder.id)}
      {@const on = isOn({ kind: 'folder', id: folder.id })}
      {@const unread = folderUnread(side.feeds, folder.id)}
      <section class="folder">
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div
          class="row head"
          class:on
          class:over={over === `folder:${folder.id}`}
          class:dragged={dragging?.kind === 'folder' && dragging.id === folder.id}
          data-feed-folder={folder.id}
          draggable="true"
          on:dragstart={(e) => startDrag(e, 'folder', folder.id)}
          on:dragend={endDrag}
          on:dragover={(e) => allow(e, `folder:${folder.id}`, 'any')}
          on:dragleave={() => over === `folder:${folder.id}` && (over = '')}
          on:drop={(e) => void dropOnFolder(e, folder.id)}
          on:contextmenu|preventDefault={(e) => folderMenu(folder, e.clientX, e.clientY, e.currentTarget)}
        >
          <button
            type="button"
            class="twisty"
            class:open
            aria-expanded={open}
            aria-label={open ? $t('feeds.folder.collapse') : $t('feeds.folder.expand')}
            on:click={() => toggleFolder(folder.id)}
          >
            <Ph svg={caretIcon} size={12} />
          </button>
          <button type="button" class="entry" aria-current={on ? 'page' : undefined} on:click={() => dispatch('scope', { kind: 'folder', id: folder.id })}>
            <Ph svg={folderIcon} size={15} />
            <span class="name" dir="auto">{folder.name}</span>
            {#if unread > 0}
              <span class="count">{unread}</span>
            {/if}
          </button>
          <button type="button" class="cal-icon more" aria-label={$t('feeds.folderMenu').replace('{name}', folder.name)} on:click={(e) => fromButton(e, (x, y) => folderMenu(folder, x, y, e.currentTarget instanceof HTMLElement ? e.currentTarget : null))}>
            <Ph svg={dotsIcon} size={14} />
          </button>
        </div>
        {#if open}
          <ul class="list nested" role="list">
            {#each feedsIn(side.feeds, folder.id) as feed (feed.id)}
              {@const feedOn = isOn({ kind: 'feed', id: feed.id })}
              <li
                class="row"
                class:on={feedOn}
                class:over={over === `feed:${feed.id}`}
                class:dragged={dragging?.kind === 'feed' && dragging.id === feed.id}
                draggable="true"
                on:dragstart={(e) => startDrag(e, 'feed', feed.id)}
                on:dragend={endDrag}
                on:dragover|stopPropagation={(e) => allow(e, `feed:${feed.id}`, 'feed')}
                on:drop|stopPropagation={(e) => void dropOnFeed(e, feed)}
                on:contextmenu|preventDefault={(e) => feedMenu(feed, e.clientX, e.clientY)}
              >
                <button type="button" class="entry" aria-current={feedOn ? 'page' : undefined} on:click={() => dispatch('scope', { kind: 'feed', id: feed.id })}>
                  <FeedIcon icon={feed.icon} />
                  <span class="name" dir="auto">{feed.title}</span>
                  {#if feed.error}
                    <span class="warn" title={$t('feeds.errorBadge').replace('{reason}', feed.error)} aria-label={$t('feeds.errorBadge').replace('{reason}', feed.error)}>
                      <Ph svg={warningIcon} size={13} />
                    </span>
                  {/if}
                  {#if feed.unread > 0}
                    <span class="count">{feed.unread}</span>
                  {/if}
                </button>
                <button type="button" class="cal-icon more" aria-label={$t('feeds.feedMenu').replace('{name}', feed.title)} on:click={(e) => fromButton(e, (x, y) => feedMenu(feed, x, y))}>
                  <Ph svg={dotsIcon} size={14} />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/each}
  </div>

  <div class="foot">
    <button type="button" class="cal-item" on:click={() => dispatch('add', 0)}>
      <Ph svg={plusIcon} size={13} />
      <span>{$t('feeds.addFeed')}</span>
    </button>
    {#if $prefs.navStyle === 'footer'}
      <div class="switch"><SpaceSwitcher /></div>
    {/if}
  </div>
</aside>

{#if formFor && formAnchor}
  <AnchoredPopover anchor={formAnchor} align="start" label={formFor === 'new' ? $t('feeds.folder.new') : $t('feeds.folder.rename')} on:close={() => (formFor = null)}>
    <form class="cal-menu rename" on:submit|preventDefault={submitForm}>
      <input class="cal-field" bind:this={nameInput} bind:value={formName} placeholder={$t('feeds.folder.name')} aria-label={$t('feeds.folder.name')} />
      <button type="submit" class="cal-btn primary" disabled={!formName.trim()}>
        {formFor === 'new' ? $t('feeds.folder.create') : $t('feeds.save')}
      </button>
    </form>
  </AnchoredPopover>
{/if}

<style>
  .side {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    width: var(--cal-sidebar-width);
    height: 100%;
    padding: var(--space-4) var(--space-4) var(--space-3);
    overflow-y: auto;
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .smart {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .entry {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    width: 100%;
    height: calc(var(--control-height) - 2px);
    padding-inline: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .smart .entry {
    color: var(--text-secondary);
  }

  .entry:hover,
  .row:hover {
    background: var(--surface-hover);
  }

  .row:hover .entry {
    background: transparent;
  }

  .entry.on,
  .row.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .entry:focus-visible,
  .twisty:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .feeds {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
  }

  h3 {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    margin: 0 0 var(--space-1);
    padding-inline-start: var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tiny {
    width: calc(var(--control-height) - 8px);
    height: calc(var(--control-height) - 8px);
    color: var(--text-tertiary);
  }

  .tiny:hover {
    color: var(--text-primary);
  }

  .hint {
    margin: 0;
    padding: 0 var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 1px;
    border-radius: var(--radius-control);
  }

  .list.loose {
    min-height: var(--space-2);
  }

  .list.loose.over {
    box-shadow: inset 0 0 0 var(--hairline) var(--accent);
  }

  .nested {
    padding-inline-start: var(--space-4);
  }

  .row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-control);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out);
  }

  .row.over {
    box-shadow: inset 0 calc(var(--hairline) * 2) 0 var(--accent);
  }

  .row.head.over {
    background: var(--selection-bg-strong);
    box-shadow: none;
  }

  .row.dragged {
    opacity: 0.45;
  }

  .warn {
    display: inline-flex;
    color: var(--warning);
  }

  .more {
    margin-inline-end: var(--space-1);
    opacity: 0;
  }

  .row:hover .more,
  .more:focus-visible {
    opacity: 1;
  }

  .twisty {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--thread-twisty);
    height: var(--thread-twisty);
    margin-inline-start: var(--space-1);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .twisty :global(.ph) {
    transition: transform var(--motion-base) var(--ease-out);
  }

  .twisty.open :global(.ph) {
    transform: rotate(90deg);
  }

  :global([dir='rtl']) .twisty:not(.open) :global(.ph) {
    transform: scaleX(-1);
  }

  .head .entry {
    padding-inline-start: var(--space-1);
  }

  .foot {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .foot .cal-item {
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .foot .cal-item:hover {
    color: var(--text-primary);
  }

  .rename {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-1);
  }

  .rename .cal-field {
    flex: 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .twisty :global(.ph),
    .row,
    .entry {
      transition: none;
    }
  }
</style>
