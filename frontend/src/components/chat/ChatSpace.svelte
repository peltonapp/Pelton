<script lang="ts">
  // the chat space: the room list on the left, the open room's timeline and
  // composer beside it, and room info on the right when asked for. In a
  // narrow window only one of list and room shows at a time. Signed out, it
  // offers the sign-in form instead.
  import { onDestroy, onMount, tick } from 'svelte'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import RoomList from './RoomList.svelte'
  import RoomView from './RoomView.svelte'
  import RoomInfo from './RoomInfo.svelte'
  import NewChatDialog from './NewChatDialog.svelte'
  import ChatSignIn from './ChatSignIn.svelte'
  import { activeRoomId, chatRooms, chatStatus, closeRoom, openRoom, refreshRooms } from '../../stores/chat'
  import { registerSpaceActions } from '../../stores/space'
  import { registerSpaceSearch, matchesQuery, focusSpaceSearch } from '../../lib/spacesearch'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { popupIsOpen } from '../../lib/popups'
  import { t } from '../../lib/i18n'
  import { chatSetViewing } from '../../lib/api'

  let host: HTMLElement
  let list: RoomList
  let width = 0
  let narrowAt = 720
  let infoOpen = false
  let newChatOpen = false
  let unregister: (() => void) | undefined
  let stopSearch: (() => void) | undefined
  let observer: ResizeObserver | undefined

  $: narrow = width > 0 && width < narrowAt
  $: room = $chatRooms.find((r) => r.id === $activeRoomId) ?? null
  $: signedOut = $chatStatus.state === 'signedOut'
  $: firstLoad = ($chatStatus.state === 'starting' || $chatStatus.state === 'syncing') && $chatRooms.length === 0
  // the info panel takes room the timeline needs in a narrow window, so it
  // shows instead of the room there.
  $: showInfo = infoOpen && room !== null

  onMount(() => {
    const token = getComputedStyle(document.documentElement).getPropertyValue('--chat-narrow')
    narrowAt = parseInt(token, 10) || narrowAt
    observer = new ResizeObserver(() => (width = host?.clientWidth ?? 0))
    observer.observe(host)
    width = host.clientWidth
    if ($activeRoomId) {
      void chatSetViewing($activeRoomId)
    }
    void refreshRooms()
    unregister = registerSpaceActions(
      'chat',
      {
        compose: () => whenFree(() => (newChatOpen = true)),
        search: () => {
          if (!focusSpaceSearch()) {
            list?.focusSearch()
          }
        },
      },
      { compose: 'chat.new.title' },
    )
    stopSearch = registerSpaceSearch('chat', {
      placeholderKey: 'chat.search.placeholder',
      emptyKey: 'chat.search.empty',
      search: (query) =>
        $chatRooms
          .filter((r) => r.kind !== 'space' && matchesQuery(query, [r.name, r.alias, r.topic, r.directUser]))
          .slice(0, 20)
          .map((r) => ({
            id: r.id,
            title: r.name || r.alias || r.id,
            detail: r.kind === 'direct' ? r.directUser : r.topic,
          })),
      open: (result) => void select(result.id),
    })
  })

  onDestroy(() => {
    observer?.disconnect()
    unregister?.()
    stopSearch?.()
    void chatSetViewing('')
  })

  function whenFree(run: () => void): void {
    if (!newChatOpen && !popupIsOpen()) {
      run()
    }
  }

  async function select(roomId: string): Promise<void> {
    await openRoom(roomId)
    await tick()
    host?.querySelector<HTMLTextAreaElement>('[data-chat-composer]')?.focus()
  }

  function back(): void {
    infoOpen = false
    closeRoom()
    void tick().then(() => list?.focusSearch())
  }
</script>

<section class="chat" class:narrow bind:this={host} aria-label={$t('space.chat')}>
  {#if signedOut}
    <div class="signin">
      <div class="signin-card">
        <h2>{$t('chat.space.signInTitle')}</h2>
        <p class="lead">{$t('chat.space.signInLead')}</p>
        <ChatSignIn />
      </div>
    </div>
  {:else}
    {#if $chatStatus.state === 'error'}
      <div class="banner danger" role="alert">
        <Ph svg={warningIcon} size={15} />
        <span>{$chatStatus.error}</span>
        <button type="button" class="link" on:click={() => openSettingsAt('chat')}>{$t('chat.space.openSettings')}</button>
      </div>
    {:else if $chatStatus.state === 'offline'}
      <div class="banner" role="status">
        <PieProgress label={$t('chat.space.reconnecting')} />
        <span>{$t('chat.space.reconnecting')}</span>
      </div>
    {/if}
    <div class="panes" class:with-info={showInfo && !narrow}>
      {#if !narrow || !room}
        <RoomList
          bind:this={list}
          activeId={$activeRoomId}
          loading={firstLoad}
          on:open={(e) => void select(e.detail)}
          on:new={() => (newChatOpen = true)}
        />
      {/if}
      {#if room && !(narrow && showInfo)}
        <RoomView {room} {narrow} infoOpen={showInfo} on:back={back} on:info={() => (infoOpen = !infoOpen)} />
      {:else if !narrow}
        <div class="empty">
          {#if firstLoad}
            <PieProgress size={20} label={$t('chat.space.loading')} />
            <p>{$t('chat.space.loading')}</p>
          {:else}
            <p>{$t('chat.space.pickRoom')}</p>
            <button type="button" class="btn" on:click={() => (newChatOpen = true)}>{$t('chat.new.title')}</button>
          {/if}
        </div>
      {/if}
      {#if showInfo && room}
        <RoomInfo {room} {narrow} on:close={() => (infoOpen = false)} />
      {/if}
    </div>
  {/if}
</section>

{#if newChatOpen}
  <NewChatDialog on:close={() => (newChatOpen = false)} on:opened={(e) => void select(e.detail)} />
{/if}

<style>
  .chat {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    min-width: 0;
    background: var(--surface-base);
    color: var(--text-primary);
  }

  .panes {
    flex: 1;
    display: grid;
    grid-template-columns: var(--chat-list-width) minmax(0, 1fr);
    min-height: 0;
  }
  .panes.with-info {
    grid-template-columns: var(--chat-list-width) minmax(0, 1fr) var(--chat-info-width);
  }
  .narrow .panes {
    grid-template-columns: minmax(0, 1fr);
  }

  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    color: var(--text-tertiary);
    font-size: var(--fz-body);
  }
  .empty p {
    margin: 0;
  }

  .banner {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }
  .banner.danger {
    background: var(--danger-bg);
    color: var(--danger);
  }
  .banner span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .link {
    margin-left: auto;
    padding: 0;
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    text-decoration: underline;
    cursor: var(--cursor-action);
  }

  .signin {
    flex: 1;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: var(--space-6) var(--pane-pad);
    overflow: auto;
  }
  .signin-card {
    width: min(var(--chat-signin-width), 100%);
  }
  .signin-card h2 {
    margin: 0 0 var(--space-1);
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
  }
  .lead {
    margin: 0 0 var(--space-4);
    color: var(--text-secondary);
    font-size: var(--fz-body);
  }

  .btn {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    cursor: var(--cursor-action);
  }
  .btn:hover {
    background: var(--surface-hover);
  }
</style>
