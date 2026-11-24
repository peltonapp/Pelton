<script lang="ts">
  // the box every settings picture sits in: a hairline border, the card radius
  // and a muted fill. `sunken` swaps the raised fill for the sunken one, for a
  // picture that is itself a raised surface on a backdrop.

  /** Uses the sunken fill instead of the raised one. */
  export let sunken: boolean = false
</script>

<div class="frame" class:sunken aria-hidden="true">
  <slot />
</div>

<style>
  .frame {
    position: relative;
    box-sizing: border-box;
    width: 100%;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .frame.sunken {
    background: var(--surface-sunken);
  }
</style>
