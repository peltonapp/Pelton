<script lang="ts">
  // the side by side compare of a group of duplicate contacts: one column per
  // card, a row per field with a radio to pick which card the value comes from
  // (only where the cards disagree), the addresses and numbers combined, and a
  // preview of the merged contact. which card survives is picked too, and when
  // the cards are in different address books so is the book the merged contact
  // goes in. nothing is written until Merge.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { mergeGroup } from '../../stores/duplicates'
  import {
    bookIds,
    buildRequest,
    chosenValue,
    defaultChoices,
    defaultSurvivor,
    differs,
    extraProperties,
    keepBoth,
    mergeFields,
    needsTarget,
    unionValues,
    type Choices,
    type MergeField,
  } from '../../lib/contactmerge'
  import { t } from '../../lib/i18n'
  import type { AddressBook, Contact } from '../../lib/types'

  /** The contacts to merge. */
  export let group: Contact[]
  /** Every address book, to name the books and to offer as the target. */
  export let books: AddressBook[]

  const dispatch = createEventDispatcher<{ close: void; merged: void }>()

  const labels: Record<MergeField, string> = {
    fullName: 'contacts.field.name',
    organization: 'contacts.field.organization',
    title: 'contacts.field.title',
    note: 'contacts.field.note',
  }

  let survivorId = defaultSurvivor(group).id
  let choices: Choices = defaultChoices(group, defaultSurvivor(group))
  let targetBookId = 0
  let busy = false

  $: survivor = group.find((c) => c.id === survivorId) ?? group[0]
  $: bookName = (id: number) => books.find((b) => b.id === id)?.name || $t('contacts.book.untitled')
  $: involved = bookIds(group)
  $: spans = needsTarget(group)
  $: readOnlyBook = books.find((b) => (involved.includes(b.id) || b.id === targetBookId) && b.readOnly)
  $: writable = books.filter((b) => !b.readOnly)
  $: emails = unionValues(group, survivor, 'emails')
  $: phones = unionValues(group, survivor, 'phones')
  $: extras = extraProperties(group)
  $: merged = Object.fromEntries(mergeFields.map((f) => [f, chosenValue(group, f, choices[f])])) as Record<MergeField, string>
  $: ready = !busy && !readOnlyBook && (!spans || targetBookId !== 0)

  // moving the survivor keeps what was picked, except that a field the new
  // survivor holds goes back to it when the pick was the old survivor's.
  function pickSurvivor(id: number): void {
    const before = survivorId
    survivorId = id
    const next = group.find((c) => c.id === id) ?? group[0]
    for (const field of mergeFields) {
      if (choices[field] === before && next[field].trim() !== '') {
        choices[field] = id
      }
    }
    choices = choices
  }

  async function merge(): Promise<void> {
    if (!ready) {
      return
    }
    busy = true
    const done = await mergeGroup(buildRequest(group, { survivorId, choices, targetBookId }), group.length)
    busy = false
    if (done) {
      dispatch('merged')
    }
  }
</script>

