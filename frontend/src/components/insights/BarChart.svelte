<script lang="ts">
  // a bar chart of mail received and sent, drawn as plain svg: one stacked bar
  // per bucket, received below and sent on top. Hovering a bar shows its real
  // numbers; a screen reader gets the same numbers as a table.
  import { tip } from '../../lib/tip'

  /** One bar: what it covers, already worded, and its two counts. */
  export let bars: { label: string; received: number; sent: number }[]
  /** Accessible name of the chart. */
  export let label: string
  /** Names of the two series, for tooltips and the legend. */
  export let receivedLabel: string
  export let sentLabel: string

  const height = 100
  const step = 10
  const gap = 2

  $: max = Math.max(1, ...bars.map((b) => b.received + b.sent))
  $: width = Math.max(1, bars.length) * step

  function h(n: number): number {
    return (n / max) * height
  }

  function text(b: { label: string; received: number; sent: number }): string {
    return `${b.label}: ${b.received.toLocaleString()} ${receivedLabel}, ${b.sent.toLocaleString()} ${sentLabel}`
  }
</script>

<figure class="chart">
  <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" role="img" aria-label={label}>
    <line class="base" x1="0" y1={height} x2={width} y2={height} />
    {#each bars as b, i (i)}
      <g>
        {#if b.received > 0}
          <rect class="received" x={i * step + gap / 2} y={height - h(b.received)} width={step - gap} height={h(b.received)} />
        {/if}
        {#if b.sent > 0}
          <rect class="sent" x={i * step + gap / 2} y={height - h(b.received) - h(b.sent)} width={step - gap} height={h(b.sent)} />
        {/if}
        <rect class="hit" data-testid="bar" x={i * step} y="0" width={step} height={height} use:tip={{ text: text(b), side: 'top' }} />
      </g>
    {/each}
  </svg>
  <table class="sr">
    <caption>{label}</caption>
    <tbody>
      {#each bars as b, i (i)}
        <tr><th scope="row">{b.label}</th><td>{b.received} {receivedLabel}</td><td>{b.sent} {sentLabel}</td></tr>
      {/each}
    </tbody>
  </table>
  <figcaption class="legend">
    <span><i class="swatch received"></i>{receivedLabel}</span>
    <span><i class="swatch sent"></i>{sentLabel}</span>
  </figcaption>
</figure>

<style>
  .chart {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  svg {
    display: block;
    width: 100%;
    height: calc(var(--space-6) * 6);
  }

  .base {
    stroke: var(--border-default);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }

  .received {
    fill: color-mix(in srgb, var(--text-primary) 62%, transparent);
  }

  .sent {
    fill: color-mix(in srgb, var(--text-primary) 24%, transparent);
  }

  .hit {
    fill: transparent;
  }

  g:hover .received {
    fill: var(--text-primary);
  }

  g:hover .sent {
    fill: color-mix(in srgb, var(--text-primary) 45%, transparent);
  }

  .legend {
    display: flex;
    gap: var(--space-4);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .legend span {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .swatch {
    display: inline-block;
    width: var(--space-3);
    height: var(--space-3);
    border-radius: var(--space-1);
  }

  .swatch.received {
    background: color-mix(in srgb, var(--text-primary) 62%, transparent);
  }

  .swatch.sent {
    background: color-mix(in srgb, var(--text-primary) 24%, transparent);
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
