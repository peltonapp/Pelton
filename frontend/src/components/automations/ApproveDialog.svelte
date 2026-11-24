<script lang="ts">
  // asks the user to approve what a script may do before it is switched on:
  // its permissions with their risk, and the code that will run, as written
  // or as compiled from its blocks.
  import { createEventDispatcher, onMount } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import PermissionList from './PermissionList.svelte'
  import { t } from '../../lib/i18n'
  import { parseTree, type Script, type ScriptCatalog } from '../../lib/scripting'
  import { compileBlocks } from '../../stores/scripts'
  import '../../theme/controls.css'

  /** The script to approve. */
  export let script: Script
  /** The registry. */
  export let catalog: ScriptCatalog

  const dispatch = createEventDispatcher<{ approve: void; close: void }>()

  let code = script.kind === 'lua' ? script.source : ''

  onMount(async () => {
    if (script.kind === 'blocks') {
      const res = await compileBlocks(parseTree(script.blocks), script.trigger.type)
      code = res.errors.length > 0 ? res.errors.map((e) => e.message).join('\n') : res.lua
    }
  })
</script>

<Modal title={$t('scripts.approve.title').replace('{name}', script.name)} hint={$t('scripts.approve.hint')} size="large" on:close>
  <div class="body">
    <h3 class="heading">{$t('scripts.permissions')}</h3>
    <PermissionList {catalog} declared={script.permissions} approved={script.approved} />
    <h3 class="heading">{$t('scripts.approve.code')}</h3>
    <pre class="code">{code}</pre>
  </div>
  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('scripts.cancel')}</button>
    <button type="button" class="cal-btn primary" on:click={() => dispatch('approve')}>{$t('scripts.approve.confirm')}</button>
  </svelte:fragment>
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .heading {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .code {
    margin: 0;
    max-height: var(--automation-steps-max);
    overflow: auto;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    white-space: pre-wrap;
  }
</style>
