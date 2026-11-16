<script lang="ts">
  // the message box of a room. Enter sends, Shift+Enter starts a new line, and
  // Up in an empty box edits the user's last message. Typing @ suggests the
  // room's members and :code suggests emoji; complete :codes turn into emoji
  // on sending. Files come from the attach button, a paste or a drop on the
  // room, wait here as chips and go out with the next send.
  import { onDestroy, tick } from 'svelte'
  import paperclipIcon from '@phosphor-icons/core/assets/duotone/paperclip-duotone.svg?raw'
  import sendIcon from '@phosphor-icons/core/assets/duotone/paper-plane-right-duotone.svg?raw'
  import replyIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-left-duotone.svg?raw'
  import pencilIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import ChatAvatar from './ChatAvatar.svelte'
  import { sendMessage, sendFilePaths, sendFileBlob, timelines, chatStatus, chatError } from '../../stores/chat'
  import { chatMembers, chatTyping, chatPickFiles, chatUploadLimit } from '../../lib/api'
  import { replaceShortcodes, shortcodeQuery, searchShortcodes } from '../../lib/emoji'
  import { mentionQuery, matchMembers, applyMention, mentionsIn } from '../../lib/chatmentions'
  import { formatBytes } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { ChatMember, ChatMessage, ChatRoom } from '../../lib/types'

  /** The room messages go to. */
  export let room: ChatRoom
  /** The message being answered, or none. */
  export let replyTo: ChatMessage | null = null
  /** The user's own message being edited, or none. */
  export let editing: ChatMessage | null = null

  type Staged = { id: number; name: string; size: number; path?: string; file?: File }

  let text = ''
  let box: HTMLTextAreaElement
  let staged: Staged[] = []
  let stagedSeq = 0
  let chosen: { userId: string; name: string }[] = []
  let members: ChatMember[] | null = null
  let membersRoom = ''
  let suggestions: { kind: 'member'; member: ChatMember }[] | { kind: 'emoji'; code: string; emoji: string }[] = []
  let suggestIndex = 0
  let typingSent = false
  let typingTimer: ReturnType<typeof setTimeout> | null = null
  let limit = 0
  let editingId = ''
  let drafts: Record<string, string> = {}
  let lastRoom = room.id

  // each room keeps its own unsent draft.
  $: if (room.id !== lastRoom) {
    drafts[lastRoom] = editing ? '' : text
    lastRoom = room.id
    text = drafts[room.id] ?? ''
    staged = []
    chosen = []
    members = null
    suggestions = []
    void tick().then(resize)
  }

  // starting an edit puts the message in the box; ending it empties the box.
  $: if ((editing?.id ?? '') !== editingId) {
    editingId = editing?.id ?? ''
    text = editing ? editing.body : ''
    void tick().then(() => {
      resize()
      box?.setSelectionRange(text.length, text.length)
    })
  }

  onDestroy(() => {
    if (typingTimer) {
      clearTimeout(typingTimer)
    }
    if (typingSent) {
      void chatTyping(room.id, false).catch(() => {})
    }
  })

  /** Puts the cursor in the message box. */
  export function focus(): void {
    box?.focus()
  }

  /** Adds files from disk to send with the next message. */
  export function stagePaths(paths: string[]): void {
    staged = [...staged, ...paths.map((p) => ({ id: ++stagedSeq, name: p.split(/[\\/]/).pop() || p, size: 0, path: p }))]
    focus()
  }

  async function stageFiles(files: File[]): Promise<void> {
    if (!limit) {
      limit = await chatUploadLimit().catch(() => 0)
    }
    for (const f of files) {
      if (limit && f.size > limit) {
        toastError($t('chat.composer.tooLarge').replace('{name}', f.name || 'file').replace('{limit}', formatBytes(limit)))
        continue
      }
      staged = [...staged, { id: ++stagedSeq, name: f.name || $t('chat.composer.pastedImage'), size: f.size, file: f }]
    }
  }

  function resize(): void {
    if (!box) {
      return
    }
    box.style.height = 'auto'
    box.style.height = `${box.scrollHeight}px`
  }

  async function loadMembers(): Promise<ChatMember[]> {
    if (!members || membersRoom !== room.id) {
      membersRoom = room.id
      members = await chatMembers(room.id).catch(() => [])
    }
    return members
  }

  async function onInput(): Promise<void> {
    resize()
    signalTyping()
    const caret = box.selectionStart ?? text.length
    const mq = mentionQuery(text, caret)
    if (mq) {
      const list = matchMembers(await loadMembers(), mq.query, $chatStatus.userId)
      suggestions = list.map((member) => ({ kind: 'member' as const, member }))
      suggestIndex = 0
      return
    }
    const sq = shortcodeQuery(text, caret)
    if (sq) {
      suggestions = searchShortcodes(sq.query).map((s) => ({ kind: 'emoji' as const, ...s }))
      suggestIndex = 0
      return
    }
    suggestions = []
  }

  function signalTyping(): void {
    if (!$prefs.chatSendTyping) {
      return
    }
    if (!typingSent && text.trim()) {
      typingSent = true
      void chatTyping(room.id, true).catch(() => {})
    }
    if (typingTimer) {
      clearTimeout(typingTimer)
    }
    typingTimer = setTimeout(stopTyping, 4000)
  }

  function stopTyping(): void {
    if (typingTimer) {
      clearTimeout(typingTimer)
      typingTimer = null
    }
    if (typingSent) {
      typingSent = false
      void chatTyping(room.id, false).catch(() => {})
    }
  }

  async function pick(i: number): Promise<void> {
    const s = suggestions[i]
    if (!s) {
      return
    }
    const caret = box.selectionStart ?? text.length
    if (s.kind === 'member') {
      const mq = mentionQuery(text, caret)
      if (!mq) {
        return
      }
      const next = applyMention(text, mq, caret, s.member)
      text = next.text
      chosen = [...chosen, { userId: s.member.userId, name: s.member.name }]
      suggestions = []
      await tick()
      box.setSelectionRange(next.caret, next.caret)
    } else {
      const sq = shortcodeQuery(text, caret)
      if (!sq) {
        return
      }
      text = text.slice(0, sq.start) + s.emoji + ' ' + text.slice(caret)
      const at = sq.start + s.emoji.length + 1
      suggestions = []
      await tick()
      box.setSelectionRange(at, at)
    }
    box.focus()
  }

  function lastOwnText(): ChatMessage | undefined {
    const list = $timelines[room.id]?.messages ?? []
    for (let i = list.length - 1; i >= 0; i--) {
      const m = list[i]
      if (m.own && (m.kind === 'text' || m.kind === 'emote' || m.kind === 'notice')) {
        return m
      }
    }
    return undefined
  }

  function onKeydown(event: KeyboardEvent): void {
    if (suggestions.length > 0) {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault()
        const n = suggestions.length
        suggestIndex = (suggestIndex + (event.key === 'ArrowDown' ? 1 : n - 1)) % n
        return
      }
      if (event.key === 'Enter' || event.key === 'Tab') {
        event.preventDefault()
        void pick(suggestIndex)
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        suggestions = []
        return
      }
    }
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault()
      void send()
      return
    }
    if (event.key === 'Escape' && (replyTo || editing)) {
      event.preventDefault()
      event.stopPropagation()
      replyTo = null
      editing = null
      return
    }
    if (event.key === 'ArrowUp' && !text && !editing) {
      const last = lastOwnText()
      if (last) {
        event.preventDefault()
        replyTo = null
        editing = last
      }
    }
  }

  function onPaste(event: ClipboardEvent): void {
    const files = Array.from(event.clipboardData?.files ?? [])
    if (files.length > 0) {
      event.preventDefault()
      void stageFiles(files)
    }
  }

  async function attach(): Promise<void> {
    try {
      const paths = await chatPickFiles()
      if (paths.length) {
        stagePaths(paths)
      }
    } catch (err) {
      toastError(chatError(err))
    }
  }

  async function send(): Promise<void> {
    const body = replaceShortcodes(text).trim()
    const files = staged
    if (!body && files.length === 0) {
      return
    }
    const roomId = room.id
    const reply = replyTo?.id ?? ''
    const edit = editing?.id ?? ''
    const emote = body.startsWith('/me ')
    const finalText = emote ? body.slice(4) : body
    text = ''
    staged = []
    replyTo = null
    editing = null
    chosen = mentionsIn(finalText, chosen)
    const mentions = chosen
    chosen = []
    suggestions = []
    stopTyping()
    drafts[roomId] = ''
    await tick()
    resize()
    if (finalText) {
      try {
        await sendMessage({ roomId, text: finalText, markdown: $prefs.chatMarkdown, emote, replyTo: reply, edit, mentions })
      } catch (err) {
        toastError(chatError(err))
      }
    }
    const paths = files.filter((f) => f.path).map((f) => f.path as string)
    if (paths.length) {
      void sendFilePaths(roomId, paths, finalText ? '' : reply)
    }
    for (const f of files) {
      if (f.file) {
        void sendFileBlob(roomId, f.file, finalText ? '' : reply)
      }
    }
  }
