<script lang="ts">
  // the middle pane for a shared library: its name, which scripts pass to
  // require, a description and the Lua, with the same aids as a script's
  // code. a library runs inside the script that loads it, with that script's
  // permissions, so it declares none of its own.
  import { onDestroy } from 'svelte'
  import LuaWorkbench from './LuaWorkbench.svelte'
  import { t } from '../../lib/i18n'
  import { checkScriptLua } from '../../lib/api'
  import type { ScriptCatalog, ScriptLibrary, ScriptLibraryInput } from '../../lib/scripting'
  import { exportLibrary, libraries, removeLibrary, saveLibrary, testRun } from '../../stores/scripts'
  import { askConfirm } from '../../stores/confirm'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { syntaxErrorLine } from './lua/assist'
  import type { LineMark } from './lua/extensions'
  import '../../theme/controls.css'

  /** The library being edited. */
  export let library: ScriptLibrary
  /** The registry. */
  export let catalog: ScriptCatalog

  let input: ScriptLibraryInput = fromLibrary(library)
  let lastId = library.id
  let lastUpdated = library.updatedAt
  let busy = false
  let syntax = ''
  let timer: ReturnType<typeof setTimeout> | undefined

  function fromLibrary(l: ScriptLibrary): ScriptLibraryInput {
    return { name: l.name, description: l.description, source: l.source }
  }

  $: if (library.id !== lastId || library.updatedAt !== lastUpdated) {
    lastId = library.id
    lastUpdated = library.updatedAt
    input = fromLibrary(library)
    syntax = ''
  }
  $: dirty = JSON.stringify(input) !== JSON.stringify(fromLibrary(library))
  $: nameOk = /^[a-z][a-z0-9_]{0,63}$/.test(input.name)
  $: scheduleCheck(input.source)
  $: assist = {
    catalog,
    trigger: '',
    settings: [],
    libraries: $libraries.map((l) => l.name).filter((n) => n !== library.name),
    t: $t,
  }
  $: runMark = $testRun?.errorLibrary === library.name && $testRun.errorLine > 0 ? [{ line: $testRun.errorLine, message: $testRun.error }] : []
  $: marks = [...(syntax && syntaxErrorLine(syntax) > 0 ? [{ line: syntaxErrorLine(syntax), message: syntax }] : []), ...runMark] as LineMark[]

  function scheduleCheck(source: string): void {
    clearTimeout(timer)
    timer = setTimeout(() => {
      void checkScriptLua(source)
        .then((msg) => (syntax = msg))
        .catch(() => (syntax = ''))
    }, 400)
  }

  onDestroy(() => clearTimeout(timer))

  async function save(): Promise<void> {
    busy = true
    try {
      await saveLibrary(library.id, input)
      toastSuccess($t('scripts.toast.saved'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function remove(): Promise<void> {
    const ok = await askConfirm({
      title: $t('scripts.library.delete.title').replace('{name}', library.name),
      body: $t('scripts.library.delete.body'),
      confirmLabel: $t('scripts.delete'),
      danger: true,
    })
    if (ok) {
      await removeLibrary(library.id).catch((err) => toastError(errorMessage(err)))
    }
  }
</script>

<section class="editor" aria-label={$t('scripts.library.editor')}>
  <header class="bar">
    <input class="cal-field name" aria-label={$t('scripts.library.name')} aria-invalid={!nameOk} spellcheck="false" bind:value={input.name} />
    <div class="actions">
      <button type="button" class="cal-btn" on:click={() => void exportLibrary(library.id).catch((err) => toastError(errorMessage(err)))}>{$t('scripts.export')}</button>
      <button type="button" class="cal-btn danger" on:click={() => void remove()}>{$t('scripts.delete')}</button>
      <button type="button" class="cal-btn primary" disabled={busy || !dirty || !nameOk} on:click={() => void save()}>{$t('scripts.save')}</button>
    </div>
  </header>
  <div class="scroll">
    {#if !nameOk}<p class="error" role="alert">{$t('scripts.library.nameRule')}</p>{/if}
    {#if library.origin !== 'user'}
      <p class="notice">{$t(`scripts.origin.${library.origin}.notice`)}</p>
    {/if}
    <input class="cal-field" aria-label={$t('scripts.library.description')} placeholder={$t('scripts.library.description')} bind:value={input.description} />
    <p class="hint">{$t('scripts.library.hint').replace('{use}', `local ${input.name || 'lib'} = require("${input.name}")`)}</p>
    <LuaWorkbench label={$t('scripts.code')} content={input.source} {catalog} {assist} {marks} on:change={(e) => (input = { ...input, source: e.detail })} />
    {#if syntax}<p class="error" role="alert">{syntax}</p>{/if}
  </div>
</section>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--surface-base);
  }

  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .name {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-weight: var(--fw-medium);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--pane-pad);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .notice {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-label);
  }

  .error {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--danger);
  }
</style>
