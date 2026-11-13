<script lang="ts">
  // the warranty and liability copy, shared by the onboarding step and the
  // one-time dialog for installs that predate it, so both always say the same
  // thing. the acknowledgement checkbox is bound by the parent, which owns the
  // button it unlocks.
  import { IconExternalLink } from '@tabler/icons-svelte'
  import { t } from '../../lib/i18n'
  import { termsUrl } from '../../lib/liability'
  import { openURL } from '../../lib/browser'

  /** whether the user has ticked the acknowledgement. */
  export let accepted = false
</script>

<p class="body">{$t('liability.body')}</p>
<p class="body">{$t('liability.warranty')}</p>
<p class="body">{$t('liability.limits')}</p>
<p class="body">{$t('liability.development')}</p>

<button type="button" class="terms" on:click={() => openURL(termsUrl())}>
  {$t('liability.link')}
  <IconExternalLink size={14} stroke={1.7} />
</button>

<label class="ack">
  <input type="checkbox" bind:checked={accepted} />
  <span>{$t('liability.accept')}</span>
</label>

<style>
  .body {
    margin: 0 0 var(--space-4);
    font-size: var(--fz-body);
    color: var(--text-secondary);
    line-height: 1.6;
  }
  .terms {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    padding: 0;
    border: 0;
    background: none;
    color: var(--link);
    font-family: inherit;
    font-size: var(--fz-body);
    cursor: var(--cursor-action);
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
</style>
