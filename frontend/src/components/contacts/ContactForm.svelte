<script lang="ts">
  // the contact editor in the detail pane. It edits the fields Pelton shows;
  // everything else on the card is kept untouched by the save, which is what the
  // line at the bottom says.
  import { createEventDispatcher } from 'svelte'
  import { fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import minusIcon from '@phosphor-icons/core/assets/regular/minus.svg?raw'
  import { addressBooks, saveContact } from '../../stores/contacts'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { ContactDraft } from '../../lib/types'

  /** The contact being edited. id 0 creates a new one. */
  export let draft: ContactDraft

  const dispatch = createEventDispatcher<{ close: void; dirty: boolean }>()

  let saving = false
  const opened = JSON.stringify(draft)

  // the space asks before it throws away what was typed.
  $: dispatch('dirty', JSON.stringify(draft) !== opened)

  // a new contact cannot be saved until a book is chosen: one filed in the wrong
  // book is a contact on the wrong device.
  $: writable = $addressBooks.filter((b) => !b.readOnly)
  // a Matrix ID starts with @ and has a colon before the server name.
  $: matrixId = (draft.matrixId ?? '').trim()
  $: matrixInvalid = matrixId !== '' && !/^@[^:\s]+:\S+$/.test(matrixId)
  $: canSave =
    !saving &&
    !matrixInvalid &&
    draft.bookId !== 0 &&
    (draft.fullName.trim() !== '' || draft.emails.some((e) => e.value.trim() !== ''))

  function addEmail(): void {
    draft.emails = [...draft.emails, { value: '', label: '' }]
  }

  function addPhone(): void {
    draft.phones = [...draft.phones, { value: '', label: '' }]
  }

  async function save(): Promise<void> {
    saving = true
    draft.matrixId = matrixId
    const saved = await saveContact(draft)
    saving = false
    if (saved) {
      dispatch('close')
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      dispatch('close')
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<form
  class="form"
  aria-label={draft.id === 0 ? $t('contacts.editor.new') : $t('contacts.editor.edit')}
  on:submit|preventDefault={save}
  on:keydown={onKeydown}
  in:fly={{ y: 6, duration: dur(motion.base), easing: cubicOut }}
>
  <h2>{draft.id === 0 ? $t('contacts.editor.new') : $t('contacts.editor.edit')}</h2>

  <label class="field">
    <span>{$t('contacts.field.book')}</span>
    <Select
      value={String(draft.bookId)}
      disabled={draft.id !== 0}
      ariaLabel={$t('contacts.field.book')}
      items={[
        { value: '0', label: $t('contacts.field.bookPlaceholder'), disabled: true },
        ...writable.map((book) => ({ value: String(book.id), label: book.name || $t('contacts.book.untitled') })),
      ]}
      on:change={(e) => (draft.bookId = Number(e.detail))}
    />
  </label>

  <label class="field">
    <span>{$t('contacts.field.name')}</span>
    <input type="text" bind:value={draft.fullName} />
  </label>

  <div class="pair">
    <label class="field">
      <span>{$t('contacts.field.organization')}</span>
      <input type="text" bind:value={draft.organization} />
    </label>
    <label class="field">
      <span>{$t('contacts.field.title')}</span>
      <input type="text" bind:value={draft.title} />
    </label>
  </div>

  <fieldset>
    <legend>{$t('contacts.field.emails')}</legend>
    {#each draft.emails as email, i (i)}
      <div class="value-row" in:fly={{ y: -4, duration: dur(motion.fast), easing: cubicOut }}>
        <input type="email" bind:value={email.value} placeholder="name@example.com" aria-label={$t('contacts.field.emails')} />
        <input type="text" bind:value={email.label} placeholder={$t('contacts.field.labelPlaceholder')} class="label-input" aria-label={$t('contacts.field.label')} />
        <button
          type="button"
          class="icon"
          aria-label={$t('contacts.field.removeEmail')}
          on:click={() => (draft.emails = draft.emails.filter((_, n) => n !== i))}
        >
          <Ph svg={minusIcon} size={14} />
        </button>
      </div>
    {/each}
    <button type="button" class="add" on:click={addEmail}>
      <Ph svg={plusIcon} size={12} />
      {$t('contacts.field.addEmail')}
    </button>
  </fieldset>

  <fieldset>
    <legend>{$t('contacts.field.phones')}</legend>
    {#each draft.phones as phone, i (i)}
      <div class="value-row" in:fly={{ y: -4, duration: dur(motion.fast), easing: cubicOut }}>
        <input type="tel" bind:value={phone.value} aria-label={$t('contacts.field.phones')} />
        <input type="text" bind:value={phone.label} placeholder={$t('contacts.field.labelPlaceholder')} class="label-input" aria-label={$t('contacts.field.label')} />
        <button
          type="button"
          class="icon"
          aria-label={$t('contacts.field.removePhone')}
          on:click={() => (draft.phones = draft.phones.filter((_, n) => n !== i))}
        >
          <Ph svg={minusIcon} size={14} />
        </button>
      </div>
    {/each}
    <button type="button" class="add" on:click={addPhone}>
      <Ph svg={plusIcon} size={12} />
      {$t('contacts.field.addPhone')}
    </button>
  </fieldset>

  <label class="field">
    <span>{$t('contacts.matrix')}</span>
    <input
      type="text"
      bind:value={draft.matrixId}
      placeholder="@name:example.org"
      autocapitalize="off"
      autocomplete="off"
      spellcheck="false"
      dir="ltr"
      aria-invalid={matrixInvalid}
    />
    {#if matrixInvalid}
      <span class="invalid" role="alert">{$t('contacts.matrix.invalid')}</span>
    {/if}
  </label>

  <label class="field">
    <span>{$t('contacts.field.note')}</span>
    <textarea rows="4" bind:value={draft.note}></textarea>
  </label>

  <p class="hint">{$t('contacts.editor.keepsRest')}</p>

  <div class="actions">
    <button type="button" class="btn" on:click={() => dispatch('close')}>{$t('mailboxes.cancel')}</button>
    <button type="submit" class="btn primary" disabled={!canSave}>
      {saving ? $t('contacts.editor.saving') : $t('action.save')}
    </button>
  </div>
</form>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-width: 0;
    padding: var(--space-4);
    overflow-y: auto;
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
  }

  .field,
  fieldset {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
    margin: 0;
    padding: 0;
    border: none;
  }

  .field > span,
  legend {
    padding: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  legend {
    margin-bottom: var(--space-1);
  }

  .pair {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .pair .field {
    flex: 1 1 140px;
  }

  input,
  textarea {
    min-width: 0;
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  input {
    height: var(--control-height);
  }

  input:focus,
  textarea:focus {
    border-color: var(--accent);
    outline: none;
  }

  input[aria-invalid='true'] {
    border-color: var(--danger);
  }

  .field > span.invalid {
    color: var(--danger);
  }

  textarea {
    padding: var(--space-2);
    resize: vertical;
  }

  .field :global(.select) {
    height: var(--control-height);
    font-size: var(--fz-body);
  }

  fieldset {
    gap: var(--space-2);
  }

  .value-row {
    display: flex;
    gap: var(--space-2);
  }

  .value-row input {
    flex: 1;
  }

  .value-row .label-input {
    flex: 0 0 30%;
  }

  .add {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0;
    border: none;
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .add:hover {
    text-decoration: underline;
  }

  .hint {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }

  .btn {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .btn:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .btn:active:not(:disabled) {
    transform: scale(0.98);
  }

  .btn.primary {
    border-color: transparent;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .btn.primary:hover:not(:disabled) {
    background: var(--accent);
    filter: brightness(0.94);
  }

  .btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .icon {
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
    color: var(--danger);
  }

  @media (prefers-reduced-motion: reduce) {
    .btn,
    input,
    textarea {
      transition: none;
    }
  }
</style>
