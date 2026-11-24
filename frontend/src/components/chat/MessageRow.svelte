<script lang="ts">
  // one message of a timeline, Pelton style: no bubbles, an avatar, name and
  // time at the start of a group and just the words after it. Replies show
  // what they answer, files load from the homeserver through the backend,
  // reactions sit under the message and the people who read up to here
  // under that. Hovering or focusing the row shows its actions.
  import { afterUpdate, createEventDispatcher } from 'svelte'
  import replyIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-left-duotone.svg?raw'
  import pencilIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import smileyIcon from '@phosphor-icons/core/assets/duotone/smiley-duotone.svg?raw'
  import copyIcon from '@phosphor-icons/core/assets/duotone/copy-duotone.svg?raw'
  import fileIcon from '@phosphor-icons/core/assets/duotone/file-duotone.svg?raw'
  import downloadIcon from '@phosphor-icons/core/assets/duotone/download-simple-duotone.svg?raw'
  import playIcon from '@phosphor-icons/core/assets/duotone/play-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-circle-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import ChatAvatar from './ChatAvatar.svelte'
  import { chatMedia, chatInlineImage, chatSaveMedia } from '../../lib/api'
  import { prepareHTML, plainToHTML, isEmojiOnly } from '../../lib/chathtml'
  import { stateText } from '../../lib/chatstate'
  import { quickReactions } from '../../lib/emoji'
  import { formatBytes, formatClockTime, formatFullDate, type TimeFormat } from '../../lib/format'
  import { openURL } from '../../lib/browser'
  import { openCustomPreview } from '../../stores/preview'
  import { mediaProgress, chatError } from '../../stores/chat'
  import { prefs } from '../../stores/prefs'
  import { toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import { openScriptMenu } from '../../lib/scriptmenu'
  import type { ChatMessage } from '../../lib/types'

  /** The message shown. */
  export let message: ChatMessage
  /** The room it belongs to. */
  export let roomId: string
  /** The name of the room, handed to scripts. */
  export let roomName: string = ''
  /** The room is a direct chat, handed to scripts. */
  export let direct: boolean = false
  /** True when it continues the group above, so it draws without avatar and name. */
  export let continued: boolean = false
  /** True when the keyboard cursor rests on it. */
  export let selected: boolean = false

  const dispatch = createEventDispatcher<{
    reply: ChatMessage
    edit: ChatMessage
    redact: ChatMessage
    react: { message: ChatMessage; key: string }
    retry: ChatMessage
    jump: string
  }>()

  let body: HTMLElement
  let reactOpen = false
  let thumb = ''
  let thumbError = false
  let player = ''
  let playerLoading = false
  let mediaKey = ''

  $: date = new Date(message.ts)
  $: time = formatClockTime(date, $prefs.timeFormat as TimeFormat)
  $: fullTime = formatFullDate(date.toISOString(), $prefs.timeFormat as TimeFormat)
  $: prepared = message.html ? prepareHTML(message.html) : { html: plainToHTML(message.body), images: [] }
  $: bigEmoji = !message.html && isEmojiOnly(message.body)
  $: textual = message.kind === 'text' || message.kind === 'notice' || message.kind === 'emote'
  $: visual = message.kind === 'image' || message.kind === 'sticker'
  $: progress = $mediaProgress[message.id]
  $: canEdit = message.own && textual
  $: canReact = message.kind !== 'state' && message.kind !== 'redacted'

  // a thumbnail loads once per message.
  $: if (visual && message.id !== mediaKey) {
    mediaKey = message.id
    void loadThumb()
  }

  async function loadThumb(): Promise<void> {
    thumb = ''
    thumbError = false
    try {
      const c = await chatMedia(roomId, message.id, true)
      if (c.data) {
        thumb = `data:${c.contentType || 'image/png'};base64,${c.data}`
      }
    } catch {
      thumbError = true
    }
  }

  // inline images in html are fetched through the backend after each render.
  afterUpdate(() => {
    if (!body) {
      return
    }
    for (const img of Array.from(body.querySelectorAll<HTMLImageElement>('img[data-mxc]'))) {
      if (img.dataset.loaded) {
        continue
      }
      img.dataset.loaded = '1'
      const mxc = img.dataset.mxc ?? ''
      void chatInlineImage(mxc)
        .then((src) => (img.src = src))
        .catch(() => img.remove())
    }
  })

  function onBodyClick(event: MouseEvent): void {
    const el = event.target as HTMLElement
    const link = el.closest<HTMLAnchorElement>('a')
    if (link) {
      event.preventDefault()
      if (link.hasAttribute('data-chat-link')) {
        openURL(link.href)
      }
      return
    }
    const spoiler = el.closest<HTMLElement>('.spoiler')
    if (spoiler) {
      spoiler.classList.toggle('revealed')
    }
  }

  function onBodyKey(event: KeyboardEvent): void {
    const el = event.target as HTMLElement
    if ((event.key === 'Enter' || event.key === ' ') && el.classList.contains('spoiler')) {
      event.preventDefault()
      el.classList.toggle('revealed')
    }
  }

  function attachment(): { id: number; filename: string; contentType: string; sizeBytes: number; inline: boolean } {
    return { id: 0, filename: message.media?.name || 'file', contentType: message.media?.mime ?? '', sizeBytes: message.media?.size ?? 0, inline: false }
  }

  function preview(): void {
    openCustomPreview(`chat:${message.id}`, attachment(), () => chatMedia(roomId, message.id, false), save)
  }

  async function save(): Promise<boolean> {
    try {
      const path = await chatSaveMedia(roomId, message.id)
      if (path) {
        toastSuccess($t('chat.media.saved').replace('{name}', message.media?.name ?? ''))
        return true
      }
    } catch (err) {
      toastError(chatError(err))
    }
    return false
  }

  async function play(): Promise<void> {
    playerLoading = true
    try {
      const c = await chatMedia(roomId, message.id, false)
      if (c.tooLarge) {
        toastError($t('chat.media.tooLarge'))
        return
      }
      const bin = atob(c.data)
      const bytes = new Uint8Array(bin.length)
      for (let i = 0; i < bin.length; i++) {
        bytes[i] = bin.charCodeAt(i)
      }
      player = URL.createObjectURL(new Blob([bytes], { type: c.contentType || message.media?.mime || '' }))
    } catch (err) {
      toastError(chatError(err))
    } finally {
      playerLoading = false
    }
  }

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(message.body)
      toastSuccess($t('chat.message.copied'))
    } catch {
      // clipboard access refused; nothing to undo
    }
  }

  function react(key: string): void {
    reactOpen = false
    dispatch('react', { message, key })
  }

  // the size an image shows at: its own, scaled down to fit the token box.
  $: box = mediaBox(message.media?.width ?? 0, message.media?.height ?? 0)
  function mediaBox(w: number, h: number): string {
    if (!w || !h) {
      return ''
    }
    return `aspect-ratio:${w}/${h};width:min(${w}px, var(--chat-media-width), calc(var(--chat-media-height) * ${w / h}))`
  }

  $: readers = message.readBy.slice(0, 3)
  $: moreReaders = message.readBy.length - readers.length

  function onScriptMenu(event: MouseEvent): void {
    openScriptMenu(event, 'chat.message', {
      id: message.id,
      fields: {
        room_id: roomId,
        room: roomName,
        id: message.id,
        sender: message.sender,
        body: message.body,
        direct,
        mention: message.mentioned,
      },
    })
  }
