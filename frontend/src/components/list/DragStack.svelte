<script lang="ts">
  // what follows the pointer while mail is being dragged: the messages as a
  // small stack of cards, each lying at its own slight angle, with a count on
  // top when there is more than one. over a place that takes them the stack
  // straightens and picks up the accent.
  import { mailDrag } from '../../stores/maildrag'
  import { displayName } from '../../lib/format'
  import { t } from '../../lib/i18n'

  // angle of each card in the stack, top card first.
  const tilts = [-5, 4, -2, 7]
  const maxCards = tilts.length

  $: drag = $mailDrag
  $: cards = drag ? drag.items.slice(0, maxCards) : []
  // the root carries a css zoom for the interface scale; pointer positions are
  // in zoomed pixels and fixed offsets are not.
  $: scale = drag ? Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1 : 1
</script>

{#if drag}
  <div
    class="stack"
    class:over={drag.over}
    style={`left:${drag.x / scale}px;top:${drag.y / scale}px`}
    aria-hidden="true"
  >
    {#each cards as item, i (item.id)}
      <div class="card" style={`--tilt:${tilts[i]}deg;--i:${i};z-index:${maxCards - i}`}>
        {#if i === 0}
          <span class="sender">{displayName(item.fromName, item.fromAddress)}</span>
          <span class="subject">{item.subject || $t('messageList.noSubject')}</span>
        {/if}
      </div>
    {/each}
    {#if drag.items.length > 1}
      <span class="count">{drag.items.length}</span>
    {/if}
    {#if drag.over && drag.hint}
      <span class="hint">{drag.hint}</span>
    {/if}
  </div>
{/if}

<style>
  .stack {
    position: fixed;
    z-index: 950;
    width: 220px;
    height: 46px;
    margin: 10px 0 0 12px;
    pointer-events: none;
  }

  .card {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--space-1);
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    transform: translateY(calc(var(--i) * 3px)) rotate(var(--tilt));
    transition:
      transform var(--motion-base) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
    animation: lift var(--motion-base) var(--ease-out);
  }

  /* picked up: each card leaves the row flat and turns to its own angle. */
  @keyframes lift {
    from {
      transform: scale(0.94) rotate(0deg);
      opacity: 0.4;
    }
  }

  /* over a place that takes the mail the stack squares up, ready to go in. */
  .stack.over .card {
    transform: translateY(calc(var(--i) * 2px)) rotate(calc(var(--tilt) * 0.25));
    border-color: var(--accent);
  }

  .sender,
  .subject {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sender {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .subject {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  /* what a drop here does, under the stack: one line, plus how Shift changes
     it, updated as the key goes down and up. */
  .hint {
    position: absolute;
    top: calc(100% + var(--space-3));
    inset-inline-start: 0;
    max-width: calc(var(--cal-popover-width) * 1.2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    border: var(--hairline) solid var(--border-default);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fz-meta);
    line-height: 1.4;
    color: var(--text-secondary);
    white-space: pre-line;
    animation: hint-in var(--motion-fast) var(--ease-out);
  }

  @keyframes hint-in {
    from {
      opacity: 0;
    }
  }

  .count {
    position: absolute;
    top: -8px;
    inset-inline-end: -8px;
    z-index: 10;
    min-width: 20px;
    padding: 0 var(--space-2);
    border-radius: 999px;
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    line-height: 20px;
    text-align: center;
    font-variant-numeric: tabular-nums;
  }
</style>
