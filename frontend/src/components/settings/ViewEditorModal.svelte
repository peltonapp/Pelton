<script lang="ts">
  // the saved View builder: name, icon, color, the search query (text + from/to/
  // subject), a relative date window, scope toggles and an account scope. used
  // both for creating a view from scratch and for editing an existing one (and
  // for "save this search as a view", where the parent seeds the query fields).
  import { createEventDispatcher } from 'svelte'
  import { IconX } from '@tabler/icons-svelte'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import type { View, Account } from '../../lib/types'
  import { viewIconNames, viewIconComponent, viewColors, viewColorCss } from '../../lib/viewicons'
  import { saveView } from '../../lib/api'
  import { upsertView } from '../../stores/views'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  // value is the working view. the parent passes a blank view (id 0) to create,
  // or an existing view to edit; either way it is copied so edits stay local
  // until save.
  export let value: View
  export let accounts: Account[] = []

  const dispatch = createEventDispatcher<{ saved: View; close: void }>()

  // local copy so cancelling discards edits.
  let draft: View = { ...value, queryFrom: [...(value.queryFrom ?? [])], queryTo: [...(value.queryTo ?? [])] }
  let saving = false
  // what the draft looked like when the editor opened, so closing knows whether
  // there is anything to lose.
  const opened = JSON.stringify(draft)

  $: dirty = JSON.stringify(draft) !== opened

  // pending chip text per address field, committed on Enter/comma/blur.
  let chipDraft: { queryFrom: string; queryTo: string } = { queryFrom: '', queryTo: '' }

  type ChipField = 'queryFrom' | 'queryTo'

  // addChips folds the pending text into the field's chip list, splitting on
  // comma/semicolon/newline and dropping duplicates and blanks.
  function addChips(field: ChipField): void {
    const parts = chipDraft[field]
      .split(/[,;\n]/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (parts.length) {
      const seen = new Set(draft[field].map((a) => a.toLowerCase()))
      const next = [...draft[field]]
      for (const p of parts) {
        const key = p.toLowerCase()
        if (!seen.has(key)) {
          next.push(p)
          seen.add(key)
        }
      }
      draft[field] = next
    }
    chipDraft = { ...chipDraft, [field]: '' }
  }

  function onChipKey(e: KeyboardEvent, field: ChipField): void {
    if (e.key === 'Enter' || e.key === ',' || e.key === ';') {
      e.preventDefault()
      addChips(field)
    } else if (e.key === 'Backspace' && chipDraft[field] === '' && draft[field].length) {
      draft[field] = draft[field].slice(0, -1)
    }
  }

  function removeChip(field: ChipField, i: number): void {
    draft[field] = draft[field].filter((_, idx) => idx !== i)
  }

  // relative date window presets, in days. 0 means no bound.
  const windows: { days: number; key: string }[] = [
    { days: 0, key: 'views.window.any' },
    { days: 1, key: 'views.window.day' },
    { days: 7, key: 'views.window.week' },
    { days: 30, key: 'views.window.month' },
    { days: 90, key: 'views.window.quarter' },
    { days: 365, key: 'views.window.year' },
  ]

  $: canSave = draft.name.trim() !== '' && !saving

  async function save(): Promise<void> {
    if (!canSave) {
      return
    }
    // fold any half-typed address into its chip list before persisting.
    addChips('queryFrom')
    addChips('queryTo')
    saving = true
    try {
      const saved = await saveView(draft)
      upsertView(saved)
      dispatch('saved', saved)
      dispatch('close')
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      saving = false
    }
  }
</script>

<Modal
  title={draft.id === 0 ? $t('views.newTitle') : $t('views.editTitle')}
  size="medium"
  {dirty}
  on:close={() => dispatch('close')}
>
  <div class="m-body">
      <label class="field">
        <span class="label">{$t('views.nameLabel')}</span>
        <input type="text" bind:value={draft.name} placeholder={$t('views.namePlaceholder')} maxlength="60" />
      </label>

      <div class="field">
        <span class="label">{$t('views.iconLabel')}</span>
        <div class="icon-grid">
          {#each viewIconNames as name (name)}
            <button
              type="button"
              class="icon-cell"
              class:on={draft.icon === name || (draft.icon === '' && name === 'bookmark')}
              title={name}
              style={`color:${viewColorCss(draft.color)}`}
              on:click={() => (draft.icon = name)}
            >
              <svelte:component this={viewIconComponent(name)} size={18} stroke={1.7} />
            </button>
          {/each}
        </div>
      </div>

      <div class="field">
        <span class="label">{$t('views.colorLabel')}</span>
        <div class="swatches">
          {#each viewColors as color (color.value)}
            <button
              type="button"
              class="swatch"
              class:on={draft.color === color.value}
              style={`--sw:${color.css}`}
              aria-label={color.value || $t('views.colorNone')}
              on:click={() => (draft.color = color.value)}
            ></button>
          {/each}
        </div>
      </div>

      <div class="section-head">{$t('views.filterHeading')}</div>

      <label class="field">
        <span class="label">{$t('views.queryText')}</span>
        <input type="text" bind:value={draft.queryText} placeholder={$t('views.queryTextPlaceholder')} />
      </label>

      <div class="two">
        <div class="field">
          <span class="label">{$t('views.queryFrom')}</span>
          <div class="chips">
            {#each draft.queryFrom as addr, i (addr)}
              <span class="chip">
                {addr}
                <button type="button" class="chip-x" aria-label={$t('views.removeChip')} on:click={() => removeChip('queryFrom', i)}>
                  <IconX size={12} stroke={2} />
                </button>
              </span>
            {/each}
            <input
              type="text"
              class="chip-in"
              bind:value={chipDraft.queryFrom}
              placeholder={draft.queryFrom.length ? '' : $t('views.queryFromPlaceholder')}
              on:keydown={(e) => onChipKey(e, 'queryFrom')}
              on:blur={() => addChips('queryFrom')}
            />
          </div>
        </div>
        <div class="field">
          <span class="label">{$t('views.queryTo')}</span>
          <div class="chips">
            {#each draft.queryTo as addr, i (addr)}
              <span class="chip">
                {addr}
                <button type="button" class="chip-x" aria-label={$t('views.removeChip')} on:click={() => removeChip('queryTo', i)}>
                  <IconX size={12} stroke={2} />
                </button>
              </span>
            {/each}
            <input
              type="text"
              class="chip-in"
              bind:value={chipDraft.queryTo}
              placeholder={draft.queryTo.length ? '' : $t('views.queryToPlaceholder')}
              on:keydown={(e) => onChipKey(e, 'queryTo')}
              on:blur={() => addChips('queryTo')}
            />
          </div>
        </div>
      </div>
      <span class="hint">{$t('views.chipHint')}</span>

      <label class="field">
        <span class="label">{$t('views.querySubject')}</span>
        <input type="text" bind:value={draft.querySubject} placeholder={$t('views.querySubjectPlaceholder')} />
      </label>

      <label class="check">
        <input type="checkbox" bind:checked={draft.useRegex} />
        <span>{$t('views.useRegex')}</span>
      </label>
      {#if draft.useRegex}
        <span class="hint">{$t('views.useRegexHint')}</span>
      {/if}

      <div class="two">
        <label class="field">
          <span class="label">{$t('views.dateWindow')}</span>
          <Select
            value={String(draft.withinDays)}
            ariaLabel={$t('views.dateWindow')}
            items={windows.map((w) => ({ value: String(w.days), label: $t(w.key) }))}
            on:change={(e) => (draft.withinDays = Number(e.detail))}
          />
        </label>
        <label class="field">
          <span class="label">{$t('views.accountScope')}</span>
          <Select
            value={String(draft.accountId)}
            ariaLabel={$t('views.accountScope')}
            items={[
              { value: '0', label: $t('views.allAccounts') },
              ...accounts.map((acc) => ({ value: String(acc.id), label: acc.email })),
            ]}
            on:change={(e) => (draft.accountId = Number(e.detail))}
          />
        </label>
      </div>

      <div class="toggles">
        <label class="check">
          <input type="checkbox" bind:checked={draft.unreadOnly} />
          <span>{$t('views.unreadOnly')}</span>
        </label>
        <label class="check">
          <input type="checkbox" bind:checked={draft.flaggedOnly} />
          <span>{$t('views.flaggedOnly')}</span>
        </label>
        <label class="check">
          <input type="checkbox" bind:checked={draft.hasAttachment} />
          <span>{$t('views.hasAttachment')}</span>
        </label>
      </div>
    </div>

  <svelte:fragment slot="footer">
    <button type="button" class="btn ghost" on:click={() => dispatch('close')}>{$t('views.cancel')}</button>
    <button type="button" class="btn primary" disabled={!canSave} on:click={save}>{$t('views.save')}</button>
  </svelte:fragment>
</Modal>

<style>

  .m-body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }

  .label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  input[type='text'] {
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-size: var(--fz-label);
    outline: none;
  }

  input[type='text']:focus {
    border-color: var(--accent);
  }

  /* the select carries its own border, background and focus ring; how wide it
     is, is the form's business rather than the control's. */
  .field :global(.select) {
    width: 100%;
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .chips:focus-within {
    border-color: var(--accent);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 2px var(--space-1) 2px var(--space-2);
    border-radius: 999px;
    background: var(--selection-bg);
    color: var(--text-primary);
    font-size: var(--fz-meta);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip-x {
    display: inline-flex;
    align-items: center;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: 0;
  }

  .chip-x:hover {
    color: var(--text-primary);
  }

  .chip-in {
    flex: 1;
    min-width: 80px;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    padding: var(--space-1) 0;
  }

  .hint {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .section-head {
    margin-top: var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .icon-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(34px, 1fr));
    gap: var(--space-1);
  }

  .icon-cell {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 1;
    border: var(--hairline) solid transparent;
    background: transparent;
    color: var(--text-secondary);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .icon-cell:hover {
    background: var(--surface-hover);
  }

  .icon-cell.on {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .swatches {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .swatch {
    width: 24px;
    height: 24px;
    border-radius: 999px;
    border: 2px solid transparent;
    background: var(--sw);
    cursor: var(--cursor-action);
  }

  .swatch.on {
    border-color: var(--text-primary);
  }

  .toggles {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .btn {
    padding: var(--space-2) var(--space-4);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
    border: var(--hairline) solid var(--border-default);
  }

  .btn.ghost {
    background: var(--surface-raised);
    color: var(--text-secondary);
  }

  .btn.ghost:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .btn.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-contrast, #fff);
  }

  .btn.primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
