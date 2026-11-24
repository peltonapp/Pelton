<script lang="ts">
  // longer output a script showed: plain text, or markdown rendered by the
  // compose preview into a sandboxed frame, never into the app's document.
  // its buttons come from the registered result actions the script may use:
  // those with a client run here (see lib/resulthandlers.ts), the rest in the
  // backend.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import MarkdownPreview from '../compose/MarkdownPreview.svelte'
  import { t } from '../../lib/i18n'
  import { runScriptResultAction } from '../../lib/api'
  import { entryDescription, entryLabel, inertMarkdown, type ScriptCatalog, type ScriptResult } from '../../lib/scripting'
  import { resultHandler } from '../../lib/resulthandlers'
  import { scriptIcon } from '../../lib/scripticons'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import '../../theme/controls.css'

  /** The result on screen. */
  export let result: ScriptResult
  /** The registry, for the buttons. */
  export let catalog: ScriptCatalog

  const dispatch = createEventDispatcher<{ close: void }>()
  let busy = ''

  $: actions = catalog.resultActions.filter((a) => result.actions.includes(a.id) && (!a.client || resultHandler(a.client)))
  $: title = result.title || $t('scripts.result.title').replace('{name}', result.scriptName)

  async function act(id: string): Promise<void> {
    const def = catalog.resultActions.find((a) => a.id === id)
    if (!def) {
      return
    }
    busy = id
    try {
      if (def.client) {
        const key = await resultHandler(def.client)!(result)
        if (key) {
          toastSuccess($t(key))
        }
        if (def.client === 'reply') {
          dispatch('close')
        }
        return
      }
      const said = await runScriptResultAction(result.id, id)
      toastSuccess($t('scripts.result.done').replace('{action}', entryLabel($t, def)).replace('{detail}', said))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = ''
    }
  }
</script>

<Modal {title} hint={result.title ? result.scriptName : ''} size="large" on:close>
  <div class="body">
    {#if result.dry}<p class="badge">{$t('scripts.result.dry')}</p>{/if}
    {#if result.format === 'markdown'}
      <div class="markdown">
        <MarkdownPreview markdown={inertMarkdown(result.text)} font="" />
      </div>
    {:else}
      <pre class="text">{result.text}</pre>
    {/if}
  </div>
  <svelte:fragment slot="footer">
    {#each actions as a (a.id)}
      <button type="button" class="cal-btn" disabled={busy !== '' || (result.dry && !a.client)} title={entryDescription($t, a)} on:click={() => void act(a.id)}>
        <Ph svg={scriptIcon(a.icon)} size={14} />
        {entryLabel($t, a)}
      </button>
    {/each}
    <button type="button" class="cal-btn primary" on:click={() => dispatch('close')}>{$t('scripts.result.close')}</button>
  </svelte:fragment>
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .badge {
    align-self: flex-start;
    margin: 0;
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .text {
    margin: 0;
    max-height: calc(var(--control-height) * 14);
    overflow: auto;
    padding: var(--space-3);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .markdown {
    display: flex;
    height: calc(var(--control-height) * 14);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .cal-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
</style>