</script>

<div class="composer">
  {#if replyTo || editing}
    <div class="context">
      <Ph svg={replyTo ? replyIcon : pencilIcon} size={14} />
      <span class="context-text">
        {#if replyTo}
          {$t('chat.composer.replyingTo').replace('{name}', replyTo.senderName)}
          <span class="context-body">{replyTo.body}</span>
        {:else}
          {$t('chat.composer.editing')}
        {/if}
      </span>
      <button
        type="button"
        class="icon small"
        aria-label={$t('chat.composer.cancel')}
        title={$t('chat.composer.cancel')}
        on:click={() => {
          replyTo = null
          editing = null
        }}
      >
        <Ph svg={xIcon} size={12} />
      </button>
    </div>
  {/if}

  {#if staged.length > 0}
    <ul class="staged" aria-label={$t('chat.composer.files')}>
      {#each staged as f (f.id)}
        <li class="chip">
          <span class="chip-name" title={f.name}>{f.name}</span>
          {#if f.size}<span class="chip-size">{formatBytes(f.size)}</span>{/if}
          <button
            type="button"
            class="icon small"
            aria-label={$t('chat.composer.removeFile').replace('{name}', f.name)}
            on:click={() => (staged = staged.filter((x) => x.id !== f.id))}
          >
            <Ph svg={xIcon} size={11} />
          </button>
        </li>
      {/each}
    </ul>
  {/if}

  {#if suggestions.length > 0}
    <ul class="suggest" role="listbox" aria-label={$t('chat.composer.suggestions')}>
      {#each suggestions as s, i}
        <li role="option" aria-selected={i === suggestIndex}>
          <button type="button" class="suggest-row" class:active={i === suggestIndex} on:mousedown|preventDefault={() => void pick(i)}>
            {#if s.kind === 'member'}
              <ChatAvatar mxc={s.member.avatar} name={s.member.name} seed={s.member.userId} size="var(--chip-height)" />
              <span class="suggest-name">{s.member.name}</span>
              <span class="suggest-id">{s.member.userId}</span>
            {:else}
              <span class="suggest-emoji">{s.emoji}</span>
              <span class="suggest-name">:{s.code}:</span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  {/if}

  <div class="row">
    <button type="button" class="icon" aria-label={$t('chat.composer.attach')} title={$t('chat.composer.attach')} on:click={() => void attach()}>
      <Ph svg={paperclipIcon} size={17} />
    </button>
    <textarea
      bind:this={box}
      bind:value={text}
      data-chat-composer
      rows="1"
      placeholder={room.encrypted ? $t('chat.composer.placeholderEncrypted') : $t('chat.composer.placeholder')}
      aria-label={$t('chat.composer.label').replace('{room}', room.name)}
      on:input={() => void onInput()}
      on:keydown={onKeydown}
      on:paste={onPaste}
      on:blur={() => (suggestions = [])}
    ></textarea>
    <button
      type="button"
      class="send"
      aria-label={editing ? $t('chat.composer.save') : $t('chat.composer.send')}
      title={editing ? $t('chat.composer.save') : $t('chat.composer.send')}
      disabled={!text.trim() && staged.length === 0}
      on:click={() => void send()}
    >
      <Ph svg={sendIcon} size={17} />
    </button>
  </div>
  <p class="hint">{$prefs.chatMarkdown ? $t('chat.composer.hintMarkdown') : $t('chat.composer.hint')}</p>
</div>

<style>
  .composer {
    position: relative;
    padding: 0 var(--pane-pad) var(--space-2);
  }

  .context {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-bottom: none;
    border-radius: var(--radius-control) var(--radius-control) 0 0;
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }
  .context-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .context-body {
    margin-left: var(--space-2);
    color: var(--text-tertiary);
  }

  .staged {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
    margin: 0 0 var(--space-1);
    padding: 0;
    list-style: none;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    max-width: 100%;
    height: var(--chip-height);
    padding: 0 var(--space-1) 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-meta);
  }
  .chip-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chip-size {
    color: var(--text-tertiary);
  }

  .suggest {
    position: absolute;
    left: var(--pane-pad);
    right: var(--pane-pad);
    bottom: 100%;
    margin: 0;
    padding: var(--space-1);
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    z-index: 3;
  }
  .suggest-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    text-align: left;
    cursor: var(--cursor-action);
  }
  .suggest-row.active {
    background: var(--selection-bg);
  }
  .suggest-id {
    margin-left: auto;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
  .suggest-emoji {
    width: var(--chip-height);
    text-align: center;
  }

  .row {
    display: flex;
    align-items: flex-end;
    gap: var(--space-2);
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }
  .row:focus-within {
    border-color: var(--accent);
  }

  textarea {
    flex: 1;
    min-width: 0;
    max-height: var(--chat-composer-max);
    padding: var(--space-2) 0;
    border: none;
    outline: none;
    resize: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    line-height: var(--list-line-height);
  }

  .icon,
  .send {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--control-height);
    height: var(--control-height);
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
  .icon.small {
    width: var(--chip-height);
    height: var(--chip-height);
  }
  .send {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .send:disabled {
    background: transparent;
    color: var(--text-tertiary);
    cursor: default;
  }

  .hint {
    margin: var(--space-1) 0 0;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }
</style>
