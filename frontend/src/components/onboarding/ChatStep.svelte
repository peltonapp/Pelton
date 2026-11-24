<script lang="ts">
  // the chat step of the setup: what chat is, and turning it on. chat stays
  // off unless this step's button is pressed; signing in can follow right
  // away or wait for Settings.
  import { createEventDispatcher, onMount } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import ChatSignIn from '../chat/ChatSignIn.svelte'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import '../../theme/controls.css'
  import { prefs, setChatEnabled } from '../../stores/prefs'
  import { chatSignedIn, chatStatus, refreshStatus } from '../../stores/chat'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ enabled: boolean; signedin: void }>()

  onMount(() => {
    if ($prefs.chatEnabled) {
      void refreshStatus()
    }
  })

  function turn(on: boolean): void {
    setChatEnabled(on)
    dispatch('enabled', on)
  }
</script>

<ul class="facts">
  <li>{$t('setup.chat.matrix')}</li>
  <li>{$t('setup.chat.server')}</li>
  <li>{$t('setup.chat.off')}</li>
</ul>

{#if !$prefs.chatEnabled}
  <button type="button" class="cal-btn primary" on:click={() => turn(true)}>{$t('chatSettings.enabled')}</button>
{:else if $chatSignedIn}
  <p class="state" role="status"><Ph svg={checkIcon} size={16} />{$t('setup.chat.signedIn').replace('{user}', $chatStatus.userId)}</p>
{:else}
  <p class="state" role="status"><Ph svg={checkIcon} size={16} />{$t('setup.chat.on')}</p>
  <div class="signin">
    <ChatSignIn on:signedin={() => dispatch('signedin')} />
  </div>
  <p class="later">{$t('setup.chat.later')}</p>
  <button type="button" class="cal-btn" on:click={() => turn(false)}>{$t('setup.chat.turnOff')}</button>
{/if}

<style>
  .facts {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0 0 var(--space-5);
    padding-inline-start: var(--space-5);
    font-size: var(--fz-body);
    line-height: 1.55;
    color: var(--text-secondary);
  }

  .state {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 0 var(--space-4);
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .state :global(.ph) {
    color: var(--success);
  }

  .signin {
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .later {
    margin: var(--space-3) 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }
</style>
