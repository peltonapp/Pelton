<script lang="ts">
  // the Lua the blocks compile to, read only, beside the canvas. it compiles a
  // moment after the last edit, marks the lines of the selected block, lists
  // what the compiler refused, and offers to turn the script into Lua for good.
  import { afterUpdate, createEventDispatcher, onDestroy } from 'svelte'
  import { t } from '../../../lib/i18n'
  import type { BlockTree, CompileResult, ScriptCatalog } from '../../../lib/scripting'
  import { compileBlocks } from '../../../stores/scripts'
  import { errorMessage } from '../../../stores/toast'
  import { lineMap } from './luamap'
  import '../../../theme/controls.css'

  /** The tree to compile. */
  export let tree: BlockTree
  /** The registry. */
  export let catalog: ScriptCatalog
  /** The script's trigger id. */
  export let trigger: string
  /** The selected block, whose lines are marked. */
  export let selected: string | null

  const dispatch = createEventDispatcher<{ select: string; convert: void }>()
  const delay = 300

  let result: CompileResult | null = null
  let failed = ''
  let timer: ReturnType<typeof setTimeout> | undefined
  let gen = 0
  let codeEl: HTMLElement
  let lastMarked = ''

  $: schedule(tree, trigger)
  $: lines = result?.lua ? result.lua.replace(/\n$/, '').split('\n') : []
  $: map = result?.lua ? lineMap(tree, catalog, result.lua) : new Map()
  $: range = selected ? map.get(selected) : undefined

  function schedule(next: BlockTree, trig: string): void {
    clearTimeout(timer)
    const mine = ++gen
    timer = setTimeout(() => {
      compileBlocks(next, trig)
        .then((r) => {
          if (mine === gen) {
            result = r
            failed = ''
          }
        })
        .catch((err) => {
          if (mine === gen) {
            failed = errorMessage(err)
          }
        })
    }, delay)
  }

  afterUpdate(() => {
    const key = range ? `${range.from}-${range.to}` : ''
    if (key && key !== lastMarked) {
      codeEl?.querySelector('.line.marked')?.scrollIntoView({ block: 'nearest' })
    }
    lastMarked = key
  })

  onDestroy(() => clearTimeout(timer))
</script>

<aside class="panel" aria-label={$t('scripts.generated')}>
  <header class="head">
    <h4 class="title">{$t('scripts.generated')}</h4>
    <button type="button" class="cal-btn" disabled={!result || result.errors.length > 0 || !!failed} on:click={() => dispatch('convert')}>{$t('scripts.convert')}</button>
  </header>
  <p class="hint">{$t('scripts.blocks.lua.hint')}</p>
  {#if failed}
    <p class="error" role="alert">{failed}</p>
  {:else if result && result.errors.length > 0}
    <ul class="errors">
      {#each result.errors as e, i (i)}
        <li>
          {#if e.block}
            <button type="button" class="link" on:click={() => dispatch('select', e.block)}>{e.block}</button>:
          {/if}
          {e.message}
        </li>
      {/each}
    </ul>
  {:else}
    <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
    <pre class="code" bind:this={codeEl} aria-label={$t('scripts.generated')} tabindex="0"><code>{#each lines as line, i (i)}<span class="line" class:marked={!!range && i + 1 >= range.from && i + 1 <= range.to}><span class="num" aria-hidden="true">{i + 1}</span>{line}</span>{/each}</code></pre>
  {/if}
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
    min-height: 0;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .title {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .code {
    flex: 1;
    min-height: 0;
    margin: 0;
    padding: var(--space-2) 0;
    overflow: auto;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    line-height: 1.5;
  }

  .code:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .line {
    display: block;
    padding-inline-end: var(--space-2);
    white-space: pre;
  }

  .line.marked {
    background: var(--selection-bg);
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .num {
    display: inline-block;
    min-width: var(--automation-gutter);
    padding-inline-end: var(--space-2);
    color: var(--text-tertiary);
    text-align: end;
    user-select: none;
  }

  .errors,
  .error {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--danger);
  }

  .errors {
    padding-inline-start: var(--space-4);
  }

  .link {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    text-decoration: underline;
    cursor: var(--cursor-action);
  }
</style>
