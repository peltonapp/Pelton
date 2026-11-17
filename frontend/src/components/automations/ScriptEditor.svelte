<script lang="ts">
  // the middle pane of the Automations space: one script's name, trigger,
  // code, permissions and run options, with saving, a test run that changes
  // nothing, running it now, export and delete. a Lua script is edited as
  // source; a block script shows the block editor (mounted from the store,
  // see blockEditor) above the Lua its blocks compile to, read only.
  import { onDestroy } from 'svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import LuaEditor from './LuaEditor.svelte'
  import ParamField from './ParamField.svelte'
  import PermissionList from './PermissionList.svelte'
  import ScriptFolder from './ScriptFolder.svelte'
  import NetHosts from './NetHosts.svelte'
  import RunSteps from './RunSteps.svelte'
  import { t } from '../../lib/i18n'
  import { checkScriptLua } from '../../lib/api'
  import {
    entryDescription,
    entryLabel,
    inputsDiffer,
    parseTree,
    riskyFunctions,
    serializeTree,
    toInput,
    triggerTakesMessage,
    withTriggerPermission,
    type BlockTree,
    type CompileResult,
    type Script,
    type ScriptCatalog,
    type ScriptInput,
  } from '../../lib/scripting'
  import {
    blockEditor,
    compileBlocks,
    convertToLua,
    exportToFile,
    removeScript,
    runNow,
    runTest,
    saveScript,
    setAllowUnasked,
    testRun,
  } from '../../stores/scripts'
  import { askConfirm } from '../../stores/confirm'
  import { visibleMessageId } from '../../stores/tabs'
  import { errorMessage, toastError, toastInfo, toastSuccess } from '../../stores/toast'
  import '../../theme/controls.css'

  /** The script being edited. */
  export let script: Script
  /** The registry. */
  export let catalog: ScriptCatalog

  let input: ScriptInput = toInput(script)
  let lastId = script.id
  let lastUpdated = script.updatedAt
  let busy = false
  let syntax = ''
  let compiled: CompileResult | null = null
  let blockPerms: string[] | null = null
  let testOn: 'sample' | 'open' = 'sample'
  let checkTimer: ReturnType<typeof setTimeout> | undefined

  $: if (script.id !== lastId || script.updatedAt !== lastUpdated) {
    lastId = script.id
    lastUpdated = script.updatedAt
    input = toInput(script)
    compiled = null
    blockPerms = null
    syntax = ''
  }
  $: dirty = inputsDiffer(input, toInput(script))
  $: trigger = catalog.triggers.find((tr) => tr.id === input.trigger.type)
  $: triggerItems = groupTriggers(catalog)
  $: tree = input.kind === 'blocks' ? parseTree(input.blocks) : null
  $: if (tree && !$blockEditor) {
    void recompile(tree, input.trigger.type)
  }
  $: if (input.kind === 'lua') {
    scheduleCheck(input.source)
  }
  $: missing = (blockPerms ?? compiled?.permissions ?? []).filter((p) => !input.permissions.includes(p))
  $: takesMessage = triggerTakesMessage(catalog, input.trigger.type)
  $: risky = riskyFunctions(catalog, script.permissions)
  $: catchUpItems = (['live', 'run', 'ask'] as const).map((m) => ({ value: m, label: $t(`scripts.catchUp.${m}`) }))

  function groupTriggers(c: ScriptCatalog) {
    const groups = new Map<string, { value: string; label: string; disabled?: boolean }[]>()
    for (const tr of c.triggers) {
      const list = groups.get(tr.category) ?? []
      list.push({ value: tr.id, label: entryLabel($t, tr), disabled: !!tr.missing })
      groups.set(tr.category, list)
    }
    return [...groups.entries()].map(([category, options]) => ({ label: $t(`scripts.category.${category}`), options }))
  }

  async function recompile(next: BlockTree, triggerId: string): Promise<void> {
    try {
      compiled = await compileBlocks(next, triggerId)
    } catch (err) {
      compiled = { lua: '', errors: [{ block: '', message: errorMessage(err) }], permissions: [], lines: {} }
    }
  }

  function scheduleCheck(source: string): void {
    clearTimeout(checkTimer)
    checkTimer = setTimeout(() => {
      void checkScriptLua(source)
        .then((msg) => (syntax = msg))
        .catch(() => (syntax = ''))
    }, 400)
  }

  onDestroy(() => clearTimeout(checkTimer))

  function setTrigger(id: string): void {
    input = { ...input, trigger: { type: id, params: {} }, permissions: withTriggerPermission(catalog, id, input.permissions) }
  }

  function setTriggerParam(name: string, value: unknown): void {
    input = { ...input, trigger: { ...input.trigger, params: { ...input.trigger.params, [name]: value } } }
  }

  function setTree(next: BlockTree): void {
    input = { ...input, blocks: serializeTree(next) }
  }

  async function save(): Promise<void> {
    busy = true
    try {
      const wasOn = script.enabled
      const saved = await saveScript(script.id, input)
      if (wasOn && !saved.enabled) {
        toastInfo($t('scripts.toast.needsApproval'))
      } else {
        toastSuccess($t('scripts.toast.saved'))
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function test(): Promise<void> {
    busy = true
    try {
      await runTest(input, testOn === 'open' ? ($visibleMessageId ?? 0) : 0)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function remove(): Promise<void> {
    const ok = await askConfirm({
      title: $t('scripts.delete.title').replace('{name}', script.name),
      body: $t('scripts.delete.body'),
      confirmLabel: $t('scripts.delete'),
      danger: true,
    })
    if (ok) {
      await removeScript(script.id).catch((err) => toastError(errorMessage(err)))
    }
  }

  async function convert(): Promise<void> {
    const ok = await askConfirm({
      title: $t('scripts.convert.title'),
      body: $t('scripts.convert.body'),
      confirmLabel: $t('scripts.convert'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      if (dirty) {
        await saveScript(script.id, input)
      }
      await convertToLua(script.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function allow(fn: string, on: boolean): Promise<void> {
    await setAllowUnasked(script.id, fn, on).catch((err) => toastError(errorMessage(err)))
  }
</script>

<section class="editor" aria-label={$t('scripts.editor')}>
  <header class="bar">
    <input class="cal-field name" aria-label={$t('scripts.name')} bind:value={input.name} />
    <div class="actions">
      {#if script.enabled && script.trigger.type === 'manual'}
        <button type="button" class="cal-btn" disabled={busy || dirty} on:click={() => void runNow(script, takesMessage ? ($visibleMessageId ?? 0) : 0)}>{$t('scripts.runNow')}</button>
      {/if}
      <button type="button" class="cal-btn" on:click={() => void exportToFile(script.id).catch((err) => toastError(errorMessage(err)))}>{$t('scripts.export')}</button>
      <button type="button" class="cal-btn danger" on:click={() => void remove()}>{$t('scripts.delete')}</button>
      <button type="button" class="cal-btn primary" disabled={busy || !dirty} on:click={() => void save()}>{$t('scripts.save')}</button>
    </div>
  </header>

  <div class="scroll">
    {#if script.origin !== 'user'}
      <p class="notice">{$t(`scripts.origin.${script.origin}.notice`)}</p>
    {/if}

    <input class="cal-field" aria-label={$t('scripts.description')} placeholder={$t('scripts.description')} bind:value={input.description} />

    <fieldset class="group">
      <legend>{$t('scripts.trigger')}</legend>
      <Select ariaLabel={$t('scripts.trigger')} value={input.trigger.type} items={triggerItems} on:change={(e) => setTrigger(e.detail)} />
      {#if trigger}
        <p class="hint">{entryDescription($t, trigger)}</p>
        {#if trigger.params.length > 0}
          <div class="params">
            {#each trigger.params as p (p.name)}
              <ParamField param={p} value={input.trigger.params[p.name]} on:change={(e) => setTriggerParam(p.name, e.detail)} />
            {/each}
          </div>
        {/if}
      {/if}
      {#if input.trigger.type === 'mail.arrived'}
        <div class="params">
          <div class="field">
            <label class="label" for="catch-up">{$t('scripts.catchUp')}</label>
            <Select id="catch-up" value={input.catchUp} items={catchUpItems} on:change={(e) => (input = { ...input, catchUp: e.detail as ScriptInput['catchUp'] })} />
          </div>
          {#if input.catchUp !== 'live'}
            <div class="field">
              <label class="label" for="catch-up-cap">{$t('scripts.catchUpCap')}</label>
              <input id="catch-up-cap" class="cal-field" type="number" min="1" max="1000" bind:value={input.catchUpCap} />
            </div>
          {/if}
        </div>
      {/if}
    </fieldset>

    <fieldset class="group code">
      <legend>{$t(input.kind === 'lua' ? 'scripts.code' : 'scripts.blocks')}</legend>
      {#if input.kind === 'lua'}
        <div class="source">
          <LuaEditor label={$t('scripts.code')} content={input.source} on:change={(e) => (input = { ...input, source: e.detail })} />
        </div>
        {#if syntax}<p class="error" role="alert">{syntax}</p>{/if}
      {:else if tree}
        <!-- the block editor mounts here; see blockEditor in stores/scripts.ts -->
        <div class="blocks" data-block-editor-mount>
          {#if $blockEditor}
            <svelte:component
              this={$blockEditor}
              {tree}
              {catalog}
              trigger={input.trigger.type}
              settings={input.settingsSchema}
              on:change={(e) => setTree(e.detail)}
              on:permissions={(e) => (blockPerms = e.detail)}
              on:convert={() => void convert()}
            />
          {:else}
            <p class="hint">{$t('scripts.blocks.pending')}</p>
          {/if}
        </div>
      {/if}
      {#if tree && !$blockEditor}
        <div class="generated-head">
          <span class="label">{$t('scripts.generated')}</span>
          <button type="button" class="cal-btn" disabled={dirty || (compiled?.errors.length ?? 0) > 0} on:click={() => void convert()}>{$t('scripts.convert')}</button>
        </div>
        {#if compiled && compiled.errors.length > 0}
          <ul class="errors" role="alert">
            {#each compiled.errors as e, i (i)}
              <li>{e.block ? `${e.block}: ` : ''}{e.message}</li>
            {/each}
          </ul>
        {:else}
          <div class="source generated">
            <LuaEditor readonly label={$t('scripts.generated')} content={compiled?.lua ?? ''} />
          </div>
        {/if}
      {/if}
    </fieldset>

    <fieldset class="group">
      <legend>{$t('scripts.permissions')}</legend>
      {#if missing.length > 0}
        <p class="hint">
          {$t('scripts.perm.blocksNeed').replace('{list}', missing.join(', '))}
          <button type="button" class="cal-btn" on:click={() => (input = { ...input, permissions: [...input.permissions, ...missing] })}>{$t('scripts.perm.add')}</button>
        </p>
      {/if}
      <PermissionList {catalog} declared={input.permissions} editable locked={trigger?.permission ?? ''} on:change={(e) => (input = { ...input, permissions: e.detail })} />
      {#if input.permissions.some((p) => p === 'net' || p.startsWith('net:'))}<NetHosts declared={input.permissions} on:change={(e) => (input = { ...input, permissions: e.detail })} />{/if}
      {#if script.permissions.some((p) => p.startsWith('files.'))}<ScriptFolder scriptId={script.id} />{/if}
      {#if risky.length > 0}
        <p class="label">{$t('scripts.allowUnasked')}</p>
        <ul class="unasked">
          {#each risky as fn (fn)}
            <li>
              <ToggleSwitch label={fn} checked={script.allowUnasked.includes(fn)} on:change={(e) => void allow(fn, e.detail)} />
              <code>{fn}</code>
            </li>
          {/each}
        </ul>
      {/if}
    </fieldset>

    <fieldset class="group">
      <legend>{$t('scripts.test')}</legend>
      <p class="hint">{$t('scripts.test.hint')}</p>
      <div class="test-row">
        {#if takesMessage}
          <Select
            ariaLabel={$t('scripts.test.on')}
            value={testOn}
            items={[
              { value: 'sample', label: $t('scripts.test.sample') },
              { value: 'open', label: $t('scripts.test.open'), disabled: $visibleMessageId === null },
            ]}
            on:change={(e) => (testOn = e.detail === 'open' ? 'open' : 'sample')}
          />
        {/if}
        <button type="button" class="cal-btn" disabled={busy} on:click={() => void test()}>{$t('scripts.test.run')}</button>
      </div>
      {#if $testRun}
        <p class="result {$testRun.status}" role="status">
          {$t(`scripts.status.${$testRun.status}`)}{$testRun.error ? `: ${$testRun.error}` : ''}
        </p>
        <RunSteps steps={$testRun.steps} />
      {/if}
    </fieldset>
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
    gap: var(--space-4);
    padding: var(--pane-pad);
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    border: none;
  }

  legend {
    padding: 0;
    margin-bottom: var(--space-2);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .params {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--control-height) * 6), 1fr));
    gap: var(--space-3);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .hint {
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
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

  .source {
    display: flex;
    min-height: calc(var(--control-height) * 10);
  }

  .source.generated {
    min-height: calc(var(--control-height) * 6);
  }

  .blocks {
    display: flex;
    flex-direction: column;
    min-height: calc(var(--control-height) * 4);
    padding: var(--space-3);
    border: var(--hairline) dashed var(--border-default);
    border-radius: var(--radius-control);
  }

  .generated-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .error,
  .errors {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--danger);
  }

  .errors {
    padding-inline-start: var(--space-4);
  }

  .unasked {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .unasked li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-meta);
  }

  .unasked code {
    font-family: var(--font-mono);
  }

  .test-row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
  }

  .result {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--success);
  }

  .result.error,
  .result.denied,
  .result.timeout,
  .result.limit {
    color: var(--danger);
  }
</style>
