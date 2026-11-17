<script lang="ts">
  // the last step of the setup: what was set up, what was skipped with a way
  // straight back to it, where all of it can be changed later, and the offer
  // to become the default mail app where the system can say whether it is.
  import { createEventDispatcher, onMount } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import skipIcon from '@phosphor-icons/core/assets/duotone/circle-dashed-duotone.svg?raw'
  import '../../theme/controls.css'
  import { defaultMailClientStatus, setDefaultMailClient } from '../../lib/api'
  import { bindings } from '../../stores/shortcuts'
  import { errorMessage, toastError } from '../../stores/toast'
  import { shortcutLabel, t } from '../../lib/i18n'
  import type { SetupPiece, SummaryLine } from '../../lib/setupplan'

  /** The pieces of this run and how each ended up. */
  export let lines: SummaryLine[]
  /** A short detail per piece, such as the mailbox address. */
  export let details: Partial<Record<SetupPiece, string>> = {}

  const dispatch = createEventDispatcher<{ goto: SetupPiece }>()

  let offerDefault = false
  let settingDefault = false
  let defaultSet = false

  onMount(async () => {
    try {
      const s = await defaultMailClientStatus()
      offerDefault = s.known && !s.isDefault
    } catch {
      offerDefault = false
    }
  })

  async function makeDefault(): Promise<void> {
    settingDefault = true
    try {
      await setDefaultMailClient()
      defaultSet = true
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      settingDefault = false
    }
  }

  $: settingsKey = ($bindings as Record<string, string>)['preferences'] ?? ''
</script>

{#if lines.length > 0}
  <ul class="lines">
    {#each lines as line (line.piece)}
      <li class:skipped={line.status === 'skipped'}>
        <Ph svg={line.status === 'done' ? checkIcon : skipIcon} size={16} />
        <span class="text">
          <span class="name">{$t(`setup.piece.${line.piece}`)}</span>
          <span class="detail">{line.status === 'done' ? details[line.piece] || $t('setup.summary.done') : $t('setup.summary.skipped')}</span>
        </span>
        {#if line.status === 'skipped'}
          <button type="button" class="cal-btn" on:click={() => dispatch('goto', line.piece)}>{$t('setup.summary.setUpNow')}</button>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<p class="later">
  {$t('setup.summary.later')}
  {#if settingsKey}<kbd>{shortcutLabel(settingsKey)}</kbd>{/if}
</p>

{#if offerDefault && !defaultSet}
  <div class="default-mail">
    <span class="dm-text">
      <span class="dm-title">{$t('onboarding.defaultMailTitle')}</span>
      <span class="muted">{$t('onboarding.defaultMailSub')}</span>
    </span>
    <FlowButton disabled={settingDefault} on:click={makeDefault}>{$t('onboarding.defaultMailSet')}</FlowButton>
  </div>
{/if}

<style>
  .lines {
    margin: 0;
    padding: 0;
    list-style: none;
    border-top: var(--hairline) solid var(--border-default);
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-default);
    color: var(--success);
  }

  li.skipped {
    color: var(--text-tertiary);
  }

  .text {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }

  .name {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .detail {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow-wrap: anywhere;
  }

  .later {
    margin: var(--space-5) 0 0;
    font-size: var(--fz-body);
    line-height: 1.55;
    color: var(--text-secondary);
  }

  kbd {
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }

  .default-mail {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .dm-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 14em;
  }

  .dm-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
  }

  .muted {
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-tertiary);
  }
</style>
