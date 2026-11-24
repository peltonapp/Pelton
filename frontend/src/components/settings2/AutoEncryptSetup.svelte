<script lang="ts">
  // the short walk through that switching automatic encryption on starts: what
  // it does, then each place keys can come from, explained in a sentence or
  // two and allowed or skipped one at a time. Only the local source starts out
  // allowed; nothing on the network is used unless it is allowed here.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import globeIcon from '@phosphor-icons/core/assets/duotone/globe-duotone.svg?raw'
  import houseIcon from '@phosphor-icons/core/assets/duotone/house-duotone.svg?raw'
  import KeyServerList from './KeyServerList.svelte'
  import { t } from '../../lib/i18n'
  import type { KeySources } from '../../lib/types'
  import '../../theme/controls.css'

  const dispatch = createEventDispatcher<{ close: void; done: KeySources }>()

  type SourceStep = 'autocrypt' | 'wkd' | 'koo' | 'servers'
  const steps: SourceStep[] = ['autocrypt', 'wkd', 'koo', 'servers']
  const local: Record<SourceStep, boolean> = { autocrypt: true, wkd: false, koo: false, servers: false }

  // 0 is the introduction, 1 to 4 the sources, 5 the summary.
  let index = 0
  const allowed: Record<SourceStep, boolean | null> = { autocrypt: null, wkd: null, koo: null, servers: null }
  let servers: string[] = []
  let heading: HTMLHeadingElement

  $: step = index >= 1 && index <= steps.length ? steps[index - 1] : null
  $: chosen = steps.filter((s) => allowed[s] === true)

  function go(next: number): void {
    index = next
    queueMicrotask(() => heading?.focus())
  }

  function decide(source: SourceStep, allow: boolean): void {
    allowed[source] = allow
    go(index + 1)
  }

  function finish(): void {
    dispatch('done', {
      autocrypt: allowed.autocrypt === true,
      wkd: allowed.wkd === true,
      keysOpenpgpOrg: allowed.koo === true,
      keyServers: allowed.servers === true,
      servers,
    })
  }
</script>

<Modal title={$t('autoEncrypt.flow.title')} size="small" on:close={() => dispatch('close')}>
  <div class="flow">
    {#if index === 0}
      <h3 tabindex="-1" bind:this={heading}>{$t('autoEncrypt.settings.title')}</h3>
      <p>{$t('autoEncrypt.flow.intro')}</p>
    {:else if step}
      <p class="count">{$t('autoEncrypt.flow.step').replace('{n}', String(index)).replace('{total}', String(steps.length))}</p>
      <h3 tabindex="-1" bind:this={heading}>{$t(`autoEncrypt.sourceName.${step}`)}</h3>
      <p class="where">
        <Ph svg={local[step] ? houseIcon : globeIcon} size={14} />
        <span>{local[step] ? $t('autoEncrypt.source.local') : $t('autoEncrypt.source.network')}</span>
      </p>
      <p>{$t(`autoEncrypt.sourceExplain.${step}`)}</p>
      {#if step === 'servers'}
        <KeyServerList bind:servers />
      {/if}
    {:else}
      <h3 tabindex="-1" bind:this={heading}>{$t('autoEncrypt.flow.summaryTitle')}</h3>
      {#if chosen.length > 0}
        <p>{$t('autoEncrypt.flow.summary').replace('{list}', chosen.map((s) => $t(`autoEncrypt.sourceName.${s}`)).join(', '))}</p>
      {:else}
        <p>{$t('autoEncrypt.flow.summaryNone')}</p>
      {/if}
      <ul class="choices">
        {#each steps as s (s)}
          <li>
            <span>{$t(`autoEncrypt.sourceName.${s}`)}</span>
            <span class="verdict">{allowed[s] ? $t('autoEncrypt.flow.allowed') : $t('autoEncrypt.flow.skipped')}</span>
          </li>
        {/each}
      </ul>
      <p class="later">{$t('autoEncrypt.flow.later')}</p>
    {/if}
  </div>
  <svelte:fragment slot="footer">
    {#if index > 0}
      <button type="button" class="cal-btn" on:click={() => go(index - 1)}>{$t('autoEncrypt.flow.back')}</button>
    {/if}
    {#if index === 0}
      <button type="button" class="cal-btn primary" on:click={() => go(1)}>{$t('autoEncrypt.flow.start')}</button>
    {:else if step}
      <button type="button" class="cal-btn" on:click={() => step && decide(step, false)}>{$t('autoEncrypt.flow.skip')}</button>
      <button type="button" class="cal-btn primary" on:click={() => step && decide(step, true)}>{$t('autoEncrypt.flow.allow')}</button>
    {:else}
      <button type="button" class="cal-btn primary" on:click={finish}>{$t('autoEncrypt.flow.finish')}</button>
    {/if}
  </svelte:fragment>
</Modal>

<style>
  .flow {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  h3 {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    outline: none;
  }

  p {
    margin: 0;
    line-height: 1.5;
  }

  .count,
  .later {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .where {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .choices {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .choices li {
    display: flex;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-1) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .verdict {
    color: var(--text-tertiary);
  }
</style>
