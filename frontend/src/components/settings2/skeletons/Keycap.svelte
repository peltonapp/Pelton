<script lang="ts">
  // one key of the keyboard, drawn as a small raised cap around its name.
</script>

<kbd><slot /></kbd>

<style>
  kbd {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: calc(var(--fz-label) + var(--space-3));
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    line-height: 1.5;
    color: var(--text-secondary);
  }
</style>
