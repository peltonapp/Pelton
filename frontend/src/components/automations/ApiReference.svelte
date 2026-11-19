<script lang="ts">
  // the API reference beside the Lua editor, built from the registry: the
  // globals, every module function with its signature, what it needs and
  // whether it asks first, the triggers with their event fields, the value
  // types and the permissions. a search narrows it, a click inserts the name
  // at the caret, and the whole reference copies as markdown.
  import { createEventDispatcher } from 'svelte'
  import riskIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import copyIcon from '@phosphor-icons/core/assets/duotone/copy-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { scriptReference } from '../../lib/api'
  import type { ScriptCatalog } from '../../lib/scripting'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { referenceSections, searchReference, type RefEntry } from './lua/reference'
  import '../../theme/controls.css'

  /** The registry the reference is built from. */
  export let catalog: ScriptCatalog
  /** The trigger the script uses, whose section opens first. */
  export let trigger = ''

  const dispatch = createEventDispatcher<{ insert: string }>()

  let query = ''

  $: sections = referenceSections(catalog, $t)
  $: shown = searchReference(sections, query)

  function pick(e: RefEntry): void {
    dispatch('insert', e.insert)
  }

  async function copyMarkdown(): Promise<void> {
    try {
      await navigator.clipboard.writeText(await scriptReference(true))
      toastSuccess($t('scripts.ref.copied'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<aside class="reference" aria-label={$t('scripts.ref.title')}>
  <div class="head">
    <input class="cal-field" type="search" aria-label={$t('scripts.ref.search')} placeholder={$t('scripts.ref.search')} bind:value={query} />
    <button type="button" class="icon" title={$t('scripts.ref.copy')} aria-label={$t('scripts.ref.copy')} on:click={() => void copyMarkdown()}>
      <Ph svg={copyIcon} size={15} />
    </button>
  </div>
  <p class="hint">{$t('scripts.ref.hint')}</p>
  <div class="list">
    {#each shown as s (s.key)}
      <details open={!!query || s.kind === 'globals' || s.key === `trigger:${trigger}`}>
        <summary>{s.title}</summary>
        {#if s.description}<p class="desc">{s.description}</p>{/if}
        <ul>
          {#each s.entries as e (e.name)}
            <li>
              <button type="button" class="entry" title={$t('scripts.ref.insert')} on:click={() => pick(e)}>
                <code>{e.code}</code>
                {#if e.description}<span class="desc">{e.description}</span>{/if}
                {#if e.permissions.length > 0 || e.risky || (s.kind === 'permissions' && e.risk)}
                  <span class="meta">
                    {#each e.permissions as p (p)}<span class="perm risk-{e.risk || 'low'}">{p}</span>{/each}
                    {#if s.kind === 'permissions'}<span class="perm risk-{e.risk}">{$t(`scripts.risk.${e.risk}`)}</span>{/if}
                    {#if e.risky}<span class="perm risk-high"><Ph svg={riskIcon} size={12} />{$t('scripts.lua.asksFirst')}</span>{/if}
                  </span>
                {/if}
              </button>
            </li>
          {/each}
        </ul>
      </details>
    {:else}
      <p class="hint">{$t('scripts.ref.none')}</p>
    {/each}
  </div>
</aside>

<style>
  .reference {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
    min-height: 0;
  }

  .head {
    display: flex;
    gap: var(--space-1);
  }

  .head input {
    flex: 1;
    min-width: 0;
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: var(--control-height);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  summary {
    padding: var(--space-1) 0;
    font-size: var(--fz-label);
    font-family: var(--font-mono);
    color: var(--text-primary);
    cursor: var(--cursor-action);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .entry {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: 100%;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    text-align: start;
    color: var(--text-primary);
    cursor: var(--cursor-action);
  }

  .entry:hover,
  .entry:focus-visible {
    background: var(--surface-hover);
  }

  code {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    overflow-wrap: anywhere;
  }

  .desc {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .perm {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0 var(--space-1);
    border: var(--hairline) solid currentColor;
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    line-height: var(--chip-height);
    color: var(--text-tertiary);
  }

  .perm.risk-medium {
    color: var(--warning);
  }

  .perm.risk-high {
    color: var(--danger);
  }
</style>