<Modal title={$t('contacts.merge.title')} hint={$t('contacts.merge.hint')} size="large" closable={!busy} on:close={() => dispatch('close')}>
  <div class="scroll">
    <div class="table" style={`--n:${group.length}`} role="group" aria-label={$t('contacts.merge.title')}>
      <div class="row head">
        <span class="label"></span>
        {#each group as c (c.id)}
          <div class="cell">
            <label class="keep">
              <input type="radio" name="survivor" checked={c.id === survivorId} on:change={() => pickSurvivor(c.id)} />
              <span>{$t('contacts.merge.survivor')}</span>
            </label>
            <strong class="who">{c.fullName || c.emails[0]?.value || $t('contacts.book.untitled')}</strong>
            <span class="book">{c.bookName || bookName(c.bookId)}</span>
          </div>
        {/each}
      </div>

      {#each mergeFields as field (field)}
        {@const options = differs(group, field)}
        <div class="row" role="radiogroup" aria-label={$t('contacts.merge.fieldGroup').replace('{field}', $t(labels[field]).toLowerCase())}>
          <span class="label">{$t(labels[field])}</span>
          {#each group as c (c.id)}
            <div class="cell">
              {#if c[field].trim() === ''}
                <span class="empty">{$t('contacts.merge.empty')}</span>
              {:else if options}
                <label class="pick">
                  <input type="radio" name={`field-${field}`} checked={choices[field] === c.id} on:change={() => (choices = { ...choices, [field]: c.id })} />
                  <span class="value">{c[field]}</span>
                </label>
              {:else}
                <span class="value">{c[field]}</span>
              {/if}
            </div>
          {/each}
          {#if field === 'note' && options}
            <div class="both">
              <label class="pick">
                <input type="radio" name="field-note" checked={choices.note === keepBoth} on:change={() => (choices = { ...choices, note: keepBoth })} />
                <span>{$t('contacts.merge.keepBoth')}</span>
              </label>
            </div>
          {/if}
        </div>
      {/each}

      {#each [{ key: 'emails', label: 'contacts.field.emails' }, { key: 'phones', label: 'contacts.field.phones' }] as list (list.key)}
        <div class="row">
          <span class="label">{$t(list.label)}</span>
          {#each group as c (c.id)}
            <div class="cell">
              {#each list.key === 'emails' ? c.emails : c.phones as v, i (i)}
                <span class="value">{v.value}</span>
              {:else}
                <span class="empty">{$t('contacts.merge.none')}</span>
              {/each}
            </div>
          {/each}
        </div>
      {/each}

      <div class="row">
        <span class="label">{$t('contacts.merge.other')}</span>
        {#each group as c (c.id)}
          <div class="cell">
            {#each c.extra as name, i (i)}
              <span class="value mono">{name}</span>
            {:else}
              <span class="empty">{$t('contacts.merge.none')}</span>
            {/each}
          </div>
        {/each}
      </div>
    </div>

    {#if extras.length > 0}
      <p class="note">{$t('contacts.merge.otherHint')}</p>
    {/if}

    <section class="result" aria-label={$t('contacts.merge.result')}>
      <h4>{$t('contacts.merge.result')}</h4>
      <dl>
        {#each mergeFields as field (field)}
          {#if merged[field]}
            <dt>{$t(labels[field])}</dt>
            <dd class:multiline={field === 'note'}>{merged[field]}</dd>
          {/if}
        {/each}
        {#if emails.length > 0}
          <dt>{$t('contacts.field.emails')}</dt>
          <dd>{emails.map((e) => e.value).join(', ')}</dd>
        {/if}
        {#if phones.length > 0}
          <dt>{$t('contacts.field.phones')}</dt>
          <dd>{phones.map((p) => p.value).join(', ')}</dd>
        {/if}
      </dl>
    </section>

    {#if spans}
      <div class="target">
        <span class="target-label" id="merge-target">{$t('contacts.merge.target')}</span>
        <Select
          value={String(targetBookId)}
          ariaLabel={$t('contacts.merge.target')}
          items={[
            { value: '0', label: $t('contacts.field.bookPlaceholder'), disabled: true },
            ...writable.map((b) => ({ value: String(b.id), label: b.name || $t('contacts.book.untitled') })),
          ]}
          on:change={(e) => (targetBookId = Number(e.detail))}
        />
        <p class="note">{$t('contacts.merge.targetHint')}</p>
      </div>
    {/if}

    {#if readOnlyBook}
      <p class="problem" role="alert">{$t('contacts.merge.readOnly').replace('{book}', readOnlyBook.name || $t('contacts.book.untitled'))}</p>
    {/if}

    <p class="note">{$t('contacts.merge.syncNote')}</p>
  </div>

  <svelte:fragment slot="footer">
    <button type="button" class="btn" disabled={busy} on:click={() => dispatch('close')}>{$t('contacts.merge.cancel')}</button>
    <button type="button" class="btn primary" disabled={!ready} on:click={merge}>
      {#if busy}
        <PieProgress label={$t('contacts.merge.merging')} />
      {/if}
      {$t('contacts.merge.confirm').replace('{n}', String(group.length))}
    </button>
  </svelte:fragment>
</Modal>

<style>
  .scroll {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    width: 100%;
    min-width: 0;
  }

  .table {
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow-x: auto;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-card);
  }

  .row {
    display: grid;
    grid-template-columns: calc(var(--fz-body) * 8) repeat(var(--n), minmax(calc(var(--fz-body) * 11), 1fr));
    min-width: min-content;
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row:first-child {
    border-top: none;
  }

  .row.head {
    background: var(--surface-sunken);
  }

  .label {
    padding: var(--space-3);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .cell {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
    padding: var(--space-3);
    border-inline-start: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-body);
  }

  .both {
    grid-column: 2 / -1;
    padding: 0 var(--space-3) var(--space-3);
  }

  .keep,
  .pick {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    cursor: var(--cursor-action);
  }

  .keep {
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  input[type='radio'] {
    flex-shrink: 0;
    margin: 0.2em 0 0;
    accent-color: var(--accent);
  }

  .who {
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    overflow-wrap: anywhere;
  }

  .book {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .value {
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .mono {
    font-family: var(--font-mono);
    font-size: var(--fz-label);
  }

  .empty {
    color: var(--text-tertiary);
  }

  .result {
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
  }

  .result h4 {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  dl {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: var(--space-1) var(--space-4);
    margin: 0;
    font-size: var(--fz-body);
  }

  dt {
    color: var(--text-secondary);
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  dd.multiline {
    white-space: pre-wrap;
  }

  .target {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .target-label {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .note {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }

  .problem {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    background: var(--danger-bg);
    color: var(--danger);
    font-size: var(--fz-label);
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .btn:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .btn.primary {
    border-color: transparent;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .btn.primary:hover:not(:disabled) {
    background: var(--accent);
    filter: brightness(1.06);
  }

  .btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
