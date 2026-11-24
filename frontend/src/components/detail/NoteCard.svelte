<script lang="ts">
  // the private note at the top of the reader: a quiet card holding a note on
  // the message, or on its conversation. It edits in place and saves as you
  // type; emptying it or pressing delete removes the note. Notes stay on this
  // device and are never sent.
  import { onDestroy, tick } from 'svelte'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { deleteNote, getNote, saveNote } from '../../lib/api'
  import { markNote, noteRequest } from '../../lib/organize'
  import { errorMessage, toastError } from '../../stores/toast'

  /** The message the note belongs to, or whose conversation it belongs to. */
  export let messageId: number
  /** The conversation the message is in, 0 when the note is the message's own. */
  export let threadId: number = 0

  $: conversation = threadId !== 0

  // how long typing has to pause before the note is saved.
  const saveDelay = 600

  let body = ''
  let saved = ''
  let editing = false
  let status: '' | 'saving' | 'saved' = ''
  let timer: ReturnType<typeof setTimeout> | undefined
  let field: HTMLTextAreaElement | undefined
  let loadedKey = ''

  $: void load(messageId, conversation)

  async function load(id: number, onConversation: boolean): Promise<void> {
    const key = `${id}:${onConversation}`
    if (key === loadedKey) {
      return
    }
    await flush()
    loadedKey = key
    body = ''
    saved = ''
    editing = false
    status = ''
    try {
      const note = await getNote(id, onConversation)
      if (loadedKey === key) {
        body = note.body
        saved = note.body
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // a request to edit the note of this message opens the card.
  $: if ($noteRequest !== null && $noteRequest === messageId) {
    noteRequest.set(null)
    void startEditing()
  }

  async function startEditing(): Promise<void> {
    editing = true
    await tick()
    field?.focus()
  }

  function onInput(): void {
    status = 'saving'
    clearTimeout(timer)
    timer = setTimeout(() => void persist(), saveDelay)
  }

  // persist writes the note when it changed. A blank note is deleted.
  async function persist(): Promise<void> {
    clearTimeout(timer)
    timer = undefined
    if (body === saved) {
      status = saved === '' ? '' : status
      return
    }
    const id = messageId
    const onConversation = conversation
    const text = body
    try {
      const note = await saveNote(id, onConversation, text)
      saved = note.body
      status = note.body === '' ? '' : 'saved'
      markNote(id, threadId, note.body !== '')
    } catch (err) {
      status = ''
      toastError(errorMessage(err))
    }
  }

  async function flush(): Promise<void> {
    if (timer !== undefined) {
      await persist()
    }
  }

  function onBlur(): void {
    void persist().then(() => {
      if (body.trim() === '') {
        editing = false
      }
    })
  }

  async function remove(): Promise<void> {
    clearTimeout(timer)
    timer = undefined
    try {
      await deleteNote(messageId, conversation)
      body = ''
      saved = ''
      status = ''
      editing = false
      markNote(messageId, threadId, false)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      field?.blur()
    }
  }

  onDestroy(() => {
    if (timer !== undefined) {
      void persist()
    }
  })
</script>

{#if editing || saved !== ''}
  <section class="note" aria-label={conversation ? $t('note.titleConversation') : $t('note.title')}>
    <header class="head">
      <Ph svg={noteIcon} size={14} />
      <span class="title">{conversation ? $t('note.titleConversation') : $t('note.title')}</span>
      <span class="status" aria-live="polite">
        {#if status === 'saving'}{$t('note.saving')}{:else if status === 'saved'}{$t('note.saved')}{/if}
      </span>
      <button type="button" class="tool" aria-label={$t('note.delete')} title={$t('note.delete')} on:click={() => void remove()}>
        <Ph svg={trashIcon} size={14} />
      </button>
    </header>
    <textarea
      bind:this={field}
      bind:value={body}
      rows="2"
      placeholder={$t('note.placeholder')}
      aria-label={conversation ? $t('note.titleConversation') : $t('note.title')}
      on:input={onInput}
      on:focus={() => (editing = true)}
      on:blur={onBlur}
      on:keydown={onKey}
    ></textarea>
    <p class="private">{$t('note.private')}</p>
  </section>
{/if}

<style>
  .note {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-tertiary);
  }

  .title {
    flex: 1;
    min-width: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-secondary);
  }

  .status {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .tool:hover {
    background: var(--danger-bg);
    color: var(--danger);
  }

  textarea {
    width: 100%;
    min-height: calc(var(--control-height) * 2);
    padding: var(--space-2);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    line-height: 1.5;
    resize: vertical;
    field-sizing: content;
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  textarea:hover {
    border-color: var(--border-subtle);
  }

  textarea:focus {
    outline: none;
    border-color: var(--accent);
    background: var(--surface-raised);
  }

  .private {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
