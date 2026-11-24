<script lang="ts">
  // the Snippets page: reusable text for the composer. the list is in the
  // order the picker shows it, dragged by its grip or moved with the arrow
  // buttons; a snippet opens below the list to edit, with its optional
  // subject, recipients and trigger word, and buttons that put a placeholder
  // into the text.
  import { onMount, tick } from 'svelte'
  import { slide } from 'svelte/transition'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import editIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import upIcon from '@phosphor-icons/core/assets/duotone/arrow-up-duotone.svg?raw'
  import downIcon from '@phosphor-icons/core/assets/duotone/arrow-down-duotone.svg?raw'
  import gripIcon from '@phosphor-icons/core/assets/duotone/dots-six-vertical-duotone.svg?raw'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Ph from '../../common/Ph.svelte'
  import SnippetPreview from '../skeletons/SnippetPreview.svelte'
  import { snippets, loadSnippets, storeSnippet, removeSnippet, moveSnippets } from '../../../stores/snippets'
  import { builtinPlaceholders, triggerPrefix } from '../../../lib/snippets'
  import { reorder, type ReorderDetail } from '../../../lib/reorder'
  import { askConfirm } from '../../../stores/confirm'
  import { errorMessage, toastError } from '../../../stores/toast'
  import { dur, motion } from '../../../lib/motion'
  import { t } from '../../../lib/i18n'
  import type { Snippet } from '../../../lib/types'

  const blank: Snippet = { id: 0, name: '', body: '', subject: '', to: '', cc: '', trigger: '', position: 0 }

  // the snippet open in the editor, a copy so cancel leaves the list alone.
  let editing: Snippet | null = null
  let saving = false
  let bodyField: HTMLTextAreaElement
  let nameField: HTMLInputElement

  onMount(() => void loadSnippets())

  async function edit(snippet: Snippet): Promise<void> {
    editing = { ...snippet }
    await tick()
    nameField?.focus()
  }

  async function save(): Promise<void> {
    if (!editing || saving) {
      return
    }
    saving = true
    try {
      await storeSnippet(editing)
      editing = null
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      saving = false
    }
  }

  async function remove(snippet: Snippet): Promise<void> {
    const ok = await askConfirm({
      title: $t('snippets.deleteTitle').replace('{name}', snippet.name),
      body: $t('snippets.deleteBody'),
      confirmLabel: $t('action.delete'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await removeSnippet(snippet.id)
      if (editing?.id === snippet.id) {
        editing = null
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function move(index: number, delta: number): Promise<void> {
    const ids = $snippets.map((s) => s.id)
    const to = index + delta
    if (to < 0 || to >= ids.length) {
      return
    }
    ;[ids[index], ids[to]] = [ids[to], ids[index]]
    await reorderTo(ids)
  }

  async function reorderTo(ids: number[]): Promise<void> {
    try {
      await moveSnippets(ids)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function onReorder(event: CustomEvent<ReorderDetail>): void {
    void reorderTo(event.detail.ids.map(Number))
  }

  // putPlaceholder writes a placeholder into the text at the caret.
  async function putPlaceholder(name: string): Promise<void> {
    if (!editing) {
      return
    }
    const token = `{${name}}`
    const start = bodyField?.selectionStart ?? editing.body.length
    const end = bodyField?.selectionEnd ?? start
    editing.body = editing.body.slice(0, start) + token + editing.body.slice(end)
    await tick()
    bodyField?.focus()
    bodyField?.setSelectionRange(start + token.length, start + token.length)
  }

  function onEditorKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      editing = null
    } else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault()
      void save()
    }
  }

  $: placeholderHint = $t('snippets.placeholders.hint')
</script>

<Card title={$t('snippets.title')} description={$t('snippets.description')}>
  <Row id="snippets" label={$t('snippets.list')} hint={$t('snippets.list.hint')} stacked>
    <SnippetPreview slot="preview" />
    {#if $snippets.length === 0}
      <p class="empty">{$t('snippets.none')}</p>
    {:else}
      <ul class="list" use:reorder on:reorder={onReorder}>
        {#each $snippets as snippet, i (snippet.id)}
          <li data-reorder-id={snippet.id} class:open={editing?.id === snippet.id}>
            <span class="grip" data-reorder-handle aria-hidden="true"><Ph svg={gripIcon} size={14} /></span>
            <span class="name">{snippet.name}</span>
            {#if snippet.trigger}
              <kbd>{triggerPrefix}{snippet.trigger}</kbd>
            {/if}
            <span class="text">{snippet.body}</span>
            <span class="actions">
              <button type="button" class="c2-tool compact" aria-label={$t('snippets.moveUp').replace('{name}', snippet.name)} disabled={i === 0} on:click={() => move(i, -1)}>
                <Ph svg={upIcon} size={14} />
              </button>
              <button type="button" class="c2-tool compact" aria-label={$t('snippets.moveDown').replace('{name}', snippet.name)} disabled={i === $snippets.length - 1} on:click={() => move(i, 1)}>
                <Ph svg={downIcon} size={14} />
              </button>
              <button type="button" class="c2-tool compact" aria-label={$t('snippets.edit').replace('{name}', snippet.name)} on:click={() => edit(snippet)}>
                <Ph svg={editIcon} size={14} />
              </button>
              <button type="button" class="c2-tool compact danger" aria-label={$t('snippets.delete').replace('{name}', snippet.name)} on:click={() => remove(snippet)}>
                <Ph svg={trashIcon} size={14} />
              </button>
            </span>
          </li>
        {/each}
      </ul>
    {/if}
    {#if !editing}
      <button type="button" class="c2-tool add" on:click={() => edit(blank)}>
        <Ph svg={plusIcon} size={14} />
        <span>{$t('snippets.new')}</span>
      </button>
    {/if}
  </Row>
</Card>

{#if editing}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <section
    class="editor"
    aria-label={editing.id === 0 ? $t('snippets.new') : $t('snippets.edit').replace('{name}', editing.name)}
    on:keydown={onEditorKey}
    transition:slide={{ duration: dur(motion.base) }}
  >
    <div class="grid">
      <label for="snippet-name">{$t('snippets.field.name')}</label>
      <input id="snippet-name" bind:this={nameField} bind:value={editing.name} autocomplete="off" />

      <label for="snippet-trigger">{$t('snippets.field.trigger')}</label>
      <div class="trigger">
        <span class="prefix" aria-hidden="true">{triggerPrefix}</span>
        <input id="snippet-trigger" bind:value={editing.trigger} autocomplete="off" spellcheck="false" placeholder={$t('snippets.field.triggerPlaceholder')} />
      </div>

      <label for="snippet-subject">{$t('snippets.field.subject')}</label>
      <input id="snippet-subject" bind:value={editing.subject} autocomplete="off" placeholder={$t('snippets.field.optional')} />

      <label for="snippet-to">{$t('compose.field.to')}</label>
      <input id="snippet-to" bind:value={editing.to} autocomplete="off" placeholder={$t('snippets.field.optional')} />

      <label for="snippet-cc">{$t('compose.field.cc')}</label>
      <input id="snippet-cc" bind:value={editing.cc} autocomplete="off" placeholder={$t('snippets.field.optional')} />

      <label for="snippet-body">{$t('snippets.field.body')}</label>
      <div class="body">
        <textarea id="snippet-body" bind:this={bodyField} bind:value={editing.body} rows="7"></textarea>
        <div class="tokens" role="group" aria-label={$t('snippets.placeholders')}>
          {#each builtinPlaceholders as name (name)}
            <button type="button" class="token" on:click={() => putPlaceholder(name)}>{`{${name}}`}</button>
          {/each}
        </div>
        <p class="hint">{placeholderHint}</p>
      </div>
    </div>
    <div class="buttons">
      <button type="button" class="c2-tool" on:click={() => (editing = null)}>{$t('signatures.cancel')}</button>
      <button type="button" class="primary" disabled={saving || editing.name.trim() === ''} on:click={save}>{$t('action.save')}</button>
    </div>
  </section>
{/if}

<style>
  .empty {
    margin: 0;
    padding: var(--space-3) 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
  }

  li {
    display: grid;
    grid-template-columns: auto auto auto 1fr auto;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    background: var(--surface-raised);
  }

  li + li {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  li.open {
    background: var(--selection-bg);
  }

  .grip {
    display: inline-flex;
    color: var(--text-tertiary);
    cursor: grab;
  }

  .name {
    font-size: var(--fz-label);
    color: var(--text-primary);
    white-space: nowrap;
  }

  kbd {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .actions {
    display: inline-flex;
  }

  .danger:hover:not(:disabled) {
    background: var(--danger-bg);
    color: var(--danger);
  }

  .add {
    align-self: flex-start;
  }

  .editor {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    margin-bottom: var(--space-6);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .grid {
    display: grid;
    grid-template-columns: max-content 1fr;
    align-items: center;
    gap: var(--space-3) var(--space-4);
  }

  .grid label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .grid label[for='snippet-body'] {
    align-self: start;
    padding-top: var(--space-2);
  }

  input,
  textarea {
    width: 100%;
    min-width: 0;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    outline: none;
    box-sizing: border-box;
  }

  input:focus,
  textarea:focus {
    border-color: var(--accent);
  }

  textarea {
    resize: vertical;
    line-height: 1.5;
  }

  .trigger {
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }

  .prefix {
    font-family: var(--font-mono);
    color: var(--text-tertiary);
  }

  .trigger input {
    font-family: var(--font-mono);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .tokens {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .token {
    padding: var(--space-1) var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .token:hover {
    color: var(--text-primary);
    border-color: var(--border-default);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }

  .primary {
    height: var(--control-height);
    padding: 0 var(--space-4);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    cursor: var(--cursor-action);
  }

  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
