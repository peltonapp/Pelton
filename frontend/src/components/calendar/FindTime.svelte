<script lang="ts">
  // "Find time" for a task: the free slots that hold a block of its length,
  // from free time, the working hours and the task's due date, earliest
  // first. A slot after the due date says so. Picking one blocks it; the
  // length can be changed first, which asks again. Everything is worked out
  // on this computer.
  import { createEventDispatcher, onMount } from 'svelte'
  import '../../theme/controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import { findFreeSlots } from '../../lib/api'
  import { formatWhen } from '../../lib/calformat'
  import { formatMinutes } from './timeblocks'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { FreeSlot, Task } from '../../lib/types'

  /** The task time is found for. */
  export let task: Task
  /** How long the block is at first, in minutes. */
  export let minutes: number
  export let timeFormat: TimeFormat = 'auto'

  const dispatch = createEventDispatcher<{ close: void; pick: { start: Date; end: Date } }>()

  const lengths = [15, 30, 45, 60, 90, 120, 180, 240]

  let slots: FreeSlot[] = []
  let loading = true
  let failure = ''

  $: lengthItems = [...new Set([...lengths, minutes])].sort((a, b) => a - b).map((m) => ({ value: String(m), label: formatMinutes(m, $t) }))

  async function search(length: number): Promise<void> {
    loading = true
    failure = ''
    try {
      slots = await findFreeSlots(task.id, length)
    } catch (err) {
      slots = []
      failure = errorMessage(err)
    } finally {
      loading = false
    }
  }

  function setLength(value: string): void {
    minutes = Number(value)
    void search(minutes)
  }

  onMount(() => void search(minutes))
</script>

<Modal title={$t('timeblock.find.title').replace('{title}', task.title || $t('tasks.untitled'))} hint={$t('timeblock.find.hint')} on:close={() => dispatch('close')}>
  <div class="length">
    <span aria-hidden="true">{$t('timeblock.find.length')}</span>
    <Select value={String(minutes)} items={lengthItems} ariaLabel={$t('timeblock.find.length')} on:change={(e) => setLength(e.detail)} />
  </div>
  <div aria-live="polite" aria-busy={loading}>
    {#if loading}
      <p class="note">{$t('timeblock.find.looking')}</p>
    {:else if failure}
      <p class="note">{failure}</p>
    {:else if slots.length === 0}
      <p class="note">{$t('timeblock.find.none')}</p>
    {:else}
      <p class="note">{$t('timeblock.find.found').replace('{n}', String(slots.length))}</p>
      <ul class="slots">
        {#each slots as slot (slot.start)}
          <li>
            <button type="button" class="slot" on:click={() => dispatch('pick', { start: new Date(slot.start), end: new Date(slot.end) })}>
              <span>{formatWhen(new Date(slot.start), new Date(slot.end), false, timeFormat)}</span>
              {#if slot.afterDue}<span class="late">{$t('timeblock.find.afterDue')}</span>{/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</Modal>

<style>
  .length {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-bottom: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .slots {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .slot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .slot:hover,
  .slot:focus-visible {
    background: var(--selection-bg);
  }

  .late {
    color: var(--warning);
    font-size: var(--fz-meta);
  }

  .note {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }
</style>
