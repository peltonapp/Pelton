<script lang="ts">
  // one of the question's options, as a list of radio buttons; a double click
  // answers at once.
  import { createEventDispatcher } from 'svelte'
  import { t } from '../../../lib/i18n'
  import type { ScriptAsk } from '../../../lib/scripting'

  /** The question. */
  export let ask: ScriptAsk
  /** The picked option. */
  export let value: unknown = undefined

  const dispatch = createEventDispatcher<{ answer: unknown }>()
  const name = `ask-${Math.random().toString(36).slice(2, 9)}`

  $: options = Array.isArray(ask.params.options) ? (ask.params.options as unknown[]).map(String) : []
</script>

<div class="choices" role="radiogroup" aria-label={$t('scripts.ask.answer')}>
  {#each options as o, i (o + i)}
    <label class="choice" class:on={value === o}>
      <input type="radio" {name} value={o} checked={value === o} on:change={() => (value = o)} on:dblclick={() => dispatch('answer', o)} />
      <span>{o}</span>
    </label>
  {/each}
</div>

<style>
  .choices {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    max-height: calc(var(--control-height) * 8);
    overflow-y: auto;
  }

  .choice {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-height);
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .choice:hover {
    background: var(--surface-hover);
  }

  .choice.on {
    background: var(--selection-bg);
  }

  .choice input {
    accent-color: var(--accent);
  }
</style>
