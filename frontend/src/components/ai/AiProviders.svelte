<script lang="ts">
  // the AI providers the user set up: each with where its data goes, its
  // model and whether it is the default, with buttons to edit, make default
  // and delete, and one to add a provider. opening this contacts nothing.
  import { createEventDispatcher, onMount } from 'svelte'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import editIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import ProviderDialog from './ProviderDialog.svelte'
  import WhereDataGoes from './WhereDataGoes.svelte'
  import { aiProviderKinds, aiProviders, deleteAIProvider, setDefaultAIProvider } from '../../lib/api'
  import type { AIKind, AIProvider } from '../../lib/ai'
  import { askConfirm } from '../../stores/confirm'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ change: AIProvider[] }>()

  let providers: AIProvider[] = []
  let kinds: AIKind[] = []
  let loaded = false
  // the provider open in the dialog, 'new' to add one, null when closed.
  let editing: AIProvider | 'new' | null = null

  onMount(() => void load())

  async function load(): Promise<void> {
    try {
      ;[providers, kinds] = await Promise.all([aiProviders(), aiProviderKinds()])
      dispatch('change', providers)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loaded = true
    }
  }

  function kindLabel(id: string): string {
    const key = `ai.kind.${id}`
    const s = $t(key)
    return s === key ? (kinds.find((k) => k.id === id)?.label ?? id) : s
  }

  async function makeDefault(p: AIProvider): Promise<void> {
    try {
      await setDefaultAIProvider(p.id)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function remove(p: AIProvider): Promise<void> {
    const ok = await askConfirm({
      title: $t('ai.delete.title').replace('{name}', p.name),
      body: $t('ai.delete.body'),
      confirmLabel: $t('action.delete'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await deleteAIProvider(p.id)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onSaved(): Promise<void> {
    editing = null
    await load()
  }
</script>

{#if loaded && providers.length === 0}
  <p class="empty">{$t('ai.none')}</p>
{:else if providers.length > 0}
  <ul class="list">
    {#each providers as p (p.id)}
      <li>
        <div class="main">
          <span class="name">{p.name}</span>
          <span class="meta">{kindLabel(p.kind)} · {p.model || $t('ai.noModel')}</span>
        </div>
        <WhereDataGoes local={p.local} />
        {#if p.default}
          <span class="default">{$t('ai.default')}</span>
        {:else}
          <button type="button" class="cal-btn" on:click={() => makeDefault(p)}>{$t('ai.makeDefault')}</button>
        {/if}
        <button type="button" class="cal-icon" aria-label={$t('ai.edit').replace('{name}', p.name)} on:click={() => (editing = p)}>
          <Ph svg={editIcon} size={14} />
        </button>
        <button type="button" class="cal-icon" aria-label={$t('ai.delete').replace('{name}', p.name)} on:click={() => remove(p)}>
          <Ph svg={trashIcon} size={14} />
        </button>
      </li>
    {/each}
  </ul>
{/if}
{#if loaded && kinds.length > 0}
  <button type="button" class="cal-btn add" on:click={() => (editing = 'new')}>
    <Ph svg={plusIcon} size={14} />
    <span>{$t('ai.add')}</span>
  </button>
{/if}

{#if editing}
  <ProviderDialog {kinds} provider={editing === 'new' ? null : editing} on:close={() => (editing = null)} on:saved={onSaved} />
{/if}

<style>
  .empty {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    background: var(--surface-raised);
  }

  li + li {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }

  .name {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .meta {
    overflow: hidden;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .default {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .add {
    align-self: flex-start;
  }
</style>
