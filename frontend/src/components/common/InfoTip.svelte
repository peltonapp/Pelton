<script lang="ts">
  // a small info glyph next to a field label. The browser's own title tooltip
  // takes about a second to appear, which is long enough that people give up
  // on it, so the bubble here is drawn by the app and shows in 150ms.
  import { onDestroy } from 'svelte'
  import { IconInfoCircle } from '@tabler/icons-svelte'

  /** The explanation the bubble shows. */
  export let text: string
  /** Which side of the glyph the bubble opens towards. */
  export let align: 'left' | 'right' = 'left'

  // hover and focus are handled here rather than in css: a :hover rule opens
  // the bubble for anything that lands inside the element's box, and inside a
  // label that box is easy to grow by accident.
  let open = false
  let timer: ReturnType<typeof setTimeout> | undefined

  function show(): void {
    clearTimeout(timer)
    timer = setTimeout(() => (open = true), 150)
  }

  function hide(): void {
    clearTimeout(timer)
    open = false
  }

  // a click toggles for good: the focus the click brings would otherwise arm
  // the hover timer and reopen a bubble that was just dismissed.
  function toggle(): void {
    clearTimeout(timer)
    open = !open
  }
  onDestroy(() => clearTimeout(timer))
</script>

<button
  type="button"
  class="tip {align}"
  aria-label={text}
  on:mouseenter={show}
  on:mouseleave={hide}
  on:focus={show}
  on:blur={hide}
  on:click|preventDefault|stopPropagation={toggle}
>
  <IconInfoCircle size={13} stroke={1.7} />
  {#if open}
    <span class="bubble" aria-hidden="true">{text}</span>
  {/if}
</button>

<style>
  /* an exact 13px box: any bigger and the hover target reaches into the rest
     of the label line, which reads as the bubble opening at random. */
  .tip {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 13px;
    height: 13px;
    padding: 0;
    margin-inline-start: var(--space-1);
    border: none;
    background: transparent;
    vertical-align: -2px;
    line-height: 0;
    color: var(--text-tertiary);
    cursor: help;
  }
  .tip:hover,
  .tip:focus-visible {
    color: var(--text-secondary);
  }

  .bubble {
    position: absolute;
    top: calc(100% + var(--space-1));
    z-index: 400;
    width: max-content;
    max-width: 260px;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-regular);
    line-height: 1.45;
    text-align: start;
    white-space: normal;
    pointer-events: none;
  }
  .left .bubble {
    left: 0;
  }
  .right .bubble {
    right: 0;
  }
</style>
