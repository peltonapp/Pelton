<script lang="ts">
  // the values of a script's settings, as a form built from the schema the
  // script declares: its SETTINGS table in Lua, or the settings section of a
  // block script. the backend checks each value against the schema; a field
  // left alone keeps the declared default.
  import ParamField from './ParamField.svelte'
  import { t } from '../../lib/i18n'
  import type { Script } from '../../lib/scripting'
  import { setSettings } from '../../stores/scripts'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import '../../theme/controls.css'

  /** The script whose settings are shown. */
  export let script: Script

  let values: Record<string, unknown> = { ...script.settings }
  let lastId = script.id
  let lastUpdated = script.updatedAt
  let busy = false

  $: if (script.id !== lastId || script.updatedAt !== lastUpdated) {
    lastId = script.id
    lastUpdated = script.updatedAt
    values = { ...script.settings }
  }
  $: dirty = JSON.stringify(values) !== JSON.stringify(script.settings)

  function set(name: string, v: unknown): void {
    const next = { ...values }
    if (v === undefined || v === '' || (typeof v === 'number' && Number.isNaN(v))) {
      delete next[name]
    } else {
      next[name] = v
    }
    values = next
  }

  async function save(): Promise<void> {
    busy = true
    try {
      await setSettings(script.id, values)
      toastSuccess($t('scripts.settings.saved'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }
</script>

{#if script.settingsSchema.length > 0}
  <div class="settings">
    <div class="grid">
      {#each script.settingsSchema as p (p.name)}
        <ParamField param={p} value={values[p.name]} on:change={(e) => set(p.name, e.detail)} />
      {/each}
    </div>
    <div class="row">
      <button type="button" class="cal-btn" disabled={busy || Object.keys(values).length === 0} on:click={() => (values = {})}>{$t('scripts.settings.defaults')}</button>
      <button type="button" class="cal-btn primary" disabled={busy || !dirty} on:click={() => void save()}>{$t('scripts.settings.save')}</button>
    </div>
  </div>
{:else}
  <p class="hint">{$t(script.kind === 'lua' ? 'scripts.settings.noneLua' : 'scripts.settings.noneBlocks')}</p>
{/if}

<style>
  .settings {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--control-height) * 6), 1fr));
    gap: var(--space-3);
  }

  .row {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
