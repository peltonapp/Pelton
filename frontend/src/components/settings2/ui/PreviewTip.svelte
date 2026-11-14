<script lang="ts">
  // a small eye button beside a setting's name. hovering or focusing it opens a
  // large bubble with a picture of what the setting does, so the answer to
  // "what does this change" is a look away instead of a trial.
  import { tick } from 'svelte'
  import eyeIcon from '@phosphor-icons/core/assets/duotone/eye-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import { portal } from '../../../lib/portal'
  import { t } from '../../../lib/i18n'

  /** Name of the setting the preview belongs to. */
  export let label: string

  let open = false
  let button: HTMLButtonElement
  let bubble: HTMLDivElement
  let left = 0
  let top = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  async function show(): Promise<void> {
    clearTimeout(timer)
    open = true
    await tick()
    if (!bubble || !button) {
      return
    }
    // the root carries a css zoom for the interface scale; rects are in zoomed
    // pixels and fixed offsets are not.
    const scale = Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
    const anchor = button.getBoundingClientRect()
    const own = bubble.getBoundingClientRect()
    let x = anchor.right + 10
    if (x + own.width > window.innerWidth - 8) {
      x = anchor.left - own.width - 10
    }
    let y = anchor.top + anchor.height / 2 - own.height / 2
    y = Math.max(8, Math.min(y, window.innerHeight - own.height - 8))
    left = Math.max(8, x) / scale
    top = y / scale
  }

  function hideSoon(): void {
    clearTimeout(timer)
    timer = setTimeout(() => (open = false), 80)
  }
</script>

<button
  type="button"
  class="eye"
  bind:this={button}
  aria-label={$t('settings2.preview').replace('{name}', label)}
  aria-expanded={open}
  on:pointerenter={show}
  on:pointerleave={hideSoon}
  on:focus={show}
  on:blur={hideSoon}
>
  <Ph svg={eyeIcon} size={14} />
</button>

{#if open}
  <div class="bubble" use:portal bind:this={bubble} style={`left:${left}px;top:${top}px`} role="tooltip">
    <slot />
  </div>
{/if}

<style>
  .eye {
    display: inline-flex;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: help;
    transition: color var(--motion-fast) var(--ease-out);
  }

  .eye:hover,
  .eye:focus-visible {
    color: var(--accent);
  }

  .bubble {
    position: fixed;
    z-index: 800;
    width: 300px;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    pointer-events: none;
    animation: rise var(--motion-base) var(--ease-out);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(3px) scale(0.985);
    }
  }
</style>