</script>

{#if message.kind === 'state'}
  <div class="state" data-message-id={message.id} title={fullTime}>
    {stateText(message, $t)}
  </div>
{:else}
  <div
    class="message"
    class:continued
    class:selected
    class:mentioned={message.mentioned}
    data-message-id={message.id}
    role="article"
    aria-label={`${message.senderName}, ${fullTime}`}
    on:contextmenu={onScriptMenu}
  >
    <div class="gutter">
      {#if continued}
        <span class="hover-time" title={fullTime}>{time}</span>
      {:else}
        <ChatAvatar mxc={message.senderAvatar} name={message.senderName} seed={message.sender} />
      {/if}
    </div>
    <div class="main">
      {#if !continued}
        <div class="meta">
          <span class="sender" title={message.sender}>{message.senderName}</span>
          <time datetime={date.toISOString()} title={fullTime}>{time}</time>
        </div>
      {/if}

      {#if message.replyTo}
        <button type="button" class="reply-quote" on:click={() => message.replyTo && dispatch('jump', message.replyTo.id)}>
          {#if message.replyTo.missing}
            <span class="quote-body muted">{$t('chat.message.replyMissing')}</span>
          {:else}
            <span class="quote-name">{message.replyTo.senderName}</span>
            <span class="quote-body">{message.replyTo.body || $t('chat.preview.redacted')}</span>
          {/if}
        </button>
      {/if}

      {#if textual}
        <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
        <div
          class="body"
          class:notice={message.kind === 'notice'}
          class:emoji={bigEmoji}
          class:rich={!!message.html}
          bind:this={body}
          on:click={onBodyClick}
          on:keydown={onBodyKey}
        >
          {#if message.kind === 'emote'}<span class="emote">* {message.senderName}</span>{' '}{/if}
          <!-- eslint-disable-next-line svelte/no-at-html-tags -- sanitised by the backend, shaped by prepareHTML -->
          {@html prepared.html}
          {#if message.edited}<span class="edited">{$t('chat.message.edited')}</span>{/if}
        </div>
      {:else if visual}
        <button type="button" class="image" style={box} on:click={preview} aria-label={$t('chat.media.open').replace('{name}', message.media?.name ?? '')}>
          {#if thumb}
            <img src={thumb} alt={message.media?.name ?? ''} />
          {:else if thumbError}
            <span class="image-note"><Ph svg={warningIcon} size={16} />{$t('chat.media.failed')}</span>
          {:else}
            <PieProgress label={$t('chat.media.loading')} />
          {/if}
        </button>
        {#if message.body}
          <div class="body">{message.body}</div>
        {/if}
      {:else if message.kind === 'video' || message.kind === 'audio' || message.kind === 'file'}
        <div class="file">
          <Ph svg={fileIcon} size={20} />
          <div class="file-text">
            <span class="file-name" title={message.media?.name}>{message.media?.name}</span>
            <span class="file-size">{message.media?.size ? formatBytes(message.media.size) : ''}</span>
          </div>
          {#if message.kind !== 'file' && !player}
            <button type="button" class="file-btn" aria-label={$t('chat.media.play')} title={$t('chat.media.play')} on:click={() => void play()} disabled={playerLoading}>
              {#if playerLoading}<PieProgress label={$t('chat.media.loading')} />{:else}<Ph svg={playIcon} size={16} />{/if}
            </button>
          {/if}
          {#if message.kind === 'file'}
            <button type="button" class="file-btn" aria-label={$t('chat.media.preview')} title={$t('chat.media.preview')} on:click={preview}>
              <Ph svg={fileIcon} size={16} />
            </button>
          {/if}
          <button type="button" class="file-btn" aria-label={$t('chat.media.save')} title={$t('chat.media.save')} on:click={() => void save()} disabled={!!progress}>
            {#if progress}
              <PieProgress value={progress.bytesTotal ? progress.bytesDone / progress.bytesTotal : null} label={$t('chat.media.saving')} />
            {:else}
              <Ph svg={downloadIcon} size={16} />
            {/if}
          </button>
        </div>
        {#if player && message.kind === 'video'}
          <!-- svelte-ignore a11y-media-has-caption -->
          <video class="player" src={player} controls></video>
        {:else if player}
          <audio class="player" src={player} controls></audio>
        {/if}
        {#if message.body}
          <div class="body">{message.body}</div>
        {/if}
      {:else if message.kind === 'location'}
        <div class="body">{message.body}</div>
      {:else if message.kind === 'redacted'}
        <div class="body muted">{$t('chat.message.redacted')}</div>
      {:else if message.kind === 'encrypted'}
        <div class="body muted undecryptable">
          <Ph svg={warningIcon} size={15} />
          <span>{message.missingKeys ? $t('chat.message.missingKeys') : $t('chat.message.undecryptable')}</span>
          {#if message.missingKeys}
            <button type="button" class="inline-btn" on:click={() => dispatch('retry', message)}>{$t('chat.message.retry')}</button>
          {/if}
        </div>
      {:else}
        <div class="body muted">{$t('chat.message.unsupported')}</div>
      {/if}

      {#if message.reactions.length > 0}
        <div class="reactions">
          {#each message.reactions as r (r.key)}
            <button
              type="button"
              class="reaction"
              class:mine={!!r.mine}
              aria-pressed={!!r.mine}
              title={r.senders.join(', ')}
              on:click={() => react(r.key)}
            >
              <span class="key">{r.key}</span>
              <span class="n">{r.count}</span>
            </button>
          {/each}
        </div>
      {/if}

      {#if message.readBy.length > 0}
        <div class="read" title={$t('chat.message.readBy').replace('{names}', message.readBy.map((m) => m.name).join(', '))}>
          {#each readers as r (r.userId)}
            <ChatAvatar mxc={r.avatar} name={r.name} seed={r.userId} size="var(--fz-meta)" />
          {/each}
          {#if moreReaders > 0}<span class="more">+{moreReaders}</span>{/if}
        </div>
      {/if}
    </div>

    <div class="actions" class:open={reactOpen} role="toolbar" aria-label={$t('chat.message.actions')}>
      {#if canReact}
        <div class="react-wrap">
          <button type="button" class="act" aria-label={$t('chat.message.react')} title={$t('chat.message.react')} aria-expanded={reactOpen} on:click={() => (reactOpen = !reactOpen)}>
            <Ph svg={smileyIcon} size={15} />
          </button>
          {#if reactOpen}
            <div class="quick" role="menu">
              {#each quickReactions as key (key)}
                <button type="button" role="menuitem" class="quick-key" on:click={() => react(key)}>{key}</button>
              {/each}
            </div>
          {/if}
        </div>
        <button type="button" class="act" aria-label={$t('chat.message.reply')} title={$t('chat.message.reply')} on:click={() => dispatch('reply', message)}>
          <Ph svg={replyIcon} size={15} />
        </button>
      {/if}
      {#if canEdit}
        <button type="button" class="act" aria-label={$t('chat.message.edit')} title={$t('chat.message.edit')} on:click={() => dispatch('edit', message)}>
          <Ph svg={pencilIcon} size={15} />
        </button>
      {/if}
      {#if textual}
        <button type="button" class="act" aria-label={$t('chat.message.copy')} title={$t('chat.message.copy')} on:click={() => void copy()}>
          <Ph svg={copyIcon} size={15} />
        </button>
      {/if}
      {#if message.own && message.kind !== 'redacted'}
        <button type="button" class="act danger" aria-label={$t('chat.message.delete')} title={$t('chat.message.delete')} on:click={() => dispatch('redact', message)}>
          <Ph svg={trashIcon} size={15} />
        </button>
      {/if}
    </div>
  </div>
{/if}

<style>
  .state {
    padding: var(--space-1) var(--pane-pad) var(--space-1) calc(var(--pane-pad) + var(--chat-avatar) + var(--space-3));
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .message {
    position: relative;
    display: grid;
    grid-template-columns: var(--chat-avatar) minmax(0, 1fr);
    column-gap: var(--space-3);
    padding: var(--space-2) var(--pane-pad) var(--space-1);
  }
  .message.continued {
    padding-top: 0;
  }
  .message:hover,
  .message:focus-within,
  .message.selected {
    background: var(--surface-hover);
  }
  .message.mentioned {
    box-shadow: inset var(--space-1) 0 0 var(--accent);
  }

  .gutter {
    display: flex;
    justify-content: center;
  }
  .hover-time {
    visibility: hidden;
    padding-top: var(--space-1);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    white-space: nowrap;
  }
  .message:hover .hover-time,
  .message.selected .hover-time {
    visibility: visible;
  }

  .main {
    min-width: 0;
  }

  .meta {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
  }
  .sender {
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
  }
  time {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .body {
    color: var(--text-primary);
    font-size: var(--fz-body);
    line-height: var(--list-line-height);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .body.rich {
    white-space: normal;
  }
  .body.notice,
  .body.muted {
    color: var(--text-secondary);
  }
  .body.muted {
    font-style: italic;
  }
  .body.emoji {
    font-size: calc(var(--fz-body) * 2.2);
    line-height: 1.2;
  }
  .emote {
    font-weight: var(--fw-semibold);
  }
  .edited {
    margin-left: var(--space-2);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .body :global(a) {
    color: var(--link);
  }
  .body :global(.pill) {
    padding: 0 var(--space-1);
    border-radius: var(--radius-control);
    background: var(--selection-bg);
    color: var(--text-primary);
    text-decoration: none;
  }
  .body :global(blockquote) {
    margin: var(--space-1) 0;
    padding-left: var(--space-3);
    border-left: var(--space-1) solid var(--border-default);
    color: var(--text-secondary);
  }
  .body :global(pre),
  .body :global(code) {
    font-family: var(--font-mono);
    font-size: 0.92em;
  }
  .body :global(pre) {
    margin: var(--space-1) 0;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    overflow-x: auto;
    white-space: pre;
  }
  .body :global(:not(pre) > code) {
    padding: 0 var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }
  .body :global(p) {
    margin: 0 0 var(--space-2);
  }
  .body :global(p:last-child) {
    margin-bottom: 0;
  }
  .body :global(ul),
  .body :global(ol) {
    margin: var(--space-1) 0;
    padding-left: var(--space-5);
  }
  .body :global(img.inline-image) {
    display: block;
    max-width: var(--chat-media-width);
    max-height: var(--chat-media-height);
    border-radius: var(--radius-control);
  }
  .body :global(img.emoticon) {
    height: 1.4em;
    vertical-align: middle;
  }
  .body :global(.spoiler) {
    border-radius: var(--radius-control);
    background: var(--text-primary);
    color: transparent;
    cursor: var(--cursor-action);
  }
  .body :global(.spoiler.revealed) {
    background: var(--surface-sunken);
    color: inherit;
  }
  .body :global(table) {
    border-collapse: collapse;
  }
  .body :global(td),
  .body :global(th) {
    padding: var(--space-1) var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
  }

  .reply-quote {
    display: flex;
    flex-direction: column;
    max-width: 100%;
    margin: var(--space-1) 0;
    padding: var(--space-1) var(--space-3);
    border: none;
    border-left: var(--space-1) solid var(--border-strong);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    text-align: left;
    cursor: var(--cursor-action);
  }
  .quote-name {
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
  }
  .quote-body {
    overflow: hidden;
    font-size: var(--fz-meta);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .quote-body.muted {
    font-style: italic;
  }

  .image {
    display: flex;
    align-items: center;
    justify-content: center;
    max-width: 100%;
    min-width: calc(var(--chat-avatar) * 2);
    min-height: calc(var(--chat-avatar) * 2);
    margin: var(--space-1) 0;
    padding: 0;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
    overflow: hidden;
    cursor: zoom-in;
  }
  .image img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .image-note {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .file {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    max-width: var(--chat-media-width);
    margin: var(--space-1) 0;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-secondary);
  }
  .file-text {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }
  .file-name {
    overflow: hidden;
    color: var(--text-primary);
    font-size: var(--fz-body);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-size {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
  .file-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }
  .file-btn:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .player {
    display: block;
    max-width: var(--chat-media-width);
    max-height: var(--chat-media-height);
    margin: var(--space-1) 0;
  }

  .undecryptable {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .inline-btn {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    font-style: normal;
    cursor: var(--cursor-action);
  }

  .reactions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
    margin-top: var(--space-1);
  }
  .reaction {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }
  .reaction:hover {
    border-color: var(--border-strong);
  }
  .reaction.mine {
    border-color: var(--accent);
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .read {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: var(--space-1);
    margin-top: var(--space-1);
  }
  .more {
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .actions {
    position: absolute;
    top: calc(-1 * var(--space-3));
    right: var(--pane-pad);
    display: none;
    gap: var(--space-1);
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    z-index: 2;
  }
  .message:hover .actions,
  .message:focus-within .actions,
  .message.selected .actions,
  .actions.open {
    display: flex;
  }
  .act {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) - var(--space-2));
    height: calc(var(--control-height) - var(--space-2));
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }
  .act:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .act.danger:hover {
    color: var(--danger);
  }

  .react-wrap {
    position: relative;
  }
  .quick {
    position: absolute;
    top: 100%;
    right: 0;
    display: flex;
    gap: var(--space-1);
    margin-top: var(--space-1);
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }
  .quick-key {
    width: var(--control-height);
    height: var(--control-height);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    font-size: var(--fz-heading);
    cursor: var(--cursor-action);
  }
  .quick-key:hover {
    background: var(--surface-hover);
  }

  @media (prefers-reduced-motion: no-preference) {
    .message {
      transition: background var(--motion-fast) var(--ease-out);
    }
  }
</style>
