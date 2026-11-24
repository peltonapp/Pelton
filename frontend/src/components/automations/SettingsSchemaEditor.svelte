<script lang="ts">
  // the settings a block script declares: a name blocks refer to as a
  // setting, a type from the ones the registry allows, a label, a default and
  // an enum's options. a Lua script declares its settings in a SETTINGS table
  // instead.
  import { createEventDispatcher } from 'svelte'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-circle-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import ParamField from './ParamField.svelte'
  import { t } from '../../lib/i18n'
  import type { ParamType, ScriptCatalog, ScriptParam } from '../../lib/scripting'
  import '../../theme/controls.css'

  /** The declared settings. */
  export let schema: ScriptParam[]
  /** The registry, for the setting types. */
  export let catalog: ScriptCatalog

  const dispatch = createEventDispatcher<{ change: ScriptParam[] }>()
  const namePattern = /^[A-Za-z_][A-Za-z0-9_]*$/

  $: typeItems = catalog.settingTypes.map((ty) => ({ value: ty, label: $t(`scripts.paramType.${ty}`) }))

  function update(i: number, patch: Partial<ScriptParam>): void {
    dispatch('change', schema.map((p, j) => (j === i ? { ...p, ...patch } : p)))
  }

  function setType(i: number, type: ParamType): void {
    update(i, { type, default: undefined, options: type === 'enum' ? (schema[i].options ?? []) : undefined })
  }

  function setOptions(i: number, raw: string): void {
    const options = raw
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s !== '')
      .map((value) => ({ value, labelKey: '', label: value }))
    update(i, { options })
  }

  function add(): void {
    let n = schema.length + 1
    while (schema.some((p) => p.name === `setting${n}`)) {
      n++
    }
    dispatch('change', [...schema, { name: `setting${n}`, type: 'string', labelKey: '', label: '' }])
  }
</script>

<div class="schema">
  {#each schema as p, i (i)}
    <div class="row">
      <div class="field">
        <label class="label" for={`setting-name-${i}`}>{$t('scripts.settings.name')}</label>
        <input
          id={`setting-name-${i}`}
          class="cal-field mono"
          class:bad={!namePattern.test(p.name)}
          aria-invalid={!namePattern.test(p.name)}
          aria-describedby={namePattern.test(p.name) ? undefined : `setting-name-${i}-problem`}
          value={p.name}
          on:change={(e) => update(i, { name: e.currentTarget.value.trim() })}
        />
        {#if !namePattern.test(p.name)}
          <span class="problem" id={`setting-name-${i}-problem`}><Ph svg={warnIcon} size={12} />{$t('scripts.settings.nameRule')}</span>
        {/if}
      </div>
      <div class="field">
        <label class="label" for={`setting-type-${i}`}>{$t('scripts.settings.type')}</label>
        <Select id={`setting-type-${i}`} value={p.type} items={typeItems} on:change={(e) => setType(i, e.detail as ParamType)} />
      </div>
      <div class="field">
        <label class="label" for={`setting-label-${i}`}>{$t('scripts.settings.label')}</label>
        <input id={`setting-label-${i}`} class="cal-field" value={p.label ?? ''} on:change={(e) => update(i, { label: e.currentTarget.value })} />
      </div>
      {#if p.type === 'enum'}
        <div class="field">
          <label class="label" for={`setting-options-${i}`}>{$t('scripts.settings.options')}</label>
          <input id={`setting-options-${i}`} class="cal-field" value={(p.options ?? []).map((o) => o.value).join(', ')} on:change={(e) => setOptions(i, e.currentTarget.value)} />
        </div>
      {/if}
      <ParamField param={{ ...p, labelKey: 'scripts.settings.default', label: '', required: false }} value={p.default} on:change={(e) => update(i, { default: e.detail === '' ? undefined : e.detail })} />
      <button type="button" class="cal-btn danger remove" aria-label={$t('scripts.settings.remove').replace('{name}', p.name)} on:click={() => dispatch('change', schema.filter((_, j) => j !== i))}>{$t('scripts.settings.removeShort')}</button>
    </div>
  {/each}
  <button type="button" class="cal-btn add" on:click={add}>{$t('scripts.settings.add')}</button>
</div>

<style>
  .schema {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .row {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--control-height) * 5), 1fr));
    gap: var(--space-2);
    align-items: end;
    padding-block-end: var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
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

  .mono {
    font-family: var(--font-mono);
  }

  .bad {
    border-color: var(--danger);
  }

  .problem {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--danger);
  }

  .add,
  .remove {
    justify-self: start;
    align-self: start;
  }
</style>
