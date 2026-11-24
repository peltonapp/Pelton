<script lang="ts">
  // a conversation of three messages in the list. grouped, it is one row with a
  // count and its replies beneath; ungrouped, every message is a row of its own.
  import Frame from './Frame.svelte'
  import { sampleMessages } from './samples'

  /** Groups the conversation into one row. */
  export let threaded: boolean = true

  const [first, second, third] = sampleMessages
  const subject = first.subject
  const replies = [second, first]
</script>

<Frame>
  <div class="list">
    {#if threaded}
      <div class="row head">
        <span class="twisty">&#9662;</span>
        <span class="from strong">{first.name}</span>
        <span class="count">3</span>
        <span class="subject">{subject}</span>
      </div>
      {#each replies as r, i (i)}
        <div class="row reply">
          <span class="from">{r.name}</span>
          <span class="bar"></span>
        </div>
      {/each}
    {:else}
      {#each [first, second, first] as m, i (i)}
        <div class="row">
          <span class="from" class:strong={i === 0}>{m.name}</span>
          <span class="subject">{subject}</span>
        </div>
      {/each}
    {/if}
    <div class="row">
      <span class="from">{third.name}</span>
      <span class="subject">{third.subject}</span>
    </div>
  </div>
</Frame>

<style>
  .list {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    font-size: var(--fz-label);
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row.head {
    background: var(--selection-bg);
  }

  .row.reply {
    padding-inline-start: calc(var(--space-3) + var(--thread-twisty));
    background: var(--surface-sunken);
  }

  .twisty {
    width: var(--space-4);
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .from {
    flex-shrink: 0;
    color: var(--text-secondary);
  }

  .from.strong {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .count {
    padding: 0 var(--space-2);
    border-radius: 999px;
    background: var(--surface-base);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .subject {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }

  .bar {
    width: 40%;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }
</style>
