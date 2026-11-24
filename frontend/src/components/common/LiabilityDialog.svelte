<script lang="ts">
  // the one-time warranty and liability acknowledgement for installs that
  // predate the onboarding step. it has no dismiss affordance: the notice is a
  // condition of using Pelton, so the only way on is the checkbox. the parent
  // mounts it only when the acknowledgement is missing.
  import { fade, scale } from 'svelte/transition'
  import { createEventDispatcher } from 'svelte'
  import { t } from '../../lib/i18n'
  import { acceptLiability } from '../../lib/liability'
  import { calm } from '../../lib/motion'
  import { toastError, errorMessage } from '../../stores/toast'
  import LiabilityNotice from './LiabilityNotice.svelte'

  const dispatch = createEventDispatcher<{ accepted: void }>()

  let accepted = false
  let busy = false

  async function confirm(): Promise<void> {
    if (!accepted || busy) return
    busy = true
    try {
      await acceptLiability()
      dispatch('accepted')
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }
</script>

<div class="backdrop" transition:fade={{ duration: calm(120) }}></div>
<div
  class="dialog"
  role="dialog"
  aria-modal="true"
  aria-label={$t('liability.continueTitle')}
  transition:scale={{ duration: calm(150), start: 0.94 }}
>
  <h2>{$t('liability.continueTitle')}</h2>
  <LiabilityNotice bind:accepted />
  <div class="actions">
    <button type="button" class="primary" disabled={!accepted || busy} on:click={confirm}>
      {$t('liability.continue')}
    </button>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 400;
    background: var(--scrim, rgba(0, 0, 0, 0.4));
    backdrop-filter: blur(2px);
  }

  .dialog {
    position: fixed;
    z-index: 401;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(460px, calc(100vw - 2 * var(--space-5)));
    padding: var(--space-6);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  h2 {
    margin: 0 0 var(--space-5);
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    margin-top: var(--space-6);
  }

  .primary {
    height: var(--control-height);
    padding: 0 var(--space-5);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font-family: inherit;
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }
  .primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
