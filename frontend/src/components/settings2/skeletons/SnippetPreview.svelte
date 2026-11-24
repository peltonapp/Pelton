<script lang="ts">
  // a small picture of a snippet going in: ";thanks" typed in a line of the
  // composer, and the block it turns into with its placeholder lit.
  import { triggerPrefix } from '../../../lib/snippets'
</script>

<div class="stage" aria-hidden="true">
  <div class="line">
    <span class="bar w30"></span>
    <span class="typed">{triggerPrefix}thanks</span>
    <span class="key">Tab</span>
  </div>
  <div class="out">
    <span class="bar w70 strong"></span>
    <span class="row">
      <span class="bar w35"></span>
      <span class="ph"></span>
      <span class="bar w20"></span>
    </span>
    <span class="bar w55"></span>
  </div>
</div>

<style>
  .stage {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 220px;
    padding: 10px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
  }

  .line {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-bottom: 8px;
    border-bottom: var(--hairline) dashed var(--border-subtle);
  }

  .typed {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-primary);
  }

  .key {
    padding: 0 4px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .out,
  .row {
    display: flex;
    gap: 5px;
  }

  .out {
    flex-direction: column;
  }

  .row {
    align-items: center;
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

  .ph {
    display: block;
    width: 22%;
    height: 7px;
    border-radius: 2px;
    background: var(--selection-bg-strong);
    box-shadow: inset 0 0 0 var(--hairline) var(--accent);
  }

  .w20 { width: 20%; }
  .w30 { width: 30%; }
  .w35 { width: 35%; }
  .w55 { width: 55%; }
  .w70 { width: 70%; }
</style>
