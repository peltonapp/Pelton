<script lang="ts">
  // the one-time tips of the new design. It watches for the moments a tip
  // belongs to (the first message opened, the first archive, the first other
  // space, the first drag of mail and so on) and hands them to the tips store,
  // which decides. A tip hangs under the control it is about when that is on
  // screen, and otherwise sits as a quiet bar at the bottom. One click on its
  // close button or Escape puts it away for good.
  import { onDestroy, onMount, tick } from 'svelte'
  import { get } from 'svelte/store'
  import { fly } from 'svelte/transition'
  import Ph from '../common/Ph.svelte'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import tipIcon from '@phosphor-icons/core/assets/duotone/lightbulb-duotone.svg?raw'
  import { activeTip, dismissTip, noteTipContext } from '../../stores/firsttips'
  import { bindings } from '../../stores/shortcuts'
  import { prefs } from '../../stores/prefs'
  import { space } from '../../stores/space'
  import { visibleMessageId } from '../../stores/tabs'
  import { mailDrag } from '../../stores/maildrag'
  import { activeNote } from '../../stores/notes'
  import { onCoachAction } from '../../lib/coach'
  import { tipParts } from '../../lib/firsttips'
  import { float } from '../../lib/floating'
  import { popupIsOpen } from '../../lib/popups'
  import { dur, motion } from '../../lib/motion'
  import { shortcutLabel, t } from '../../lib/i18n'

  const composing = new Set(['compose', 'reply', 'reply-all', 'forward'])

  let anchor: HTMLElement | null = null
  const stops: (() => void)[] = []

  let lastSpace: string | null = null

  onMount(() => {
    stops.push(
      space.subscribe((s) => {
        if (s === lastSpace) {
          return
        }
        // a tip belongs to the space it came up in.
        if (lastSpace !== null) {
          dismissTip()
        }
        lastSpace = s
        if (s !== 'mail' && !noteTipContext('space-keys') && s === 'calendar') {
          noteTipContext('calendar-quick-add')
        }
      }),
      visibleMessageId.subscribe((id) => {
        if (id !== null) {
          void tick().then(() => noteTipContext('reader-view'))
        }
      }),
      mailDrag.subscribe((drag) => {
        if (drag) {
          noteTipContext('mail-drag')
        }
      }),
      activeNote.subscribe((note) => {
        if (note) {
          noteTipContext('notes-links')
        }
      }),
      onCoachAction((action) => {
        if (action === 'archive' || action === 'done') {
          void tick().then(() => noteTipContext('archive-undo'))
        } else if (composing.has(action)) {
          void tick().then(() => noteTipContext('compose-send'))
        }
      }),
    )
    document.addEventListener('focusin', onFocusIn)
    window.addEventListener('keydown', onKeydown, true)
  })

  onDestroy(() => {
    for (const stop of stops) {
      stop()
    }
    document.removeEventListener('focusin', onFocusIn)
    window.removeEventListener('keydown', onKeydown, true)
    dismissTip()
  })

  function onFocusIn(event: FocusEvent): void {
    const target = event.target as HTMLElement | null
    if (target?.matches?.('[data-tip-anchor="search"]')) {
      noteTipContext('search-operators')
    }
  }

  // Escape puts a tip away before anything else hears it, unless a popup is
  // open: that one closes first.
  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || !get(activeTip) || popupIsOpen()) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    dismissTip()
  }

  function visible(el: HTMLElement | null): HTMLElement | null {
    if (!el) {
      return null
    }
    const r = el.getBoundingClientRect()
    return r.width > 0 && r.height > 0 ? el : null
  }

  $: anchor = $activeTip?.anchor ? visible(document.querySelector<HTMLElement>(`[data-tip-anchor="${$activeTip.anchor}"]`)) : null
  $: parts = $activeTip ? tipParts($t($activeTip.textKey), $activeTip, $bindings as Record<string, string>) : []
  $: hintTop = $prefs.toastPosition === 'bottom-center'
</script>

{#if $activeTip}
  {#key $activeTip.id}
    <div
      class="tip"
      class:anchored={anchor !== null}
      class:top={hintTop && anchor === null}
      role="status"
      aria-label={$t('tips.label')}
      use:float={{ anchor }}
      transition:fly={{ y: anchor ? -4 : 6, duration: dur(motion.base) }}
    >
      <Ph svg={tipIcon} size={15} />
      <span class="text">
        {#each parts as part, i (i)}
          {#if 'combo' in part}
            <kbd>{shortcutLabel(part.combo)}</kbd>
          {:else}
            <span>{part.text}</span>
          {/if}
        {/each}
      </span>
      <button type="button" class="close" aria-label={$t('tips.dismiss')} title={$t('tips.dismiss')} on:click={dismissTip}>
        <Ph svg={closeIcon} size={13} />
      </button>
    </div>
  {/key}
{/if}

<style>
  .tip {
    position: fixed;
    left: 50%;
    bottom: var(--coach-hint-offset);
    transform: translateX(-50%);
    z-index: 850;
    display: inline-flex;
    align-items: flex-start;
    gap: var(--space-2);
    max-width: min(32rem, calc(100% - var(--space-6) * 2));
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-inline-start: 2px solid var(--accent);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    line-height: 1.5;
  }

  .tip.top {
    bottom: auto;
    top: calc(var(--topbar-height) + var(--space-4));
  }

  /* placed by the float action under its control. */
  .tip.anchored {
    bottom: auto;
    transform: none;
    max-width: 22rem;
  }

  .tip > :global(.ph:first-child) {
    margin-top: calc((1.5em - 15px) / 2);
    color: var(--accent);
  }

  .text {
    flex: 1;
    min-width: 0;
  }

  kbd {
    display: inline-flex;
    align-items: center;
    min-height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-primary);
    vertical-align: middle;
  }

  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--chip-height);
    height: var(--chip-height);
    padding: 0;
    border: 0;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .close:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .close:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
