<script lang="ts">
  // the list of rooms: invites first, then favourites, people (direct chats),
  // rooms and low priority, each newest first. A search narrows it; when the
  // account is in spaces, a picker limits it to one space's rooms. Arrow keys
  // in the search move through the rows and Enter opens one.
  import { createEventDispatcher } from 'svelte'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-circle-duotone.svg?raw'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import chatIcon from '@phosphor-icons/core/assets/duotone/chats-circle-duotone.svg?raw'
  import ChatAvatar from './ChatAvatar.svelte'
  import { chatRooms, refreshRooms, chatError } from '../../stores/chat'
  import { chatJoin, chatLeave } from '../../lib/api'
  import { toastError } from '../../stores/toast'
  import { formatListDate, type TimeFormat } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'
  import { roomSections, previewLine } from '../../lib/chatrooms'
  import type { ChatRoom } from '../../lib/types'

  /** The room on screen, highlighted in the list. */
  export let activeId: string | null = null
  /** True while the first sync has not brought any rooms yet. */
  export let loading: boolean = false

  const dispatch = createEventDispatcher<{ open: string; new: void }>()

  let query = ''
  let spaceFilter = ''
  let searchInput: HTMLInputElement
  let cursor = -1
  let busy = new Set<string>()

  $: spaces = $chatRooms.filter((r) => r.kind === 'space' && r.membership === 'join')
  $: if (spaceFilter && !spaces.some((s) => s.id === spaceFilter)) {
    spaceFilter = ''
  }
  $: sections = roomSections($chatRooms, query, spaceFilter)
  $: flat = sections.flatMap((s) => s.rooms)
  $: if (cursor >= flat.length) {
    cursor = flat.length - 1
  }

  /** Puts the cursor in the search field. */
  export function focusSearch(): void {
    searchInput?.focus()
    searchInput?.select()
  }

  function onSearchKey(event: KeyboardEvent): void {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      cursor = Math.min(flat.length - 1, cursor + 1)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      cursor = Math.max(0, cursor - 1)
    } else if (event.key === 'Enter') {
      const target = flat[cursor] ?? flat[0]
      if (target && target.membership === 'join') {
        event.preventDefault()
        dispatch('open', target.id)
      }
    } else if (event.key === 'Escape' && query) {
      event.stopPropagation()
      query = ''
    }
  }

  function time(room: ChatRoom): string {
    return room.lastTs ? formatListDate(new Date(room.lastTs).toISOString(), $prefs.timeFormat as TimeFormat) : ''
  }

  async function answerInvite(room: ChatRoom, accept: boolean): Promise<void> {
    busy = new Set(busy).add(room.id)
    try {
      if (accept) {
        const id = await chatJoin(room.id)
        await refreshRooms()
        dispatch('open', id)
      } else {
        await chatLeave(room.id)
        await refreshRooms()
      }
    } catch (err) {
      toastError(chatError(err))
    } finally {
      const next = new Set(busy)
      next.delete(room.id)
      busy = next
    }
  }
</script>

