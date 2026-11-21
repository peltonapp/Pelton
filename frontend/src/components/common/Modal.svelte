<script context="module" lang="ts">
  // every open modal, outermost first. escape only reaches the top one, and a
  // modal opened from inside another (the icon picker over the profile form)
  // stacks above it.
  const stack: symbol[] = []
</script>

<script lang="ts">
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { fade, scale } from 'svelte/transition'
  import { IconX } from '@tabler/icons-svelte'
  import { prefs } from '../../stores/prefs'
  import { popupIsOpen } from '../../lib/popups'
  import { t } from '../../lib/i18n'

  /** Heading, also the dialog's accessible name. */
  export let title: string
  /** Line under the heading. Left out when empty. */
  export let hint = ''
  /** Width preset: small 420px, medium 560px, large 820px. */
  export let size: 'small' | 'medium' | 'large' = 'medium'
  /**
   * True while the form holds edits that closing would throw away. Escape, the
   * backdrop and the close button then ask before they discard.
   */
  export let dirty = false
  /**
   * False while the dialog must not be dismissed, during an import that is
   * already running. It hides the close button and makes escape and the
   * backdrop inert.
   */
  export let closable = true

  const dispatch = createEventDispatcher<{ close: void }>()
  const id = Symbol('modal')

  let dialog: HTMLElement
  let depth = 0
  let confirming = false
  let restoreTo: HTMLElement | null = null

  $: motion = $prefs.reduceMotion ? 0 : 1
  // 300 is where the app's overlay band starts. Four apart leaves room for the
  // backdrop and dialog of each level without running into the popups at 320.
  $: layer = 300 + depth * 4

  onMount(async () => {
    restoreTo = document.activeElement as HTMLElement | null
    stack.push(id)
    depth = stack.length - 1
    await tick()
    focusFirst()
  })

  onDestroy(() => {
    const at = stack.indexOf(id)
    if (at !== -1) {
      stack.splice(at, 1)
    }
    restoreTo?.focus?.()
  })

  function focusables(): HTMLElement[] {
    if (!dialog) {
      return []
    }
    const found = dialog.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    )
    return [...found].filter((el) => el.offsetParent !== null)
  }

  // the dialog itself takes focus unless a field asked for it. Focusing the
  // first control instead would put the ring on whatever happens to come first,
  // and anything that reacts to focus (a tooltip, a picker) would fire on open.
  function focusFirst(): void {
    const wanted = dialog?.querySelector<HTMLElement>('[autofocus]')
    if (wanted) {
      wanted.focus()
      return
    }
    dialog?.focus()
  }

  /** Closes, or asks first when there are unsaved edits. */
  export function requestClose(): void {
    if (!closable) {
      return
    }
    if (dirty) {
      confirming = true
      return
    }
    dispatch('close')
  }

  function onKeydown(event: KeyboardEvent): void {
    if (stack[stack.length - 1] !== id) {
      return
    }
    if (event.key === 'Escape') {
      // a picker open over the dialog gets the key first. this listens at the
      // capture phase, so without asking, escape would close the dialog out
      // from under the open list and discard the edits behind it.
      if (popupIsOpen()) {
        return
      }
      event.stopPropagation()
      if (confirming) {
        confirming = false
      } else {
        requestClose()
      }
      return
    }
    if (event.key !== 'Tab') {
      return
    }
    // the focus trap. without it tab walks out of the dialog into the page
    // behind it, which is still there and still clickable-looking.
    const items = focusables()
    if (items.length === 0) {
      return
    }
    const edge = event.shiftKey ? items[0] : items[items.length - 1]
    const current = document.activeElement
    // focus on the dialog itself (where it starts) or anywhere outside the
    // controls counts as an edge too, or shift+tab from the start walks out.
    const atEdge = current === edge || current === dialog || !items.includes(current as HTMLElement)
    if (atEdge) {
      event.preventDefault()
      ;(event.shiftKey ? items[items.length - 1] : items[0]).focus()
    }
  }
</script>

<svelte:window on:keydown|capture={onKeydown} />

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div
  class="backdrop"
  style="z-index: {layer}"
  transition:fade={{ duration: 120 * motion }}
  on:click={requestClose}
></div>

<div
  class="dialog {size}"
  style="z-index: {layer + 1}"
  role="dialog"
  aria-modal="true"
  aria-label={title}
  tabindex="-1"
  bind:this={dialog}
  transition:scale={{ duration: 150 * motion, start: motion ? 0.94 : 1 }}
>
  <header>
    <div class="heading">
      <h2>
        {#if $$slots.icon}
          <span class="title-icon"><slot name="icon" /></span>
        {/if}
        {title}
      </h2>
      {#if hint}
        <p class="hint">{hint}</p>
      {/if}
    </div>
    {#if closable}
      <button type="button" class="close" aria-label={$t('modal.close')} on:click={requestClose}>
        <IconX size={16} stroke={1.8} />
      </button>
    {/if}
  </header>

  <div class="body">
    <slot />
  </div>

  {#if $$slots.footer}
    <footer>
      <slot name="footer" />
    </footer>
  {/if}

  {#if confirming}
    <div class="discard" transition:fade={{ duration: 100 * motion }}>
      <div class="discard-box">
        <h3>{$t('modal.discardTitle')}</h3>
        <p>{$t('modal.discardHint')}</p>
        <div class="discard-actions">
          <button type="button" class="ghost" on:click={() => (confirming = false)}>
            {$t('modal.keepEditing')}
          </button>
          <button type="button" class="danger" on:click={() => dispatch('close')}>
            {$t('action.discard')}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    backdrop-filter: blur(2px);
  }

  .dialog {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    display: flex;
    flex-direction: column;
    max-height: 85vh;
    width: min(var(--modal-width), calc(100vw - 2 * var(--space-5)));
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }
  .small {
    --modal-width: 420px;
  }
  .medium {
    --modal-width: 560px;
  }
  .large {
    --modal-width: 820px;
  }

  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-4);
    padding-bottom: var(--space-3);
  }

  .heading {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  h2 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .title-icon {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .close {
    flex-shrink: 0;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }
  .close:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 0 var(--space-4) var(--space-4);
  }

  footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
  }

  .discard {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-4);
    background: var(--scrim);
  }

  .discard-box {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    max-width: 340px;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }
  .discard-box h3 {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }
  .discard-box p {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
    line-height: 1.5;
  }

  .discard-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-1);
  }
  .discard-actions button {
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }
  .ghost {
    border: var(--hairline) solid var(--border-default);
    background: transparent;
    color: var(--text-secondary);
  }
  .ghost:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .danger {
    border: var(--hairline) solid transparent;
    background: var(--danger);
    color: var(--text-inverse);
  }
  .danger:hover {
    filter: brightness(1.05);
  }
</style>
