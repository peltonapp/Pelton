<script lang="ts">
  // the toast stack for transient feedback (queued, saved, errors). each toast
  // shows a kind icon, its message and a close button, and animates in and out.
  // the stack anchors to the corner or edge the user chose in settings. purely
  // ephemeral ui state.
  import { fly, fade } from 'svelte/transition'
  import { IconCircleCheck, IconAlertCircle, IconInfoCircle, IconX } from '@tabler/icons-svelte'
  import { toasts, dismiss, pauseToast, resumeToast, type Toast } from '../../stores/toast'
  import PieProgress from './PieProgress.svelte'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'
  import { calm } from '../../lib/motion'
  import type { ToastKind } from '../../stores/toast'
  import type { ToastPosition } from '../../lib/types'

  const icons = {
    success: IconCircleCheck,
    error: IconAlertCircle,
    info: IconInfoCircle,
  }

  $: position = ($prefs.toastPosition as ToastPosition) ?? 'bottom-right'
  // toasts fly in from the nearest edge: downward for top positions, upward for
  // bottom positions.
  $: flyY = position.startsWith('top') ? -16 : 16

  function iconFor(kind: ToastKind) {
    return icons[kind]
  }

  // each toast is read out once, when it arrives: a countdown ticking in its
  // text stays quiet. errors interrupt, everything else waits its turn.
  let said = new Set<number>()
  let politeText = ''
  let alertText = ''
  $: announce($toasts)

  function announce(list: Toast[]): void {
    const live = new Set(list.map((x) => x.id))
    for (const toast of list) {
      if (said.has(toast.id)) {
        continue
      }
      const actions = [toast.action, ...(toast.more ?? [])].filter(Boolean).map((a) => a!.label)
      const text = [toast.message, ...actions].join('. ')
      if (toast.kind === 'error') {
        alertText = text
      } else {
        politeText = text
      }
    }
    said = live
  }

  // the new design holds a toast while the pointer or focus rests on it.
  $: holds = $prefs.design !== 'classic'
</script>

<div class="sr-only" role="status">{politeText}</div>
<div class="sr-only" role="alert">{alertText}</div>
<div class={`stack ${position}`}>
  {#each $toasts as toast (toast.id)}
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class={`toast ${toast.kind}`}
      in:fly={{ y: flyY, duration: calm(200) }}
      out:fade={{ duration: calm(150) }}
      on:mouseenter={() => holds && pauseToast(toast.id)}
      on:mouseleave={() => holds && resumeToast(toast.id)}
      on:focusin={() => holds && pauseToast(toast.id)}
      on:focusout={() => holds && resumeToast(toast.id)}
    >
      <span class="icon" aria-hidden="true">
        {#if toast.busy}
          <PieProgress size={15} label={toast.message} />
        {:else}
          <svelte:component this={iconFor(toast.kind)} size={17} stroke={1.7} />
        {/if}
      </span>
      <span class="message">{toast.message}</span>
      {#if toast.action}
        <button type="button" class="action" on:click={toast.action.run}>{toast.action.label}</button>
      {/if}
      {#each toast.more ?? [] as extra (extra.label)}
        <button type="button" class="action" on:click={extra.run}>{extra.label}</button>
      {/each}
      <button type="button" class="close" aria-label={$t('common.toasts.dismiss')} on:click={() => dismiss(toast.id)}>
        <IconX size={14} stroke={1.8} />
      </button>
    </div>
  {/each}
</div>

<style>
  .stack {
    position: fixed;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    /* above the whole modal band. Modals layer from 300 upwards (300 + depth *
       4, see Modal.svelte), so a toast at 200 sat behind the blurred backdrop
       of any open dialog - and a dialog is exactly where an error toast is
       most likely to be raised, which made the error invisible. */
    z-index: 600;
    padding: var(--space-5);
    pointer-events: none;
  }

  /* anchoring. the stack only occupies its corner; toasts capture pointer events
     themselves so the rest of the app stays clickable. the top anchors start
     below the macOS traffic lights, which are drawn over the webview and would
     otherwise land on a top-left toast; zero on every other platform. */
  .stack.top-left {
    top: var(--titlebar-lights);
    left: 0;
    align-items: flex-start;
  }
  .stack.top-center {
    top: var(--titlebar-lights);
    left: 50%;
    transform: translateX(-50%);
    align-items: center;
  }
  .stack.top-right {
    top: var(--titlebar-lights);
    right: 0;
    align-items: flex-end;
  }
  .stack.bottom-left {
    bottom: 0;
    left: 0;
    align-items: flex-start;
  }
  .stack.bottom-center {
    bottom: 0;
    left: 50%;
    transform: translateX(-50%);
    align-items: center;
  }
  .stack.bottom-right {
    bottom: 0;
    right: 0;
    align-items: flex-end;
  }

  .toast {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    color: var(--text-primary);
    box-shadow: var(--shadow-overlay);
    padding: var(--space-3) var(--space-3) var(--space-3) var(--space-4);
    font-size: var(--fz-label);
    max-width: 52ch;
  }

  /* the kind is carried by the icon color, not a border. */
  .toast.success .icon {
    color: var(--success);
  }
  .toast.error .icon {
    color: var(--danger);
  }
  .toast.info .icon {
    color: var(--accent);
  }

  .icon {
    display: inline-flex;
    flex-shrink: 0;
  }

  .message {
    flex: 1;
    line-height: 1.4;
  }

  .action {
    flex-shrink: 0;
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--accent);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }

  .action:hover {
    background: var(--surface-hover);
  }

  .close {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
    flex-shrink: 0;
  }

  .close:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
