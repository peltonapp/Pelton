<script lang="ts">
  // the composer's primary action: Send, with a caret that opens the
  // send-later menu. while the message is being queued the label gives way to
  // a pie loader, and once it is queued a check draws itself before the
  // composer closes. the button keeps one width throughout so nothing jumps.
  import { createEventDispatcher } from 'svelte'
  import sendIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import caretIcon from '@phosphor-icons/core/assets/duotone/caret-down-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import eveningIcon from '@phosphor-icons/core/assets/duotone/sun-horizon-duotone.svg?raw'
  import morningIcon from '@phosphor-icons/core/assets/duotone/sun-duotone.svg?raw'
  import weekIcon from '@phosphor-icons/core/assets/duotone/calendar-dots-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import PieProgress from '../../common/PieProgress.svelte'
  import AnchoredPopover from '../../common/AnchoredPopover.svelte'
  import DateTimePicker from '../../common/DateTimePicker.svelte'
  import { buildSendPresets, formatSendWhen, type SendPreset } from '../../../lib/composeactions'
  import { shortcutTitle } from '../../../stores/shortcuts'
  import { tip } from '../../../lib/tip'
  import { t } from '../../../lib/i18n'
  import { toastError } from '../../../stores/toast'

  /** Where the send is: waiting for a click, being queued, or queued. */
  export let state: 'idle' | 'sending' | 'sent' = 'idle'
  /** Unique id base for the custom time field. */
  export let id: string
  /** Opens the send-later menu above the button rather than below. */
  export let menuAbove = false

  const dispatch = createEventDispatcher<{ send: void; schedule: Date }>()

  const icons: Record<SendPreset['key'], string> = {
    laterToday: clockIcon,
    thisEvening: eveningIcon,
    tomorrowMorning: morningIcon,
    mondayMorning: weekIcon,
  }

  let caret: HTMLButtonElement
  let menuOpen = false
  let custom = ''

  $: busy = state !== 'idle'
  $: presets = menuOpen ? buildSendPresets(new Date(), $t) : []

  function toggleMenu(): void {
    if (busy) {
      return
    }
    menuOpen = !menuOpen
    custom = ''
  }

  function schedule(when: Date): void {
    if (when.getTime() <= Date.now()) {
      toastError($t('compose.sendLater.pickFutureTime'))
      return
    }
    menuOpen = false
    dispatch('schedule', when)
  }
</script>

<div class="split" class:busy>
  <button
    type="button"
    class="send"
    class:sending={state === 'sending'}
    class:sent={state === 'sent'}
    disabled={busy}
    aria-label={state === 'sending' ? $t('compose.action.sending') : $t('action.send')}
    title={$shortcutTitle($t('action.send'), 'send')}
    on:click={() => dispatch('send')}
  >
    {#if state === 'sending'}
      <span class="face"><PieProgress size={16} label={$t('compose.action.sending')} /></span>
    {:else if state === 'sent'}
      <span class="face">
        <svg class="check" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
          <path d="M5 12.5l4.5 4.5L19 7.5" />
        </svg>
      </span>
    {:else}
      <span class="face idle"><Ph svg={sendIcon} size={16} /><span>{$t('action.send')}</span></span>
    {/if}
  </button>
  <button
    type="button"
    class="caret"
    bind:this={caret}
    disabled={busy}
    aria-label={$t('compose.sendLater.ariaLabel')}
    aria-haspopup="menu"
    aria-expanded={menuOpen}
    use:tip={{ text: $t('compose.sendLater.ariaLabel'), side: menuAbove ? 'top' : 'bottom' }}
    on:click={toggleMenu}
  >
    <Ph svg={caretIcon} size={12} />
  </button>
</div>

{#if menuOpen}
  <AnchoredPopover anchor={caret} side={menuAbove ? 'above' : 'below'} align="start" role="menu" label={$t('compose.sendLater.ariaLabel')} on:close={() => (menuOpen = false)}>
    <div class="menu">
      {#each presets as preset (preset.key)}
        <button type="button" class="preset" role="menuitem" on:click={() => schedule(preset.when)}>
          <Ph svg={icons[preset.key]} size={16} />
          <span class="label">{preset.label}</span>
          <span class="when">{formatSendWhen(preset.when)}</span>
        </button>
      {/each}
      <div class="custom">
        <label for={`${id}-send-at`}>{$t('compose.sendLater.customLabel')}</label>
        <DateTimePicker
          id={`${id}-send-at`}
          mode="datetime"
          bind:value={custom}
          confirmLabel={$t('compose.sendLater.schedule')}
          on:confirm={() => custom && schedule(new Date(custom))}
        />
      </div>
    </div>
  </AnchoredPopover>
{/if}

<style>
  .split {
    display: inline-flex;
    flex-shrink: 0;
    border-radius: var(--radius-control);
    --wails-draggable: no-drag;
  }

  .send,
  .caret {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: var(--control-height);
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    cursor: var(--cursor-action);
    transition:
      filter var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .send {
    min-width: var(--compose-send-width);
    padding: 0 var(--space-4);
    border-start-start-radius: var(--radius-control);
    border-end-start-radius: var(--radius-control);
  }

  .caret {
    width: calc(var(--control-height) - var(--space-3));
    border-inline-start: var(--hairline) solid color-mix(in srgb, var(--accent-fg) 30%, transparent);
    border-start-end-radius: var(--radius-control);
    border-end-end-radius: var(--radius-control);
  }

  .send:hover:not(:disabled),
  .caret:hover:not(:disabled) {
    filter: brightness(1.08);
  }

  .send:active:not(:disabled),
  .caret:active:not(:disabled) {
    transform: scale(var(--press-scale));
  }

  .send:focus-visible,
  .caret:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .split.busy .send,
  .split.busy .caret {
    cursor: default;
  }

  .split.busy .caret {
    opacity: 0.7;
  }

  .face {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    animation: face-in var(--motion-base) var(--ease-out);
  }

  @keyframes face-in {
    from {
      opacity: 0;
      transform: scale(0.85);
    }
  }

  .check path {
    fill: none;
    stroke: currentColor;
    stroke-width: 2.6;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 24;
    stroke-dashoffset: 24;
    animation: draw var(--motion-slow) var(--ease-out) forwards;
  }

  @keyframes draw {
    to {
      stroke-dashoffset: 0;
    }
  }

  .menu {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: calc(var(--popover-width) - var(--space-6));
    padding: var(--space-2);
    overflow-y: auto;
  }

  .preset {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .preset :global(.ph) {
    color: var(--text-secondary);
  }

  .preset:hover,
  .preset:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .label {
    flex: 1;
  }

  .when {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .custom {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-1);
    padding: var(--space-3) var(--space-3) var(--space-2);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .custom label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }
</style>
