<script lang="ts">
  // one group of related settings: a heading, an optional line about it, and
  // the rows inside a hairline box.

  /** Heading of the group. */
  export let title: string = ''
  /** One or two sentences under the heading. */
  export let description: string = ''
  /** Drops the box, for a group whose content brings its own surface. */
  export let bare: boolean = false
</script>

<section class="group">
  {#if title}
    <h3>{title}</h3>
  {/if}
  {#if description}
    <p class="description">{description}</p>
  {/if}
  <div class="box" class:bare>
    <slot />
  </div>
</section>

<style>
  .group {
    margin-bottom: var(--space-6);
  }

  h3 {
    margin: 0 0 var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .description {
    margin: 0 0 var(--space-3);
    max-width: 62ch;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .box {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .box.bare {
    border: none;
    background: transparent;
  }

  /* rows are split by hairlines that stop short of the box edge. */
  .box:not(.bare) > :global(* + *) {
    border-top: var(--hairline) solid var(--border-subtle);
  }
</style>
