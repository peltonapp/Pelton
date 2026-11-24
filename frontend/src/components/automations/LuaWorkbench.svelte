<script lang="ts">
  // the Lua editor with its panels: the API reference, whose entries insert at
  // the caret, and the match tester for trying a pattern on real mail. used by
  // the script editor and the library editor alike.
  import { createEventDispatcher } from 'svelte'
  import bookIcon from '@phosphor-icons/core/assets/duotone/book-open-duotone.svg?raw'
  import testIcon from '@phosphor-icons/core/assets/duotone/test-tube-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import LuaEditor from './LuaEditor.svelte'
  import ApiReference from './ApiReference.svelte'
  import MatchTester from './MatchTester.svelte'
  import { t } from '../../lib/i18n'
  import type { ScriptCatalog } from '../../lib/scripting'
  import type { LuaContext } from './lua/assist'
  import type { LineMark } from './lua/extensions'

  /** The source. */
  export let content: string
  /** Accessible name of the editor. */
  export let label: string
  /** The registry. */
  export let catalog: ScriptCatalog
  /** What completion and the docs line draw on. */
  export let assist: LuaContext
  /** Lines to mark as wrong. */
  export let marks: LineMark[] = []

  const dispatch = createEventDispatcher<{ change: string }>()

  let panel: 'none' | 'reference' | 'tester' = 'none'
  let editor: LuaEditor

  function toggle(p: typeof panel): void {
    panel = panel === p ? 'none' : p
  }
</script>

<div class="bench" class:open={panel !== 'none'}>
  <div class="tools" role="toolbar" aria-label={$t('scripts.lua.tools')}>
    <button type="button" class="tool" class:on={panel === 'reference'} aria-pressed={panel === 'reference'} on:click={() => toggle('reference')}>
      <Ph svg={bookIcon} size={15} />{$t('scripts.ref.title')}
    </button>
    <button type="button" class="tool" class:on={panel === 'tester'} aria-pressed={panel === 'tester'} on:click={() => toggle('tester')}>
      <Ph svg={testIcon} size={15} />{$t('scripts.match.title')}
    </button>
  </div>
  <div class="body">
    <div class="source">
      <LuaEditor bind:this={editor} {label} {content} {assist} {marks} on:change={(e) => dispatch('change', e.detail)} />
    </div>
    {#if panel === 'reference'}
      <div class="panel">
        <ApiReference {catalog} trigger={assist.trigger} on:insert={(e) => editor.insert(e.detail)} />
      </div>
    {:else if panel === 'tester'}
      <div class="panel">
        <MatchTester idBase="lua-match" pattern="" />
      </div>
    {/if}
  </div>
</div>

<style>
  .bench {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }

  .tools {
    display: flex;
    gap: var(--space-1);
  }

  .tool {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .tool:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tool.on {
    background: var(--selection-bg);
    color: var(--accent);
  }

  .tool:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
  }

  .open .body {
    grid-template-columns: minmax(0, 1fr) var(--block-lua-width);
  }

  .source {
    display: flex;
    min-height: calc(var(--control-height) * 10);
  }

  .panel {
    display: flex;
    min-width: 0;
    max-height: calc(var(--control-height) * 18);
    overflow-y: auto;
  }

  .panel > :global(*) {
    flex: 1;
  }
</style>
