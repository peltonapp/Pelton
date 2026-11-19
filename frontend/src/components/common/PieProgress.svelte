<script lang="ts">
  // a progress indicator for places too small for a bar: a ring whose inside
  // fills clockwise from twelve o'clock as the work advances. with no value it
  // shows a fixed wedge that turns, for work whose length is unknown.

  /** Progress from 0 to 1, or null when the length of the work is unknown. */
  export let value: number | null = null
  /** Edge length in px. */
  export let size: number = 14
  /** Accessible name of what is in progress. */
  export let label: string = ''

  const radius = 5
  const circumference = 2 * Math.PI * radius

  $: clamped = value === null ? 0.3 : Math.max(0, Math.min(1, value))
  $: dash = `${clamped * circumference} ${circumference}`
</script>

<svg
  class="pie"
  class:spin={value === null}
  width={size}
  height={size}
  viewBox="0 0 24 24"
  role="progressbar"
  aria-label={label}
  aria-valuemin={0}
  aria-valuemax={100}
  aria-valuenow={value === null ? undefined : Math.round(clamped * 100)}
>
  <circle class="ring" cx="12" cy="12" r="10" />
  <circle class="wedge" cx="12" cy="12" r={radius} stroke-dasharray={dash} />
</svg>

<style>
  .pie {
    flex-shrink: 0;
    color: inherit;
  }

  .ring {
    fill: none;
    stroke: currentColor;
    stroke-width: 2.5;
  }

  /* a stroke as wide as the circle's diameter paints a filled wedge, and the
     dash length is then the share of the circle that is filled. */
  .wedge {
    fill: none;
    stroke: currentColor;
    stroke-width: 10;
    transform: rotate(-90deg);
    transform-origin: 50% 50%;
    transition: stroke-dasharray var(--motion-base) var(--ease-out);
  }

  .spin {
    transform-box: border-box;
    transform-origin: 50% 50%;
    animation: turn 0.9s linear infinite;
  }

  @keyframes turn {
    to {
      transform: rotate(360deg);
    }
  }
</style>
