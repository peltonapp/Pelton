<script lang="ts">
  // a small 3x2 grid that mirrors the screen: clicking a cell anchors the toast
  // stack to that corner or edge. shows the current choice with a filled dot.
  // hovering a cell previews a sample notification at that real screen position.
  import { createEventDispatcher } from 'svelte'
  import { IconCircleCheck } from '@tabler/icons-svelte'
  import type { ToastPosition } from '../../lib/types'
  import { t } from '../../lib/i18n'

  export let value: string

  const dispatch = createEventDispatcher<{ change: ToastPosition; hover: ToastPosition | null }>()

  const positions: ToastPosition[] = [
    'top-left',
    'top-center',
    'top-right',
    'bottom-left',
    'bottom-center',
    'bottom-right',
  ]

  // the position currently being previewed on hover, or null.
  let preview: ToastPosition | null = null

  $: dispatch('hover', preview)

  const positionKeys: Record<ToastPosition, string> = {
    'top-left': 'settingsPanel.posTopLeft',
    'top-center': 'settingsPanel.posTopCenter',
    'top-right': 'settingsPanel.posTopRight',
    'bottom-left': 'settingsPanel.posBottomLeft',
    'bottom-center': 'settingsPanel.posBottomCenter',
    'bottom-right': 'settingsPanel.posBottomRight',
  }

  $: labelFor = (p: ToastPosition): string => $t(positionKeys[p])
</script>

<div class="grid" role="group" aria-label={$t('settings.toastPosition')}>
  {#each positions as p (p)}
    <button
      type="button"
      class="cell"
      class:active={value === p}
      aria-label={labelFor(p)}
      aria-pressed={value === p}
      title={labelFor(p)}
      on:click={() => dispatch('change', p)}
      on:mouseenter={() => (preview = p)}
      on:mouseleave={() => (preview = null)}
      on:focus={() => (preview = p)}
      on:blur={() => (preview = null)}
    >
      <span class="dot"></span>
    </button>
  {/each}
</div>

{#if preview}
  <!-- a sample notification at the real screen position being hovered. -->
  <div class={`preview-toast ${preview}`} aria-hidden="true">
    <IconCircleCheck size={16} stroke={1.7} />
    <span>{$t('settingsPanel.notificationPreview')}</span>
  </div>
{/if}

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(3, 44px);
    grid-template-rows: repeat(2, 30px);
    gap: var(--space-2);
  }

  .cell {
    display: flex;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    cursor: var(--cursor-action);
    padding: 4px;
  }

  /* align the dot to the cell's corner so the grid reads like a screen map. */
  .cell:nth-child(3n + 1) {
    justify-content: flex-start;
  }
  .cell:nth-child(3n + 2) {
    justify-content: center;
  }
  .cell:nth-child(3n + 3) {
    justify-content: flex-end;
  }
  .cell:nth-child(-n + 3) {
    align-items: flex-start;
  }
  .cell:nth-child(n + 4) {
    align-items: flex-end;
  }

  .cell:hover {
    background: var(--surface-hover);
  }

  .cell.active {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 999px;
    background: var(--text-tertiary);
  }

  .cell.active .dot {
    background: var(--accent);
  }

  /* the live preview chip, anchored exactly like the real toast stack. */
  .preview-toast {
    position: fixed;
    z-index: 300;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-5);
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    color: var(--text-primary);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fz-label);
    pointer-events: none;
  }

  .preview-toast :global(svg) {
    color: var(--success);
  }

  .preview-toast.top-left {
    top: 0;
    left: 0;
  }
  .preview-toast.top-center {
    top: 0;
    left: 50%;
    transform: translateX(-50%);
  }
  .preview-toast.top-right {
    top: 0;
    right: 0;
  }
  .preview-toast.bottom-left {
    bottom: 0;
    left: 0;
  }
  .preview-toast.bottom-center {
    bottom: 0;
    left: 50%;
    transform: translateX(-50%);
  }
  .preview-toast.bottom-right {
    bottom: 0;
    right: 0;
  }
</style>
