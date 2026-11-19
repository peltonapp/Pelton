<script lang="ts">
  // the classic design has no spaces, so it shows the Automations space as a
  // full window overlay with a close button, the way it shows contacts.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import { IconX } from '@tabler/icons-svelte'
  import AutomationsSpace from './AutomationsSpace.svelte'
  import { t } from '../../lib/i18n'
  import { popupIsOpen } from '../../lib/popups'
  import '../../theme/controls.css'

  const dispatch = createEventDispatcher<{ close: void }>()

  function onKey(e: KeyboardEvent): void {
    if (e.key === 'Escape' && !popupIsOpen() && !document.querySelector('[role="dialog"]')) {
      dispatch('close')
    }
  }

  onMount(() => window.addEventListener('keydown', onKey))
  onDestroy(() => window.removeEventListener('keydown', onKey))
</script>

<div class="overlay">
  <header class="head">
    <h1 class="title">{$t('space.automations')}</h1>
    <button type="button" class="cal-icon" aria-label={$t('modal.close')} on:click={() => dispatch('close')}>
      <IconX size={16} />
    </button>
  </header>
  <div class="host">
    <AutomationsSpace />
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
    color: var(--text-primary);
    font-family: var(--font-ui);
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .host {
    position: relative;
    flex: 1;
    min-height: 0;
  }
</style>
