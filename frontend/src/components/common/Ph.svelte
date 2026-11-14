<script lang="ts">
  // one Phosphor icon, drawn from the raw svg text of @phosphor-icons/core. the
  // svg paints with currentColor, so the icon takes the text color around it.

  /** Raw svg markup, imported with ?raw from @phosphor-icons/core. */
  export let svg: string
  /** Edge length in px. */
  export let size: number = 16
</script>

<span class="ph" style={`width:${size}px;height:${size}px`} aria-hidden="true">
  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
  {@html svg}
</span>

<style>
  .ph {
    display: inline-flex;
    flex-shrink: 0;
    line-height: 0;
  }

  .ph :global(svg) {
    width: 100%;
    height: 100%;
  }
</style>
