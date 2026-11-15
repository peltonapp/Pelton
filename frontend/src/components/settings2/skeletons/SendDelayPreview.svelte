<script lang="ts">
  // the toast that follows pressing Send. with an undo window it carries an
  // Undo button and a bar counting the window down; with the window off the
  // message is simply gone.
  import Frame from './Frame.svelte'
  import { t } from '../../../lib/i18n'

  /** Length of the undo window in seconds; 0 is off. */
  export let seconds: number = 10

  $: held = seconds > 0
</script>

<Frame sunken>
  <div class="toast">
    <div class="row">
      <span class="text">{held ? $t('compose.toast.queued') : $t('outbox.toast.sent')}</span>
      {#if held}
        <span class="secs">{$t('preview.seconds').replace('{n}', String(seconds))}</span>
        <span class="undo">{$t('menu.undo')}</span>
      {/if}
    </div>
    {#if held}
      <span class="track"><span class="drain"></span></span>
    {/if}
  </div>
</Frame>

<style>
  .toast {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fz-label);
  }

  .text {
    flex: 1;
    min-width: 0;
    color: var(--text-primary);
  }

  .secs {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .undo {
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .track {
    height: 2px;
    border-radius: 1px;
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .drain {
    display: block;
    height: 100%;
    background: var(--accent);
    transform-origin: left;
    animation: drain calc(var(--motion-slow) * 12) linear infinite;
  }

  :global(:root[dir='rtl']) .drain {
    transform-origin: right;
  }

  @keyframes drain {
    from {
      transform: scaleX(1);
    }
    to {
      transform: scaleX(0);
    }
  }
</style>