<nav class="rooms" aria-label={$t('chat.rooms.label')}>
  <div class="head">
    <label class="search">
      <Ph svg={searchIcon} size={14} />
      <input
        bind:this={searchInput}
        bind:value={query}
        type="search"
        placeholder={$t('chat.rooms.search')}
        aria-label={$t('chat.rooms.search')}
        on:keydown={onSearchKey}
      />
    </label>
    <button type="button" class="new" aria-label={$t('chat.new.title')} title={$t('chat.new.title')} on:click={() => dispatch('new')}>
      <Ph svg={plusIcon} size={17} />
    </button>
  </div>
  {#if spaces.length > 0}
    <div class="spaces">
      <select bind:value={spaceFilter} aria-label={$t('chat.rooms.space')}>
        <option value="">{$t('chat.rooms.allSpaces')}</option>
        {#each spaces as s (s.id)}
          <option value={s.id}>{s.name || s.alias || s.id}</option>
        {/each}
      </select>
    </div>
  {/if}

  <div class="scroll">
    {#if loading}
      <div class="note">
        <PieProgress label={$t('chat.space.loading')} />
      </div>
    {:else if flat.length === 0 && (query || spaceFilter)}
      <EmptyState
        kind="filtered"
        icon={searchIcon}
        title={$t('chat.search.empty')}
        detail={$t('chat.search.emptyDetail')}
        actionLabel={$t('common.clearFilter')}
        onAction={() => {
          query = ''
          spaceFilter = ''
        }}
      />
    {:else if flat.length === 0}
      <EmptyState
        icon={chatIcon}
        title={$t('chat.rooms.none')}
        detail={$t('chat.rooms.noneDetail')}
        actionLabel={$t('chat.new.title')}
        onAction={() => dispatch('new')}
      />
    {/if}
    {#each sections as section (section.key)}
      <h3 class="section">{$t(section.labelKey)}</h3>
      <ul role="listbox" aria-label={$t(section.labelKey)}>
        {#each section.rooms as room (room.id)}
          {@const index = flat.indexOf(room)}
          <li role="option" aria-selected={room.id === activeId}>
            {#if room.membership === 'invite'}
              <div class="row invite" class:cursor={index === cursor}>
                <ChatAvatar mxc={room.avatar} name={room.name} seed={room.id} size="var(--chat-room-avatar)" />
                <div class="text">
                  <span class="name">{room.name || room.id}</span>
                  <span class="preview">{$t('chat.rooms.invitedBy').replace('{name}', room.inviterName || room.inviterId)}</span>
                  <span class="invite-actions">
                    {#if busy.has(room.id)}
                      <PieProgress label={$t('chat.rooms.joining')} />
                    {:else}
                      <button type="button" class="small primary" on:click={() => void answerInvite(room, true)}>{$t('chat.rooms.accept')}</button>
                      <button type="button" class="small" on:click={() => void answerInvite(room, false)}>{$t('chat.rooms.decline')}</button>
                    {/if}
                  </span>
                </div>
              </div>
            {:else}
              <button
                type="button"
                class="row"
                class:active={room.id === activeId}
                class:cursor={index === cursor}
                class:unread={room.unread > 0}
                on:click={() => dispatch('open', room.id)}
              >
                <ChatAvatar mxc={room.avatar} name={room.name} seed={room.directUser || room.id} size="var(--chat-room-avatar)" />
                <span class="text">
                  <span class="line">
                    <span class="name">{room.name || room.alias || room.id}</span>
                    {#if room.encrypted && room.kind !== 'direct'}
                      <span class="lock" title={$t('chat.room.encrypted')}><Ph svg={lockIcon} size={11} /></span>
                    {/if}
                    <span class="time">{time(room)}</span>
                  </span>
                  <span class="line">
                    <span class="preview">{previewLine(room, $t)}</span>
                    {#if room.unread > 0}
                      <span class="count" class:highlight={room.highlight > 0 || room.kind === 'direct'}>{room.unread > 99 ? '99+' : room.unread}</span>
                    {/if}
                  </span>
                </span>
              </button>
            {/if}
          </li>
        {/each}
      </ul>
    {/each}
  </div>
</nav>

<style>
  .rooms {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    border-right: var(--hairline) solid var(--border-subtle);
    background: var(--sidebar-bg);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--pane-pad) var(--pane-pad) var(--space-2);
  }

  .search {
    flex: 1;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-tertiary);
  }
  .search:focus-within {
    border-color: var(--accent);
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }

  .new {
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
  .new:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .spaces {
    padding: 0 var(--pane-pad) var(--space-2);
  }
  .spaces select {
    width: 100%;
    height: var(--control-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }

  .scroll {
    flex: 1;
    overflow-y: auto;
    padding-bottom: var(--space-3);
  }

  .note {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    padding: var(--space-3) var(--pane-pad);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .section {
    margin: var(--space-3) 0 var(--space-1);
    padding: 0 var(--pane-pad);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  ul {
    margin: 0;
    padding: 0 var(--space-2);
    list-style: none;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--row-pad-y) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: left;
    cursor: var(--cursor-action);
  }
  .row:hover {
    background: var(--surface-hover);
  }
  .row.cursor {
    box-shadow: inset 0 0 0 var(--hairline) var(--border-strong);
  }
  .row.active {
    background: var(--selection-bg);
  }
  .row.invite {
    cursor: default;
    align-items: flex-start;
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--row-gap);
    flex: 1;
    min-width: 0;
  }

  .line {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    min-width: 0;
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-list);
  }
  .unread .name {
    font-weight: var(--fw-semibold);
  }
  .invite .name {
    flex: none;
  }

  .lock {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .time {
    flex-shrink: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .preview {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }

  .count {
    flex-shrink: 0;
    min-width: 1.5em;
    padding: 0 var(--space-1);
    border-radius: 999px;
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    text-align: center;
  }
  .count.highlight {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .invite-actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-1);
  }

  .small {
    height: calc(var(--control-height) - var(--space-3));
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }
  .small.primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }
</style>
