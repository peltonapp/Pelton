<script lang="ts">
  // the button of the setup flows (onboarding, add mailbox, import): a filled
  // accent primary, a hairline secondary and a quiet text button. it presses in
  // slightly while held, and reads the same in the new and the classic design
  // since it is drawn from tokens alone.

  /** primary for the one action that moves on, secondary beside it, quiet for links. */
  export let variant: 'primary' | 'secondary' | 'quiet' = 'secondary'
  export let type: 'button' | 'submit' = 'button'
  export let disabled = false
  /** Accessible name when the visible content is not text. */
  export let label: string | undefined = undefined
</script>

<button {type} class="btn {variant}" {disabled} aria-label={label} on:click>
  <slot />
</button>

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    min-height: var(--control-height);
    padding: 0 var(--space-5);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    font-family: var(--font-ui);
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    line-height: 1.2;
    text-align: center;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .btn:active:not(:disabled) {
    transform: scale(0.97);
  }

  .btn:disabled {
    opacity: 0.45;
    cursor: default;
  }

  .primary {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .primary:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent) 90%, var(--text-primary));
  }

  .secondary {
    background: var(--surface-raised);
    border-color: var(--border-default);
    color: var(--text-primary);
  }

  .secondary:hover:not(:disabled) {
    border-color: var(--border-strong);
    background: var(--surface-hover);
  }

  .quiet {
    padding: 0 var(--space-3);
    background: transparent;
    color: var(--text-secondary);
  }

  .quiet:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
