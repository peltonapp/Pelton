<script lang="ts">
  // the settings of a board column: its name, how many cards it should hold,
  // and whether completed tasks gather in it. deleting a column asks where its
  // cards go: to another column, or off the board, their tasks kept.
  import { createEventDispatcher } from 'svelte'
  import '../../theme/controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { deleteProjectColumn, setProjectDoneColumn, updateProjectColumn } from '../../lib/api'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { ProjectColumn } from '../../lib/types'

  /** The column edited. */
  export let column: ProjectColumn
  /** Every column of the board, for where the cards of a deleted one go. */
  export let columns: ProjectColumn[]
  /** How many cards the column holds. */
  export let count = 0

  const dispatch = createEventDispatcher<{ close: void; changed: void }>()

  let name = column.name
  let limit = column.wipLimit > 0 ? String(column.wipLimit) : ''
  let done = column.done
  let deleting = false
  let moveTo = String(columns.find((c) => c.id !== column.id)?.id ?? 0)
  let busy = false

  $: others = columns.filter((c) => c.id !== column.id)
  $: targetItems = [
    ...others.map((c) => ({ value: String(c.id), label: $t('projects.column.moveTo').replace('{name}', c.name) })),
    { value: '0', label: $t('projects.column.dropCards') },
  ]
  $: wip = Math.max(0, Math.floor(Number(limit) || 0))
  $: dirty = name !== column.name || wip !== column.wipLimit || done !== column.done

  async function save(): Promise<void> {
    if (!name.trim() || busy) {
      return
    }
    busy = true
    try {
      if (name.trim() !== column.name || wip !== column.wipLimit) {
        await updateProjectColumn(column.id, name.trim(), wip)
      }
      if (done !== column.done) {
        await setProjectDoneColumn(column.projectId, done ? column.id : 0)
      }
      dispatch('changed')
      dispatch('close')
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function remove(): Promise<void> {
    busy = true
    try {
      await deleteProjectColumn(column.id, Number(moveTo))
      dispatch('changed')
      dispatch('close')
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }
</script>

<Modal title={deleting ? $t('projects.column.deleteTitle').replace('{name}', column.name) : $t('projects.column.settings')} size="small" dirty={dirty && !deleting} on:close>
  {#if deleting}
    <div class="form">
      <p class="lead">
        {count === 0 ? $t('projects.column.deleteEmpty') : $t('projects.column.deleteBody').replace('{n}', String(count))}
      </p>
      {#if count > 0}
        <Select value={moveTo} items={targetItems} ariaLabel={$t('projects.column.cardsGo')} on:change={(e) => (moveTo = e.detail)} />
      {/if}
    </div>
  {:else}
    <form class="form" id="column-form" on:submit|preventDefault={save}>
      <label class="field">
        <span class="label">{$t('projects.column.name')}</span>
        <!-- svelte-ignore a11y-autofocus -->
        <input class="cal-field" bind:value={name} autofocus maxlength="80" />
      </label>
      <label class="field">
        <span class="label">{$t('projects.column.limit')}</span>
        <input class="cal-field limit" type="number" min="0" step="1" inputmode="numeric" bind:value={limit} placeholder={$t('projects.column.noLimit')} />
        <span class="hint">{$t('projects.column.limitHint')}</span>
      </label>
      <div class="toggle">
        <div>
          <span class="label">{$t('projects.column.doneLabel')}</span>
          <span class="hint">{$t('projects.column.doneHint')}</span>
        </div>
        <ToggleSwitch checked={done} label={$t('projects.column.doneLabel')} on:change={(e) => (done = e.detail)} />
      </div>
    </form>
  {/if}

  <svelte:fragment slot="footer">
    <div class="actions">
      {#if deleting}
        <button type="button" class="cal-btn" on:click={() => (deleting = false)}>{$t('projects.dialog.cancel')}</button>
        <span class="spacer"></span>
        <button type="button" class="cal-btn primary danger" disabled={busy} on:click={() => void remove()}>{$t('projects.column.delete')}</button>
      {:else}
        <button type="button" class="cal-btn danger" on:click={() => (deleting = true)}>{$t('projects.column.delete')}</button>
        <span class="spacer"></span>
        <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('projects.dialog.cancel')}</button>
        <button type="submit" form="column-form" class="cal-btn primary" disabled={!name.trim() || busy}>{$t('projects.dialog.save')}</button>
      {/if}
    </div>
  </svelte:fragment>
</Modal>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .lead {
    margin: 0;
    font-size: var(--fz-list);
    line-height: 1.5;
    color: var(--text-secondary);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .label {
    display: block;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-secondary);
  }

  .hint {
    display: block;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .limit {
    width: calc(var(--space-6) * 4);
  }

  .toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
  }

  .spacer {
    flex: 1;
  }
</style>
