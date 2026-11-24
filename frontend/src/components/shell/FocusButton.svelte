<script lang="ts">
  // focus mode in the top bar: a quiet moon while it is off, the moon and the
  // time left while it is on, and a small menu under it to start focus for a
  // while, until a time, or until turned off, or to end it.
  import { onDestroy } from 'svelte'
  import moonIcon from '@phosphor-icons/core/assets/duotone/moon-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { focus, focusMenuOpen, beginFocus, finishFocus } from '../../stores/focus'
  import { prefs } from '../../stores/prefs'
  import { focusEnd, formatLeft, untilClock, type FocusLength } from '../../lib/focus'
  import { formatClockTime, type TimeFormat } from '../../lib/format'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'

  let button: HTMLButtonElement
  let clock = ''

  // a slow tick keeps the time left honest.
  let now = new Date()
  const timer = setInterval(() => (now = new Date()), 15000)
  onDestroy(() => clearInterval(timer))
  // a new end time is measured from now, not from the last tick.
  $: if ($focus) {
    now = new Date()
  }

  $: left = $focus.active ? formatLeft($focus.until, $t, now) : ''
  $: endsAt = $focus.active && $focus.until ? formatClockTime(new Date($focus.until), $prefs.timeFormat as TimeFormat) : ''
  $: status = !$focus.active
    ? $t('focus.title')
    : left
      ? $t('focus.indicator.left').replace('{left}', left)
      : $t('focus.indicator.on')
  $: stateLine = !$focus.active
    ? ''
    : endsAt
      ? $t($focus.scheduled ? 'focus.state.scheduled' : 'focus.state.until').replace('{time}', endsAt)
      : $t('focus.state.untilOff')
  $: lengths = [
    { length: '25m' as FocusLength, label: $t('focus.length.25m') },
    { length: '1h' as FocusLength, label: $t('focus.length.1h') },
    { length: 'off' as FocusLength, label: $t('focus.length.off') },
  ]

  function close(): void {
    focusMenuOpen.set(false)
  }

  function start(length: FocusLength): void {
    close()
    void beginFocus(focusEnd(length))
  }

  function startUntil(): void {
    const at = untilClock(clock)
    if (!at) {
      return
    }
    close()
    void beginFocus(at)
  }

  function stop(): void {
    close()
    void finishFocus()
  }
</script>

<button
  type="button"
  class="focus"
  class:on={$focus.active}
  class:open={$focusMenuOpen}
  bind:this={button}
  aria-haspopup="dialog"
  aria-expanded={$focusMenuOpen}
  aria-label={status}
  use:tip={{ text: $focusMenuOpen ? '' : status, side: 'bottom' }}
  on:click={() => focusMenuOpen.update((o) => !o)}
>
  <Ph svg={moonIcon} size={15} />
  {#if $focus.active}
    <span class="left">{left || $t('focus.indicator.short')}</span>
  {/if}
</button>

{#if $focusMenuOpen && button}
  <AnchoredPopover anchor={button} side="below" align="end" label={$t('focus.title')} on:close={close}>
    <div class="pop">
      <header>
        <span class="title">{$t('focus.title')}</span>
        {#if $focus.active}
          <span class="state">{stateLine}</span>
        {:else}
          <span class="about">{$t('focus.about')}</span>
        {/if}
      </header>

      {#if $focus.active}
        {#if $focus.held > 0}
          <p class="held">{$t('focus.held').replace('{n}', String($focus.held))}</p>
        {/if}
        <div class="actions">
          <button type="button" class="end" on:click={stop}>{$t('focus.end')}</button>
        </div>
      {:else}
        <ul role="menu" aria-label={$t('focus.title')}>
          {#each lengths as item (item.length)}
            <li role="none">
              <button type="button" role="menuitem" class="choice" on:click={() => start(item.length)}>{item.label}</button>
            </li>
          {/each}
        </ul>
        <form class="until" on:submit|preventDefault={startUntil}>
          <label for="focus-until">{$t('focus.length.until')}</label>
          <input id="focus-until" type="time" bind:value={clock} required />
          <button type="submit" class="start" disabled={!clock}>{$t('focus.start')}</button>
        </form>
      {/if}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .focus {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-2));
    min-width: calc(var(--control-height) - var(--space-2));
    justify-content: center;
    padding: 0 var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    cursor: var(--cursor-action);
    --wails-draggable: no-drag;
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .focus:hover,
  .focus.open {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .focus:active {
    transform: scale(var(--press-scale));
  }

  .focus.on {
    border: var(--hairline) solid var(--border-default);
    color: var(--text-secondary);
  }

  .left {
    animation: settle var(--motion-base) var(--ease-out);
  }

  @keyframes settle {
    from {
      opacity: 0;
    }
  }

  .pop {
    display: flex;
    flex-direction: column;
    width: var(--focus-menu-width);
    max-width: 100%;
  }

  header {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .about,
  .state {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  ul {
    margin: 0;
    padding: var(--space-2) 0;
    list-style: none;
  }

  .choice {
    display: flex;
    width: 100%;
    padding: var(--space-2) var(--space-4);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-family: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .choice:hover,
  .choice:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .until {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-label);
  }

  .until label {
    flex: 1;
    color: var(--text-secondary);
  }

  .until input {
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: inherit;
    font-size: var(--fz-label);
  }

  .held {
    margin: 0;
    padding: var(--space-3) var(--space-4) 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    padding: var(--space-3) var(--space-4);
  }

  .start,
  .end {
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .start {
    border-color: transparent;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .start:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent) 88%, var(--text-primary));
  }

  .end:hover {
    background: var(--surface-hover);
  }

  .start:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
