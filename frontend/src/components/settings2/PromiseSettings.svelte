<script lang="ts">
  // the promise settings on the Tasks page: whether mail is read for
  // promises at all, accepting the user's own on their own, the nudge, the
  // list accepted promises go to, and per account whether its mail is read
  // and whether an AI provider gives a second reading, with where that text
  // goes and what of it is sent.
  import { onMount } from 'svelte'
  import Card from './ui/Card.svelte'
  import Row from './ui/Row.svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import WhereDataGoes from '../ai/WhereDataGoes.svelte'
  import { loadPromiseSettings, promiseSettings, savePromiseSettings } from '../../stores/promises'
  import { aiProviders } from '../../lib/api'
  import { t } from '../../lib/i18n'
  import type { AIProvider } from '../../lib/ai'
  import type { PromiseAccount } from '../../lib/types'

  let providers: AIProvider[] = []

  onMount(() => {
    void loadPromiseSettings()
    void aiProviders()
      .then((list) => (providers = list))
      .catch(() => (providers = []))
  })

  $: s = $promiseSettings
  $: listItems = [
    { value: 'local', label: $t('promiseSettings.list.local') },
    { value: 'default', label: $t('promiseSettings.list.default') },
  ]
  $: providerItems = providers.map((p) => ({ value: p.id, label: p.name }))

  function setAccount(id: number, patch: Partial<PromiseAccount>): void {
    void savePromiseSettings({ accounts: s.accounts.map((a) => (a.accountId === id ? { ...a, ...patch } : a)) })
  }

  function setAI(a: PromiseAccount, on: boolean): void {
    setAccount(a.accountId, { ai: on, provider: on ? a.provider || (providers.find((p) => p.default) ?? providers[0])?.id || '' : '' })
  }

  function providerOf(id: string): AIProvider | undefined {
    return providers.find((p) => p.id === id)
  }
</script>

<Card title={$t('promiseSettings.title')} description={$t('promiseSettings.description')}>
  <Row id="promises-enabled" label={$t('promiseSettings.enabled')} hint={$t('promiseSettings.enabled.hint')}>
    <ToggleSwitch checked={s.enabled} label={$t('promiseSettings.enabled')} on:change={(e) => savePromiseSettings({ enabled: e.detail })} />
  </Row>
  <Row id="promises-auto" label={$t('promiseSettings.autoMine')} hint={$t('promiseSettings.autoMine.hint')} disabled={!s.enabled}>
    <ToggleSwitch checked={s.autoAcceptMine} disabled={!s.enabled} label={$t('promiseSettings.autoMine')} on:change={(e) => savePromiseSettings({ autoAcceptMine: e.detail })} />
  </Row>
  <Row id="promises-nudge" label={$t('promiseSettings.nudge')} hint={$t('promiseSettings.nudge.hint')} disabled={!s.enabled}>
    <ToggleSwitch checked={s.nudge} disabled={!s.enabled} label={$t('promiseSettings.nudge')} on:change={(e) => savePromiseSettings({ nudge: e.detail })} />
  </Row>
  <Row id="promises-list" label={$t('promiseSettings.list')} hint={$t('promiseSettings.list.hint')} disabled={!s.enabled}>
    <Select value={s.taskList} items={listItems} disabled={!s.enabled} ariaLabel={$t('promiseSettings.list')} on:change={(e) => savePromiseSettings({ taskList: e.detail === 'default' ? 'default' : 'local' })} />
  </Row>
</Card>

{#if s.accounts.length > 0}
  <Card title={$t('promiseSettings.accounts')} description={$t('promiseSettings.ai.what').replace('{n}', String(s.aiMaxChars)).replace('{perDay}', String(s.aiPerDay))}>
    {#each s.accounts as a (a.accountId)}
      <Row id={`promises-account-${a.accountId}`} label={a.name || a.email} hint={a.name ? a.email : ''} disabled={!s.enabled} stacked>
        <div class="account">
          <ToggleSwitch
            checked={a.on}
            disabled={!s.enabled}
            label={$t('promiseSettings.account.read').replace('{account}', a.email)}
            on:change={(e) => setAccount(a.accountId, { on: e.detail })}
          />
          <span class="label">{$t('promiseSettings.account.read.short')}</span>
          <ToggleSwitch
            checked={a.ai}
            disabled={!s.enabled || !a.on || providers.length === 0}
            label={$t('promiseSettings.account.ai').replace('{account}', a.email)}
            on:change={(e) => setAI(a, e.detail)}
          />
          <span class="label">{$t('promiseSettings.account.ai.short')}</span>
          {#if a.ai}
            <Select
              value={a.provider}
              items={providerItems}
              ariaLabel={$t('promiseSettings.account.provider').replace('{account}', a.email)}
              on:change={(e) => setAccount(a.accountId, { provider: e.detail })}
            />
            {#if providerOf(a.provider)}
              <WhereDataGoes local={providerOf(a.provider)?.local ?? false} />
            {/if}
          {/if}
        </div>
      </Row>
    {/each}
    {#if providers.length === 0}
      <p class="none">{$t('promiseSettings.ai.noProvider')}</p>
    {/if}
  </Card>
{/if}

<style>
  .account {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .label {
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .none {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }
</style>
