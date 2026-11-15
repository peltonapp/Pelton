<script lang="ts">
  // one file in the attachments browser's grid: a thumbnail when the file is
  // an image or a pdf already stored here, its type glyph otherwise, then the
  // name and size. The thumbnail is only asked for once the tile is on screen.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import cloudIcon from '@phosphor-icons/core/assets/duotone/cloud-arrow-down-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { kindIcon } from './kinds'
  import { canThumbnail, thumbnailFor } from '../../lib/thumbnails'
  import { formatBytes } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import type { AttachmentEntry } from '../../lib/types'

  /** The file shown. */
  export let entry: AttachmentEntry
  /** Marked for saving. */
  export let selected = false
  /** The file in focus. */
  export let active = false

  const dispatch = createEventDispatcher<{ choose: MouseEvent; open: void }>()

  let el: HTMLDivElement
  let thumb: string | null = null
  let shown = false
  let observer: IntersectionObserver | null = null
  let askedFor = -1

  onMount(() => {
    if (typeof IntersectionObserver === 'undefined') {
      return
    }
    observer = new IntersectionObserver((records) => {
      shown = records.some((r) => r.isIntersecting)
    })
    observer.observe(el)
  })

  onDestroy(() => observer?.disconnect())

  // a tile can be handed another file as the grid scrolls.
  let thumbFor = -1
  $: if (entry.id !== thumbFor) {
    thumbFor = entry.id
    thumb = null
  }

  $: if (shown && canThumbnail(entry) && askedFor !== entry.id) {
    askedFor = entry.id
    const id = entry.id
    void thumbnailFor(entry).then((url) => {
      if (entry.id === id) {
        thumb = url
      }
    })
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<div
  id={`att-${entry.id}`}
  class="tile"
  class:active
  role="option"
  aria-selected={selected}
  tabindex="-1"
  bind:this={el}
  on:click={(e) => dispatch('choose', e)}
  on:dblclick={() => dispatch('open')}
>
  <div class="picture" class:remote={!entry.downloaded}>
    {#if thumb}
      <img src={thumb} alt="" draggable="false" />
    {:else}
      <Ph svg={kindIcon[entry.kind] ?? kindIcon.other} size={32} />
    {/if}
    {#if !entry.downloaded}
      <span class="badge" title={$t('attachments.notDownloaded')}>
        <Ph svg={cloudIcon} size={13} />
      </span>
    {/if}
  </div>
  <span class="name" dir="auto" title={entry.filename}>{entry.filename}</span>
  <span class="meta">{formatBytes(entry.sizeBytes)}</span>
</div>

<style>
  .tile {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .tile:hover {
    background: var(--surface-hover);
  }

  .tile[aria-selected='true'] {
    background: var(--selection-bg);
  }

  .tile.active {
    border-color: var(--accent);
  }

  .picture {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 1;
    overflow: hidden;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--accent);
  }

  .picture.remote {
    color: var(--text-tertiary);
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .badge {
    position: absolute;
    inset-block-end: var(--space-2);
    inset-inline-end: var(--space-2);
    display: inline-flex;
    padding: var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .meta {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }
</style>
