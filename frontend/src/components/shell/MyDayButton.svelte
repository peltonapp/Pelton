<script lang="ts">
  // the top bar's My Day button: shows or hides today's agenda and tasks
  // beside the mail, and says so by looking pressed while the panel is on.
  import sunIcon from '@phosphor-icons/core/assets/duotone/sun-horizon-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { prefs, setMyDayPanel } from '../../stores/prefs'
  import { shortcutTitle } from '../../stores/shortcuts'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
</script>

<button
  type="button"
  class="myday-btn"
  aria-pressed={$prefs.myDayPanel}
  aria-label={$t('myday.title')}
  use:tip={{ text: $shortcutTitle($t('myday.toggle'), 'toggle-my-day'), side: 'bottom' }}
  on:click={() => setMyDayPanel(!$prefs.myDayPanel)}
>
  <Ph svg={sunIcon} size={15} />
</button>

<style>
  .myday-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: calc(var(--control-height) - var(--space-2));
    min-width: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    --wails-draggable: no-drag;
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .myday-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .myday-btn[aria-pressed='true'] {
    background: var(--selection-bg);
    color: var(--accent);
  }

  .myday-btn:active {
    transform: scale(var(--press-scale));
  }

  :global(:root[data-reduce-motion]) .myday-btn {
    transition: none;
  }
</style>
