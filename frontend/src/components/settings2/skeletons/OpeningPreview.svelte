<script lang="ts">
  // a list beside the reading pane, with the highlight on the second row after
  // a press of the down arrow. with open on arrow the pane follows it; without,
  // it still shows the first message. an unread dot stays on the row until
  // opening it marks it read.
  import Frame from './Frame.svelte'
  import { sampleMessages } from './samples'

  /** Marks a message read once it is open. */
  export let markRead: boolean = true
  /** Opens the highlighted message as the arrow keys move. */
  export let openOnArrow: boolean = true

  const rows = sampleMessages.slice(0, 3)

  $: opened = openOnArrow ? 1 : 0
  $: unread = (i: number) => i === 1 && !(markRead && opened === 1)
</script>

<Frame>
  <div class="split">
    <div class="list">
      {#each rows as m, i (m.email)}
        <div class="row" class:cursor={i === 1}>
          <span class="dot" class:on={unread(i)}></span>
          <span class="text">
            <span class="from" class:strong={unread(i)}>{m.name}</span>
            <span class="subject">{m.subject}</span>
          </span>
        </div>
      {/each}
    </div>
    <div class="read">
      <span class="title">{rows[opened].subject}</span>
      <span class="bar w90"></span>
      <span class="bar w80"></span>
      <span class="bar w50"></span>
    </div>
  </div>
</Frame>

<style>
  .split {
    display: grid;
    grid-template-columns: 1fr 1fr;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .list {
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row.cursor {
    background: var(--selection-bg);
  }

  .dot {
    width: 6px;
    height: 6px;
    flex-shrink: 0;
    border-radius: 50%;
  }

  .dot.on {
    background: var(--accent);
  }

  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: var(--fz-meta);
  }

  .from {
    color: var(--text-secondary);
  }

  .from.strong {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .subject {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
  }

  .read {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3);
    background: var(--surface-raised);
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .w50 { width: 50%; }
  .w80 { width: 80%; }
  .w90 { width: 90%; }
</style>
