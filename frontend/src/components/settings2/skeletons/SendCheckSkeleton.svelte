<script lang="ts">
  // a small picture of the composer stopping a send: the header lines, the
  // bar that asks, and the text under it. for a missing attachment the line
  // that mentions one is lit; for an empty subject the subject line is empty.

  /** Which question the picture shows. */
  export let issue: 'missingAttachment' | 'emptySubject' = 'missingAttachment'
</script>

<div class="stage" aria-hidden="true">
  <div class="field"><span class="label"></span><span class="bar w45"></span></div>
  <div class="field">
    <span class="label"></span>
    {#if issue === 'emptySubject'}
      <span class="empty"></span>
    {:else}
      <span class="bar w60 strong"></span>
    {/if}
  </div>
  <div class="ask">
    <span class="dot"></span>
    <span class="bar w40"></span>
    <span class="button"></span>
    <span class="button"></span>
  </div>
  <div class="text">
    <span class="bar w80"></span>
    <span class="bar w65" class:hit={issue === 'missingAttachment'}></span>
    <span class="bar w50"></span>
  </div>
</div>

<style>
  .stage {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 220px;
    padding: 10px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
  }

  .field {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 5px;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .label {
    width: 16%;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-default);
  }

  .empty {
    width: 30%;
    height: 7px;
    border: var(--hairline) dashed var(--warning);
    border-radius: 2px;
  }

  .ask {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 6px;
    border: var(--hairline) solid var(--warning);
    border-inline-start-width: 3px;
    border-radius: var(--radius-control);
    background: var(--warning-bg);
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--warning);
  }

  .button {
    width: 14%;
    height: 8px;
    border: var(--hairline) solid var(--border-default);
    border-radius: 2px;
    background: var(--surface-raised);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding-top: 2px;
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .bar.strong {
    height: 4px;
    background: var(--text-tertiary);
  }

  .bar.hit {
    background: var(--warning);
  }

  .ask .bar {
    flex: 1;
  }

  .w40 { width: 40%; }
  .w45 { width: 45%; }
  .w50 { width: 50%; }
  .w60 { width: 60%; }
  .w65 { width: 65%; }
  .w80 { width: 80%; }
</style>
