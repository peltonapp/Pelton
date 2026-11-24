<script lang="ts">
  // a picture of one theme mode. light and dark pin their own token sets;
  // system and schedule show the two halves, schedule with a clock on top.
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import MiniWindow from './MiniWindow.svelte'

  /** Which mode to draw. */
  export let mode: 'light' | 'dark' | 'system' | 'schedule' = 'light'
  /** Height of the picture in px. */
  export let height: number = 84

  $: split = mode === 'system' || mode === 'schedule'
</script>

<div class="frame" style={`height:${height}px`} aria-hidden="true">
  {#if split}
    <div class="half theme-light"><MiniWindow /></div>
    <div class="half end theme-dark"><MiniWindow /></div>
  {:else}
    <div class="whole {mode === 'dark' ? 'theme-dark' : 'theme-light'}"><MiniWindow /></div>
  {/if}
  {#if mode === 'schedule'}
    <span class="clock"><Ph svg={clockIcon} size={18} /></span>
  {/if}
</div>

<style>
  .frame {
    position: relative;
    display: flex;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    overflow: hidden;
  }

  .whole {
    flex: 1;
  }

  /* each half shows the same full-width window, cut at the middle. */
  .half {
    position: relative;
    flex: 1;
    overflow: hidden;
  }

  .half > :global(.window) {
    position: absolute;
    inset-block: 0;
    inset-inline-start: 0;
    width: 200%;
  }

  .half.end > :global(.window) {
    inset-inline-start: -100%;
  }

  .clock {
    position: absolute;
    inset-block-start: 50%;
    inset-inline-start: 50%;
    display: inline-flex;
    padding: 3px;
    border: var(--hairline) solid var(--border-strong);
    border-radius: 50%;
    background: var(--surface-raised);
    color: var(--text-primary);
    transform: translate(-50%, -50%);
  }

  :global([dir="rtl"]) .clock {
    transform: translate(50%, -50%);
  }
</style>
