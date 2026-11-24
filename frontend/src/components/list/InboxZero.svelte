<script lang="ts">
  // the empty inbox of the new design: a thin ring drawn closed and one line
  // under it. it stays quiet on purpose, an empty inbox is the normal state
  // after a run of done, not an event.
  import EmptyState from '../common/EmptyState.svelte'
  import { t } from '../../lib/i18n'
</script>

<div class="zero">
  <EmptyState title={$t('inboxZero.line')}>
    <svg class="mark" viewBox="0 0 48 48" aria-hidden="true">
      <circle class="track" cx="24" cy="24" r="20" />
      <circle class="ring" cx="24" cy="24" r="20" pathLength="100" />
      <path class="tick" d="M16 24.5 l5.5 5.5 L32 19" pathLength="100" />
    </svg>
  </EmptyState>
</div>

<style>
  .zero {
    display: flex;
    flex-direction: column;
    height: 100%;
    animation: settle var(--motion-slow) var(--ease-out);
  }

  .zero :global(.icon) {
    opacity: 1;
  }

  .mark {
    width: var(--inbox-zero-mark);
    height: var(--inbox-zero-mark);
    fill: none;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .track {
    stroke: var(--border-subtle);
    stroke-width: 1.5;
  }

  .ring {
    stroke: var(--border-strong);
    stroke-width: 1.5;
    stroke-dasharray: 100;
    stroke-dashoffset: 0;
    transform: rotate(-90deg);
    transform-origin: center;
    animation: draw var(--motion-slow) var(--ease-out) both;
  }

  .tick {
    stroke: var(--text-secondary);
    stroke-width: 1.75;
    stroke-dasharray: 100;
    animation: draw var(--motion-slow) var(--ease-out) var(--motion-fast) both;
  }

  @keyframes draw {
    from {
      stroke-dashoffset: 100;
    }
  }

  @keyframes settle {
    from {
      opacity: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .zero,
    .ring,
    .tick {
      animation: none;
    }
  }

  :global(:root[data-reduce-motion]) .zero,
  :global(:root[data-reduce-motion]) .ring,
  :global(:root[data-reduce-motion]) .tick {
    animation: none;
  }
</style>
