<script lang="ts">
  // one setting: its name and a line about it on the left, its control on the
  // right. `stacked` puts the control underneath, for controls too wide to sit
  // beside the text. a `preview` slot adds a button that shows a picture of
  // what the setting does.
  import PreviewTip from './PreviewTip.svelte'

  /** Name of the setting. */
  export let label: string
  /** One or two sentences about what it does. */
  export let hint: string = ''
  /** Anchor settings search scrolls to; unique within the page. */
  export let id: string = ''
  /** Puts the control under the text instead of beside it. */
  export let stacked: boolean = false
  /** Dims the row for a setting that does not apply right now. */
  export let disabled: boolean = false
</script>

<div class="row" class:stacked class:disabled data-setting={id || undefined}>
  <div class="text">
    <span class="label">
      {label}
      {#if $$slots.preview}
        <PreviewTip {label}>
          <slot name="preview" />
        </PreviewTip>
      {/if}
    </span>
    {#if hint}
      <span class="hint">{hint}</span>
    {/if}
  </div>
  <div class="control">
    <slot />
  </div>
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-5);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4);
  }

  .row.stacked {
    flex-direction: column;
    align-items: stretch;
    gap: var(--space-3);
  }

  .row.disabled {
    opacity: 0.5;
    pointer-events: none;
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .hint {
    max-width: 58ch;
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-tertiary);
  }

  .control {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-shrink: 0;
  }

  .stacked .control {
    flex-shrink: 1;
    min-width: 0;
  }

  /* settings search lands here and the row says so for a moment. */
  .row:global(.found) {
    animation: found var(--motion-slow) var(--ease-out) 3 alternate;
  }

  @keyframes found {
    to {
      background: var(--selection-bg);
    }
  }
</style>
