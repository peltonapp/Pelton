<script lang="ts">
  // the one import every calendar file goes through, whether it was picked,
  // dropped or opened with Pelton: which files and how many events they hold,
  // the calendar they go into, then the import and a toast with the result.
  import { createEventDispatcher } from 'svelte'
  import fileIcon from '@phosphor-icons/core/assets/duotone/file-text-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import '../../theme/controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import Ph from '../common/Ph.svelte'
  import { importICSData } from '../../lib/api'
  import { calendarColor } from '../../lib/calformat'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Calendar, ICSFile } from '../../lib/types'

  /** The files read for import. */
  export let files: ICSFile[]
  /** Calendars the events can go in, and the one chosen. */
  export let calendars: Calendar[]
  export let calendarId: number

  const dispatch = createEventDispatcher<{ close: void }>()

  let running = false

  $: readable = files.filter((f) => !f.error && f.events + (f.tasks ?? 0) > 0)
  $: total = readable.reduce((n, f) => n + f.events + (f.tasks ?? 0), 0)
  $: items = calendars.map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName'), color: calendarColor(c) }))

  async function run(): Promise<void> {
    if (running || total === 0) {
      return
    }
    running = true
    let imported = 0
    let skipped = 0
    let tasks = 0
    try {
      for (const f of readable) {
        const r = await importICSData(calendarId, f.data)
        imported += r.imported + r.updated
        skipped += r.skipped
        tasks += r.tasks ?? 0
      }
      let message = $t('calendar.imported').replace('{n}', String(imported))
      if (tasks) {
        message += ` ${$t('calendar.importTasks').replace('{n}', String(tasks))}`
      }
      toastSuccess(skipped ? `${message} ${$t('calendar.importSkipped').replace('{n}', String(skipped))}` : message)
      dispatch('close')
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      running = false
    }
  }
</script>

<Modal title={$t('calendar.importTitle')} size="small" closable={!running} on:close>
  <ul class="files">
    {#each files as f, i (i)}
      <li class="file" class:bad={f.error || f.events + (f.tasks ?? 0) === 0}>
        <span class="icon" aria-hidden="true"><Ph svg={f.error ? warningIcon : fileIcon} size={16} /></span>
        <div class="text">
          <span class="name">{f.name}</span>
          {#if f.error}
            <span class="meta">{$t('calendar.importUnreadable')}</span>
          {:else}
            <span class="meta">{$t('calendar.importCount').replace('{n}', String(f.events))}{f.summaries.length ? `: ${f.summaries.filter(Boolean).join(', ')}` : ''}{f.tasks ? `. ${$t('calendar.importTaskCount').replace('{n}', String(f.tasks))}` : ''}</span>
          {/if}
        </div>
      </li>
    {/each}
  </ul>

  <div class="row">
    <span class="label">{$t('calendar.importInto')}</span>
    <Select class="ctl" value={String(calendarId)} {items} ariaLabel={$t('calendar.importInto')} on:change={(e) => (calendarId = Number(e.detail))} />
  </div>

  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn" disabled={running} on:click={() => dispatch('close')}>{$t('calendar.cancel')}</button>
    <button type="button" class="cal-btn primary" disabled={running || total === 0 || items.length === 0} on:click={run}>
      {$t('calendar.importCountButton').replace('{n}', String(total))}
    </button>
  </svelte:fragment>
</Modal>

<style>
  .files {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .file {
    display: grid;
    grid-template-columns: var(--space-6) minmax(0, 1fr);
    align-items: start;
    column-gap: var(--space-3);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .icon {
    display: inline-flex;
    justify-content: center;
    color: var(--text-tertiary);
  }

  .bad .icon {
    color: var(--warning);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .name {
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .meta {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding-top: var(--space-3);
  }

  .label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .row :global(.ctl) {
    height: var(--control-height);
    font-size: var(--fz-list);
  }
</style>
