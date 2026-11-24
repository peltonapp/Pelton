<script lang="ts">
  // picks the AI providers a script may send data to. each one ticked is
  // declared as "ai:<id>", or "ai_local:<id>" for a provider on this device or
  // the local network, and approved with the rest of the script's permissions,
  // so the approval names where the data goes. providers are managed in a
  // dialog here too, which is how the classic design reaches them.
  import { createEventDispatcher, onMount } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import AiProviders from './AiProviders.svelte'
  import WhereDataGoes from './WhereDataGoes.svelte'
  import { aiProviders } from '../../lib/api'
  import { aiTarget, setAIAccess, type AIProvider } from '../../lib/ai'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  /** The declared permissions. */
  export let declared: string[]

  const dispatch = createEventDispatcher<{ change: string[] }>()

  let providers: AIProvider[] = []
  let managing = false

  onMount(async () => {
    try {
      providers = await aiProviders()
    } catch (err) {
      toastError(errorMessage(err))
    }
  })

  $: allowed = new Set(declared.map(aiTarget).filter((id) => id !== ''))

  function toggle(p: AIProvider, on: boolean): void {
    dispatch('change', setAIAccess(declared, p, on))
  }
</script>

<div class="access" role="group" aria-labelledby="ai-access-label">
  <span class="label" id="ai-access-label">{$t('ai.access.label')}</span>
  {#if providers.length === 0}
    <p class="hint">{$t('ai.access.none')}</p>
  {:else}
    <ul>
      {#each providers as p (p.id)}
        <li>
          <label>
            <input type="checkbox" checked={allowed.has(p.id)} on:change={(e) => toggle(p, e.currentTarget.checked)} />
            <span class="name">{p.name}</span>
          </label>
          <WhereDataGoes local={p.local} />
        </li>
      {/each}
    </ul>
    <p class="hint">{$t('ai.access.hint')}</p>
  {/if}
  <button type="button" class="cal-btn manage" on:click={() => (managing = true)}>{$t('ai.access.manage')}</button>
</div>

{#if managing}
  <Modal title={$t('ai.providers')} hint={$t('ai.description')} on:close={() => (managing = false)}>
    <div class="providers">
      <AiProviders on:change={(e) => (providers = e.detail)} />
    </div>
  </Modal>
{/if}

<style>
  .access {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin-top: var(--space-2);
  }

  .label {
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  ul {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }

  li label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .manage {
    align-self: flex-start;
  }

  .providers {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
</style>
