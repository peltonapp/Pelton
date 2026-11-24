<script lang="ts">
  // a line of text, or several for the textarea control.
  import { createEventDispatcher, onMount } from 'svelte'
  import { t } from '../../../lib/i18n'
  import type { ScriptAsk } from '../../../lib/scripting'
  import '../../../theme/controls.css'

  /** The question. */
  export let ask: ScriptAsk
  /** The answer so far. */
  export let value: unknown = ''

  const dispatch = createEventDispatcher<{ answer: unknown }>()
  let el: HTMLInputElement | HTMLTextAreaElement

  $: text = typeof value === 'string' ? value : value == null ? '' : String(value)

  onMount(() => el?.focus())

  function onKey(e: KeyboardEvent): void {
    if (e.key === 'Enter' && (ask.control !== 'textarea' || e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      dispatch('answer', text)
    }
  }
</script>

{#if ask.control === 'textarea'}
  <textarea bind:this={el} class="cal-field area" rows="6" aria-label={$t('scripts.ask.answer')} value={text} on:input={(e) => (value = e.currentTarget.value)} on:keydown={onKey}></textarea>
{:else}
  <input bind:this={el} class="cal-field" type="text" aria-label={$t('scripts.ask.answer')} value={text} on:input={(e) => (value = e.currentTarget.value)} on:keydown={onKey} />
{/if}

<style>
  .area {
    resize: vertical;
    font-family: var(--font-ui);
  }

  input,
  textarea {
    width: 100%;
  }
</style>
