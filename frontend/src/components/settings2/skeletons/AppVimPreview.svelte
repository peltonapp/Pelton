<script lang="ts">
  // a message list moved with the letter keys. enabled, the highlight steps
  // down the rows as j is pressed; off, the keys do nothing and it stays put.
  import Frame from './Frame.svelte'
  import Keycap from './Keycap.svelte'
  import { sampleMessages } from './samples'

  /** In-app vim keys are on. */
  export let enabled: boolean = true
</script>

<Frame>
  <div class="stage">
    <div class="list" class:enabled>
      <span class="cursor"></span>
      {#each sampleMessages as m (m.email)}
        <div class="row">
          <span class="from">{m.name}</span>
          <span class="subject">{m.subject}</span>
        </div>
      {/each}
    </div>
    <div class="keys" class:off={!enabled}>
      <Keycap>j</Keycap>
      <Keycap>k</Keycap>
    </div>
  </div>
</Frame>

<style>
  .stage {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }

  .list {
    --row: calc(var(--fz-label) + var(--space-4));
    position: relative;
    flex: 1;
    min-width: 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .cursor {
    position: absolute;
    inset-inline: 0;
    top: 0;
    height: var(--row);
    background: var(--selection-bg);
  }

  .enabled .cursor {
    animation: step calc(var(--motion-slow) * 12) steps(1) infinite;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    height: var(--row);
    padding: 0 var(--space-3);
    font-size: var(--fz-label);
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .from {
    flex-shrink: 0;
    color: var(--text-primary);
  }

  .subject {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
  }

  .keys {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .keys.off {
    opacity: 0.4;
  }

  @keyframes step {
    33% {
      top: var(--row);
    }
    66% {
      top: calc(2 * var(--row));
    }
  }
</style>
