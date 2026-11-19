<script lang="ts">
  // one open room: its header, the banner for an unverified device, the
  // timeline, who is typing and the composer. Files dropped from the system
  // anywhere on it go to the composer to be sent.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import arrowLeftIcon from '@phosphor-icons/core/assets/duotone/arrow-left-duotone.svg?raw'
  import infoIcon from '@phosphor-icons/core/assets/duotone/info-duotone.svg?raw'
  import starIcon from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import shieldIcon from '@phosphor-icons/core/assets/duotone/shield-warning-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import ChatAvatar from './ChatAvatar.svelte'
  import Timeline from './Timeline.svelte'
  import Composer from './Composer.svelte'
  import { chatStatus, typing, chatError } from '../../stores/chat'
  import { chatSetFavourite, chatStartVerification } from '../../lib/api'
  import { onFilesDropped } from '../../lib/events'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { toastError, toastInfo } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { ChatMessage, ChatRoom } from '../../lib/types'

  /** The room shown. */
  export let room: ChatRoom
  /** True in a narrow window, where the header offers a way back to the list. */
  export let narrow: boolean = false
  /** True while the room info panel is open. */
  export let infoOpen: boolean = false

  const dispatch = createEventDispatcher<{ back: void; info: void }>()

  let composer: Composer
  let replyTo: ChatMessage | null = null
  let editing: ChatMessage | null = null
  let lastRoom = ''
  let stopDrop: (() => void) | undefined

  // a reply or edit belongs to the room it was started in.
  $: if (room.id !== lastRoom) {
    lastRoom = room.id
    replyTo = null
    editing = null
  }

  $: typists = $typing[room.id] ?? []
  $: unverified = $chatStatus.encryption.ready && !$chatStatus.encryption.deviceVerified

  onMount(() => {
    stopDrop = onFilesDropped((e) => {
      if (e.target === 'chat-room') {
        composer?.stagePaths(e.paths)
      }
    })
  })

  onDestroy(() => stopDrop?.())

  function typingLine(names: string[]): string {
    if (names.length === 1) {
      return $t('chat.typing.one').replace('{name}', names[0])
    }
    if (names.length === 2) {
      return $t('chat.typing.two').replace('{a}', names[0]).replace('{b}', names[1])
    }
    return $t('chat.typing.many')
  }

  async function toggleFavourite(): Promise<void> {
    try {
      await chatSetFavourite(room.id, !room.favourite)
    } catch (err) {
      toastError(chatError(err))
    }
  }

  async function verify(): Promise<void> {
    try {
      await chatStartVerification('')
      toastInfo($t('chat.verify.requestSent'))
    } catch (err) {
      toastError(chatError(err))
    }
  }
</script>

<section class="room" aria-label={room.name} data-file-drop-target="chat-room">
  <header>
    {#if narrow}
      <button type="button" class="icon" aria-label={$t('chat.room.back')} title={$t('chat.room.back')} on:click={() => dispatch('back')}>
        <Ph svg={arrowLeftIcon} size={17} />
      </button>
    {/if}
    <ChatAvatar mxc={room.avatar} name={room.name} seed={room.directUser || room.id} />
    <div class="title">
      <h2>
        <span class="name">{room.name || room.alias || room.id}</span>
        {#if room.encrypted}
          <span class="lock" title={$t('chat.room.encrypted')}><Ph svg={lockIcon} size={13} /></span>
        {/if}
      </h2>
      {#if room.kind === 'direct' && room.directUser}
        <p class="sub">{room.directUser}</p>
      {:else if room.topic}
        <p class="sub" title={room.topic}>{room.topic}</p>
      {/if}
    </div>
    <button
      type="button"
      class="icon"
      class:on={room.favourite}
      aria-pressed={room.favourite}
      aria-label={room.favourite ? $t('chat.room.unfavourite') : $t('chat.room.favourite')}
      title={room.favourite ? $t('chat.room.unfavourite') : $t('chat.room.favourite')}
      on:click={() => void toggleFavourite()}
    >
      <Ph svg={starIcon} size={17} />
    </button>
    <button
      type="button"
      class="icon"
      class:on={infoOpen}
      aria-pressed={infoOpen}
      aria-label={$t('chat.room.info')}
      title={$t('chat.room.info')}
      on:click={() => dispatch('info')}
    >
      <Ph svg={infoIcon} size={17} />
    </button>
  </header>

  {#if unverified && room.encrypted}
    <div class="verify" role="status">
      <Ph svg={shieldIcon} size={16} />
      <span>{$t('chat.verify.banner')}</span>
      <button type="button" class="banner-btn" on:click={() => void verify()}>{$t('chat.verify.withDevice')}</button>
      <button type="button" class="banner-btn" on:click={() => openSettingsAt('chat')}>{$t('chat.verify.withKey')}</button>
    </div>
  {/if}

  <Timeline
    {room}
    on:reply={(e) => {
      editing = null
      replyTo = e.detail
      composer?.focus()
    }}
    on:edit={(e) => {
      replyTo = null
      editing = e.detail
      composer?.focus()
    }}
  />

  <div class="typing" aria-live="polite">
    {#if typists.length > 0}
      {typingLine(typists.map((u) => u.name))}
    {/if}
  </div>

  <Composer
    bind:this={composer}
    {room}
    bind:replyTo
    bind:editing
  />
</section>

<style>
  .room {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--surface-base);
  }

  header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    flex: 1;
    min-width: 0;
  }

  h2 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lock {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .sub {
    margin: 0;
    overflow: hidden;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    flex-shrink: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }
  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .icon.on {
    color: var(--accent);
  }

  .verify {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--warning-bg);
    color: var(--warning);
    font-size: var(--fz-meta);
  }
  .verify span {
    flex: 1;
    min-width: 0;
  }

  .banner-btn {
    height: calc(var(--control-height) - var(--space-3));
    padding: 0 var(--space-2);
    border: var(--hairline) solid currentColor;
    border-radius: var(--radius-control);
    background: transparent;
    color: inherit;
    font: inherit;
    cursor: var(--cursor-action);
  }

  .typing {
    min-height: calc(var(--fz-meta) * 1.6);
    padding: 0 var(--pane-pad);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
</style>
