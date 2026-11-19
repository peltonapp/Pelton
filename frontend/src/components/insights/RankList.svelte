<script lang="ts">
  // a ranked list with a thin meter under each entry, scaled to the largest.
  // An entry with an action renders as a button.
  import CountUp from './CountUp.svelte'

  /** One entry: its label, an optional second line, and its value. */
  export let items: { key: string; label: string; sub?: string; value: number; action?: () => void }[]
  /** Renders a value for display. */
  export let format: (n: number) => string = (n) => n.toLocaleString()
  /** Shown when there are no entries. */
  export let empty: string

  $: max = Math.max(1, ...items.map((i) => i.value))
</script>

{#if items.length === 0}
  <p class="empty">{empty}</p>
{:else}
  <ol class="ranks">
    {#each items as item (item.key)}
      <li>
        <svelte:element
          this={item.action ? 'button' : 'div'}
          type={item.action ? 'button' : undefined}
          class="entry"
          class:link={!!item.action}
          role={item.action ? undefined : 'group'}
          on:click={() => item.action?.()}
        >
          <span class="text">
            <span class="label">{item.label}</span>
            {#if item.sub}<span class="sub">{item.sub}</span>{/if}
          </span>
          <span class="value"><CountUp value={item.value} {format} /></span>
          <span class="meter" aria-hidden="true"><span style={`width:${(item.value / max) * 100}%`}></span></span>
        </svelte:element>
      </li>
    {/each}
  </ol>
{/if}

<style>
  .empty {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .ranks {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li + li {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .entry {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: var(--space-1) var(--space-3);
    width: 100%;
    padding: var(--space-2) var(--space-4);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
  }

  .entry.link {
    cursor: var(--cursor-action);
  }

  .entry.link:hover {
    background: var(--surface-hover);
  }

  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .label,
  .sub {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .value {
    align-self: center;
    color: var(--text-secondary);
  }

  .meter {
    grid-column: 1 / -1;
    height: var(--space-1);
    border-radius: var(--space-1);
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .meter span {
    display: block;
    height: 100%;
    background: color-mix(in srgb, var(--text-primary) 45%, transparent);
    transition: width var(--motion-slow) var(--ease-out);
  }
</style>
