<script lang="ts">
  // posts a mail into a chat room. The user picks a room, may add a note, and
  // sees the exact text before pressing Post; nothing is sent before that. It
  // lives once at the app root and opens when shareToChat holds a message.
  import Modal from '../common/Modal.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { shareToChat, closeShareToChat } from '../../stores/chatshare'
  import { chatRooms, refreshRooms, chatError } from '../../stores/chat'
  import { prefs } from '../../stores/prefs'
  import { toastError, toastSuccess } from '../../stores/toast'
  import { chatSend } from '../../lib/api'
  import { buildShareText, withNote } from '../../lib/chatshare'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'

  let filter = ''
  let note = ''
  let roomId = ''
  let posting = false
  let openedFor = -1

  $: message = $shareToChat

  // each time the dialog opens for a message it starts clean and loads rooms.
  $: if (message && message.id !== openedFor) {
    openedFor = message.id
    filter = ''
    note = ''
    roomId = ''
    posting = false
    void refreshRooms()
  }
  $: if (!message) {
    openedFor = -1
  }

  $: base = message ? buildShareText(message, $prefs.timeFormat as TimeFormat) : ''
  $: text = withNote(note, base)
  $: joined = $chatRooms.filter((r) => r.membership === 'join' && r.kind !== 'space')
  $: needle = filter.trim().toLowerCase()
  $: rooms = needle ? joined.filter((r) => r.name.toLowerCase().includes(needle)) : joined
  $: chosen = joined.find((r) => r.id === roomId)

  async function post(): Promise<void> {
    if (!chosen || posting) {
      return
    }
    posting = true
    try {
      await chatSend({ roomId: chosen.id, text, markdown: true, emote: false, replyTo: '', edit: '', mentions: [] })
      toastSuccess($t('chat.share.done').replace('{room}', chosen.name))
      closeShareToChat()
    } catch (err) {
      toastError(chatError(err))
      posting = false
    }
  }
</script>

{#if message}
  <Modal title={$t('chat.share.title')} hint={$t('chat.share.hint')} size="medium" dirty={note.trim() !== ''} on:close={closeShareToChat}>
    <div class="body">
      <section class="rooms" aria-label={$t('chat.share.rooms')}>
        <input type="search" class="input" bind:value={filter} placeholder={$t('chat.share.filter')} aria-label={$t('chat.share.filter')} />
        <div class="list" role="radiogroup" aria-label={$t('chat.share.rooms')}>
          {#each rooms as room (room.id)}
            <button type="button" class="room" role="radio" aria-checked={room.id === roomId} class:on={room.id === roomId} on:click={() => (roomId = room.id)}>
              <span class="name" dir="auto">{room.name}</span>
            </button>
          {:else}
            <EmptyState size="compact" kind={filter.trim() ? 'filtered' : 'empty'} title={$t('chat.share.noRooms')} />
          {/each}
        </div>
      </section>

      <label class="field">
        <span>{$t('chat.share.note')}</span>
        <input type="text" class="input" bind:value={note} placeholder={$t('chat.share.notePlaceholder')} />
      </label>

      <div class="field">
        <span id="share-preview-label">{$t('chat.share.preview')}</span>
        <pre class="preview" aria-labelledby="share-preview-label" dir="auto">{text}</pre>
      </div>
    </div>

    <svelte:fragment slot="footer">
      <button type="button" class="btn" on:click={closeShareToChat}>{$t('mailboxes.cancel')}</button>
      <button type="button" class="btn primary" disabled={!chosen || posting} on:click={post}>
        {posting ? $t('chat.share.posting') : $t('chat.share.post')}
      </button>
    </svelte:fragment>
  </Modal>
{/if}

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .rooms {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .input {
    min-width: 0;
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }

  .input:focus {
    border-color: var(--accent);
    outline: none;
  }

  .list {
    display: flex;
    flex-direction: column;
    max-height: calc(var(--control-height) * 5);
    overflow-y: auto;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
  }

  .room {
    display: flex;
    align-items: center;
    min-height: var(--control-height);
    padding: 0 var(--space-3);
    border: none;
    border-inline-start: var(--space-1) solid transparent;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .room:hover {
    background: var(--surface-hover);
  }

  .room.on {
    border-inline-start-color: var(--accent);
    background: var(--selection-bg);
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }


  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .field > span {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .preview {
    max-height: calc(var(--control-height) * 7);
    margin: 0;
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-family: var(--font-mono);
    font-size: var(--fz-label);
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .btn {
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .btn:hover {
    background: var(--surface-hover);
  }

  .btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .btn.primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }
</style>
