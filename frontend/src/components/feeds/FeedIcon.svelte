<script lang="ts">
  // a feed's icon: the site's own, kept locally as a data url, or the rss glyph
  // while there is none. a newsletter row shows an envelope instead.
  import rssIcon from '@phosphor-icons/core/assets/duotone/rss-simple-duotone.svg?raw'
  import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'

  /** The data url of the site's icon, or ''. */
  export let icon: string = ''
  /** Shows the envelope for a newsletter mail. */
  export let mail: boolean = false

  let broken = false
  $: if (icon) {
    broken = false
  }
  $: safe = icon.startsWith('data:image/') && !broken
</script>

{#if safe}
  <img class="icon" src={icon} alt="" aria-hidden="true" draggable="false" on:error={() => (broken = true)} />
{:else}
  <span class="glyph" aria-hidden="true"><Ph svg={mail ? mailIcon : rssIcon} size={12} /></span>
{/if}

<style>
  .icon,
  .glyph {
    flex-shrink: 0;
    width: var(--feed-icon);
    height: var(--feed-icon);
    border-radius: var(--radius-control);
  }

  .icon {
    object-fit: contain;
  }

  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--surface-sunken);
    color: var(--text-tertiary);
  }
</style>
