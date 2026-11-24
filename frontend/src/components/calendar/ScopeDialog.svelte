<script lang="ts">
  // asks which occurrences of a repeating event a change is for: this one,
  // this one and the ones after it, or the whole series.
  import { createEventDispatcher } from 'svelte'
  import '../../theme/controls.css'
  import Modal from '../common/Modal.svelte'
  import { t } from '../../lib/i18n'
  import type { EventScope } from '../../lib/types'

  /** What is being done, for the heading: an edit or a move. */
  export let title: string

  const dispatch = createEventDispatcher<{ close: void; choose: EventScope }>()

  let scope: EventScope = 'this'

  $: options = [
    { key: 'this' as const, label: $t('calendar.scope.this') },
    { key: 'future' as const, label: $t('calendar.scope.future') },
    { key: 'all' as const, label: $t('calendar.scope.all') },
  ]
</script>

<Modal {title} size="small" on:close>
  <fieldset class="choices">
    <legend>{$t('calendar.scope.question')}</legend>
    {#each options as o (o.key)}
      <label class="choice" class:on={scope === o.key}>
        <input type="radio" name="scope" value={o.key} bind:group={scope} />
        <span>{o.label}</span>
      </label>
    {/each}
  </fieldset>
  <svelte:fragment slot="footer">
    <div class="foot">
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('calendar.cancel')}</button>
      <button type="button" class="cal-btn primary" on:click={() => dispatch('choose', scope)}>{$t('calendar.ok')}</button>
    </div>
  </svelte:fragment>
</Modal>

<style>
  .choices {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    border: none;
  }

  legend {
    margin-bottom: var(--space-3);
    font-size: var(--fz-list);
    color: var(--text-secondary);
  }

  .choice {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-list);
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out);
  }

  .choice.on {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .choice input {
    accent-color: var(--accent);
    margin: 0;
  }

  .foot {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    width: 100%;
  }
</style>
