<script lang="ts">
  // adds or edits one AI provider: its kind, name, address, key and default
  // model. nothing is contacted until the user presses fetch models or test
  // connection; the key goes to the credential store when saved and is never
  // shown again, only replaced or removed.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import WhereDataGoes from './WhereDataGoes.svelte'
  import { fetchAIModels, saveAIProvider, testAIProvider } from '../../lib/api'
  import { isLocalAddress, newProviderInput, providerInput, type AIKind, type AIProvider, type AIProviderInput } from '../../lib/ai'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  /** Kinds a provider can be. */
  export let kinds: AIKind[]
  /** The provider to edit, null to add one. */
  export let provider: AIProvider | null = null

  const dispatch = createEventDispatcher<{ close: void; saved: AIProvider }>()

  let input: AIProviderInput = provider ? providerInput(provider) : newProviderInput(kinds[0])
  let busy = ''
  let error = ''
  let tested: { ok: boolean; text: string } | null = null

  $: kind = kinds.find((k) => k.id === input.kind) ?? kinds[0]
  $: local = isLocalAddress(input.baseUrl)
  $: hasStoredKey = !!provider?.hasKey && !input.clearKey
  $: needsKey = kind.key === 'required' && !hasStoredKey && input.key.trim() === ''
  $: canSave = input.name.trim() !== '' && input.baseUrl.trim() !== '' && !needsKey && busy === ''

  function kindLabel(k: AIKind): string {
    const key = `ai.kind.${k.id}`
    const s = $t(key)
    return s === key ? k.label : s
  }

  // a new provider follows the kind it is switched to; an edited one keeps
  // what the user set.
  function changeKind(id: string): void {
    const next = kinds.find((k) => k.id === id)
    if (!next) {
      return
    }
    if (provider) {
      input = { ...input, kind: id }
    } else {
      input = { ...newProviderInput(next), key: input.key }
    }
    tested = null
  }

  async function run(what: string, fn: () => Promise<void>): Promise<void> {
    busy = what
    error = ''
    try {
      await fn()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = ''
    }
  }

  function fetchModels(): Promise<void> {
    return run('models', async () => {
      const models = await fetchAIModels(input)
      input = { ...input, models, model: input.model || models[0] || '' }
    })
  }

  function test(): Promise<void> {
    return run('test', async () => {
      tested = null
      const res = await testAIProvider(input)
      tested = res.ok
        ? { ok: true, text: res.local ? $t('ai.test.okLocal') : $t('ai.test.ok') }
        : { ok: false, text: $t('ai.test.failed').replace('{reason}', res.message) }
    })
  }

  function save(): Promise<void> {
    return run('save', async () => {
      const saved = await saveAIProvider(input)
      dispatch('saved', saved)
    })
  }
</script>

<Modal title={provider ? $t('ai.edit').replace('{name}', provider.name) : $t('ai.add')} hint={$t('ai.dialog.hint')} on:close={() => dispatch('close')}>
  <form class="grid" id="ai-provider-form" on:submit|preventDefault={save}>
    <label for="ai-kind">{$t('ai.field.kind')}</label>
    <Select
      id="ai-kind"
      value={input.kind}
      disabled={!!provider}
      items={kinds.map((k) => ({ value: k.id, label: kindLabel(k) }))}
      on:change={(e) => changeKind(e.detail)}
    />

    <label for="ai-name">{$t('ai.field.name')}</label>
    <input id="ai-name" bind:value={input.name} autocomplete="off" />

    <label for="ai-url">{$t('ai.field.url')}</label>
    <div class="stack">
      <input id="ai-url" bind:value={input.baseUrl} autocomplete="off" spellcheck="false" placeholder="https://" />
      <span><WhereDataGoes {local} /></span>
    </div>

    {#if kind.key !== 'none'}
      <label for="ai-key">{$t('ai.field.key')}</label>
      <div class="stack">
        <input
          id="ai-key"
          type="password"
          bind:value={input.key}
          autocomplete="off"
          spellcheck="false"
          placeholder={hasStoredKey ? $t('ai.key.stored') : kind.key === 'optional' ? $t('ai.key.optional') : ''}
        />
        {#if provider?.hasKey}
          <label class="check">
            <input type="checkbox" bind:checked={input.clearKey} />
            {$t('ai.key.remove')}
          </label>
        {/if}
        <p class="hint">{$t('ai.key.hint')}</p>
      </div>
    {/if}

    <label for="ai-model">{$t('ai.field.model')}</label>
    <div class="stack">
      <div class="row">
        <input id="ai-model" bind:value={input.model} list="ai-models" autocomplete="off" spellcheck="false" />
        <datalist id="ai-models">
          {#each input.models as m (m)}
            <option value={m}></option>
          {/each}
        </datalist>
        <button type="button" class="cal-btn" disabled={busy !== '' || needsKey} on:click={fetchModels}>
          {busy === 'models' ? $t('ai.models.fetching') : $t('ai.models.fetch')}
        </button>
      </div>
      {#if input.models.length > 0}
        <p class="hint">{$t('ai.models.count').replace('{count}', String(input.models.length))}</p>
      {/if}
    </div>
  </form>

  {#if tested}
    <p class="result" class:ok={tested.ok} role="status">{tested.text}</p>
  {/if}
  {#if error}
    <p class="result" role="alert">{error}</p>
  {/if}

  <div slot="footer" class="buttons">
    <button type="button" class="cal-btn" disabled={busy !== '' || needsKey || input.model.trim() === ''} on:click={test}>
      {busy === 'test' ? $t('ai.test.running') : $t('ai.test')}
    </button>
    <span class="spacer"></span>
    <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('signatures.cancel')}</button>
    <button type="submit" form="ai-provider-form" class="cal-btn primary" disabled={!canSave}>{$t('action.save')}</button>
  </div>
</Modal>

<style>
  .grid {
    display: grid;
    grid-template-columns: max-content 1fr;
    align-items: start;
    gap: var(--space-3) var(--space-4);
  }

  .grid > label {
    padding-top: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }

  .row {
    display: flex;
    gap: var(--space-2);
  }

  .row input {
    flex: 1;
    min-width: 0;
  }

  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .result {
    margin: var(--space-3) 0 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .result.ok {
    color: var(--success);
  }

  .buttons {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
  }

  .spacer {
    flex: 1;
  }
</style>
