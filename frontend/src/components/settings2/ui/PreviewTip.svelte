<script lang="ts">
  // a large bubble with a picture of what a setting does. the row that owns it
  // opens it while the pointer rests on the row and passes the pointer's place,
  // so the answer to "what does this change" needs no extra button.
  import { tick } from 'svelte'
  import { portal } from '../../../lib/portal'

  /** Whether the bubble shows. */
  export let open: boolean = false
  /** Pointer position in client pixels the bubble sits next to. */
  export let x: number = 0
  export let y: number = 0

  const offset = 16
  const edge = 8

  let bubble: HTMLDivElement
  let left = 0
  let top = 0

  $: if (open) {
    void place(x, y)
  }

  async function place(px: number, py: number): Promise<void> {
    await tick()
    if (!bubble) {
      return
    }
    // the root carries a css zoom for the interface scale; rects are in zoomed
    // pixels and fixed offsets are not.
    const scale = Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
    const own = bubble.getBoundingClientRect()
    let nx = px + offset
    if (nx + own.width > window.innerWidth - edge) {
      nx = px - own.width - offset
    }
    let ny = py + offset
    if (ny + own.height > window.innerHeight - edge) {
      ny = py - own.height - offset
    }
    left = Math.max(edge, nx) / scale
    top = Math.max(edge, ny) / scale
  }
</script>

{#if open}
  <div class="bubble" use:portal bind:this={bubble} style={`left:${left}px;top:${top}px`} role="tooltip">
    <slot />
  </div>
{/if}

<style>
  .bubble {
    position: fixed;
    z-index: 800;
    width: 300px;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    pointer-events: none;
    animation: rise var(--motion-base) var(--ease-out);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(3px) scale(0.985);
    }
  }
</style>
