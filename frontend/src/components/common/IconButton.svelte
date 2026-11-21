<script lang="ts">
  // a square icon-only button. label is required and becomes the aria-label so
  // icon-only controls stay accessible. the icon goes in the default slot.
  export let label: string
  export let title: string = label
  export let active: boolean = false
  // toggle marks a button that stays pressed (flag), which is the only kind
  // assistive tech should hear a pressed state for.
  export let toggle: boolean = false
  export let danger: boolean = false
  export let disabled: boolean = false
</script>

<button
  type="button"
  class="icon-btn"
  class:active
  class:danger
  aria-label={label}
  aria-pressed={toggle ? active : undefined}
  {title}
  {disabled}
  on:click
>
  <slot />
</button>

<style>
  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    padding: 0;
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .icon-btn:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon-btn.active {
    color: var(--accent);
    background: var(--selection-bg);
  }

  .icon-btn.danger:hover:not(:disabled) {
    color: var(--danger);
    background: var(--danger-bg);
  }

  .icon-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
</style>
