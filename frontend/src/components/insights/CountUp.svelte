<script lang="ts">
  // a number that counts up to its value once, when it first appears. With
  // reduced motion, or in the classic design, it shows the value at once.
  // Later changes show the new value straight away rather than counting again.
  import { onDestroy, onMount } from 'svelte'
  import { dur, motion } from '../../lib/motion'

  /** The number to show. */
  export let value: number
  /** Renders the number for display; the default is a locale integer. */
  export let format: (n: number) => string = (n) => Math.round(n).toLocaleString()

  let shown = 0
  let started = false
  let frame = 0

  onMount(() => {
    const ms = dur(motion.slow * 3)
    if (ms === 0 || typeof requestAnimationFrame !== 'function') {
      shown = value
      started = true
      return
    }
    const begin = performance.now()
    const step = (now: number): void => {
      const p = Math.min(1, (now - begin) / ms)
      shown = value * (1 - Math.pow(1 - p, 3))
      if (p < 1) {
        frame = requestAnimationFrame(step)
      } else {
        started = true
      }
    }
    frame = requestAnimationFrame(step)
  })

  $: if (started) {
    shown = value
  }

  onDestroy(() => cancelAnimationFrame(frame))
</script>

<span class="count"><span aria-hidden="true">{format(shown)}</span><span class="sr">{format(value)}</span></span>

<style>
  .count {
    font-variant-numeric: tabular-nums;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
