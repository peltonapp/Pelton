<script lang="ts">
  // the left pane of the Automations space: every script with its trigger, a
  // badge for one that was imported or made by an assistant, and its on/off
  // switch. switching on a script with permissions still to approve opens the
  // approval dialog instead.
  import { createEventDispatcher } from 'svelte'
  import emptyIcon from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
  import EmptyState from '../common/EmptyState.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { t } from '../../lib/i18n'
  import { entryLabel, type Script } from '../../lib/scripting'
  import { catalog, scripts, selectedScriptId, selectScript, setEnabled } from '../../stores/scripts'
  import { errorMessage, toastError } from '../../stores/toast'
  import '../../theme/controls.css'

  const dispatch = createEventDispatcher<{ create: 'lua' | 'blocks'; import: void }>()

  function triggerName(s: Script): string {
    const def = $catalog?.triggers.find((tr) => tr.id === s.trigger.type)
    return def ? entryLabel($t, def) : s.trigger.type
  }

  async function toggle(s: Script, on: boolean): Promise<void> {
    try {
      await setEnabled(s, on)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<section class="list" aria-label={$t('scripts.list')}>
  <header class="bar">
    <h2 class="title">{$t('space.automations')}</h2>
    <div class="actions">
      <button type="button" class="cal-btn" on:click={() => dispatch('import')}>{$t('scripts.import')}</button>
      <button type="button" class="cal-btn primary" on:click={() => dispatch('create', 'lua')}>{$t('scripts.new')}</button>
    </div>
  </header>
  {#if $scripts.length === 0}
    <EmptyState
      icon={emptyIcon}
      title={$t('scripts.empty.title')}
      detail={$t('scripts.empty.detail')}
      actionLabel={$t('scripts.newBlocks')}
      onAction={() => dispatch('create', 'blocks')}
      secondaryLabel={$t('scripts.newLua')}
      onSecondary={() => dispatch('create', 'lua')}
    />
  {:else}
    <ul class="rows">
      {#each $scripts as s (s.id)}
        <li class="row" class:active={$selectedScriptId === s.id}>
          <button type="button" class="pick" aria-current={$selectedScriptId === s.id ? 'true' : undefined} on:click={() => selectScript(s.id)}>
            <span class="name">{s.name}</span>
            <span class="meta">
              {triggerName(s)}
              {#if s.kind === 'blocks'}<span class="tag">{$t('scripts.kind.blocks')}</span>{/if}
              {#if s.origin !== 'user'}<span class="tag origin">{$t(`scripts.origin.${s.origin}`)}</span>{/if}
            </span>
          </button>
          <ToggleSwitch
            label={$t(s.enabled ? 'scripts.switchOff' : 'scripts.switchOn').replace('{name}', s.name)}
            checked={s.enabled}
            on:change={(e) => void toggle(s, e.detail)}
          />
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .list {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    border-inline-end: var(--hairline) solid var(--border-default);
    background: var(--surface-base);
  }

  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .actions {
    display: flex;
    gap: var(--space-1);
  }

  .rows {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--row-pad-y) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .row.active {
    background: var(--selection-bg);
  }

  .pick {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: 0;
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    text-align: start;
    cursor: pointer;
  }

  .pick:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .name {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .tag {
    padding: 0 var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
  }

  .tag.origin {
    border-color: var(--warning);
    color: var(--warning);
  }
</style>
