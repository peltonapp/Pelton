<script lang="ts">
  // the nightly-build warning. unlike the liability acknowledgement this is
  // never remembered: a nightly is untested every single day, so the warning is
  // shown on every launch. it has no dismiss affordance and sits above
  // onboarding, so it is the first thing seen and the only way on is the
  // checkbox.
  import { fade, scale } from 'svelte/transition'
  import { createEventDispatcher } from 'svelte'
  import { t } from '../../lib/i18n'
  import { calm } from '../../lib/motion'
  import nightlyLogo from '../../assets/images/icons/pelton-nightly-logo.png'

  const dispatch = createEventDispatcher<{ accepted: void }>()

  let accepted = false
</script>

<div class="backdrop" transition:fade={{ duration: calm(120) }}></div>
<div
  class="dialog"
  role="dialog"
  aria-modal="true"
  aria-label={$t('nightly.title')}
  transition:scale={{ duration: calm(150), start: 0.94 }}
>
  <img class="mark" src={nightlyLogo} alt="" draggable="false" />
  <h2>{$t('nightly.title')}</h2>

  <p class="body">{$t('nightly.body')}</p>
  <p class="warn">{$t('nightly.realInbox')}</p>
  <p class="body">{$t('nightly.dataDir')}</p>
  <p class="body">{$t('nightly.warranty')}</p>

  <label class="ack">
    <input type="checkbox" bind:checked={accepted} />
    <span>{$t('nightly.accept')}</span>
  </label>

  <div class="actions">
    <button type="button" class="primary" disabled={!accepted} on:click={() => dispatch('accepted')}>
      {$t('nightly.continue')}
    </button>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 600;
    background: var(--scrim, rgba(0, 0, 0, 0.4));
    backdrop-filter: blur(2px);
  }

  .dialog {
    position: fixed;
    z-index: 601;
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

  .mark {
    display: block;
    width: 132px;
    height: 132px;
    margin: 0 auto var(--space-4);
  }

  h2 {
    margin: 0 0 var(--space-5);
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    text-align: center;
    color: var(--text-primary);
  }

  .body {
    margin: 0 0 var(--space-4);
    font-size: var(--fz-body);
    color: var(--text-secondary);
    line-height: 1.6;
  }

  .warn {
    margin: 0 0 var(--space-4);
    padding: var(--space-4);
    border-radius: var(--radius-control);
    background: var(--danger-bg);
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    line-height: 1.6;
  }

  .ack {
    display: flex;
    align-items: flex-start;
    gap: var(--space-4);
    margin: var(--space-6) 0 0;
    font-size: var(--fz-body);
    color: var(--text-primary);
    line-height: 1.5;
    cursor: var(--cursor-action);
  }
  .ack input {
    margin: 2px 0 0;
    accent-color: var(--accent);
    flex: none;
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
