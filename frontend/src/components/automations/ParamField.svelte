<script lang="ts">
  // one parameter of a trigger or a script setting, with the control its type
  // calls for. every control comes from the parameter's schema, so a trigger
  // or setting registered later gets a working field without a change here.
  import { createEventDispatcher } from 'svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { t } from '../../lib/i18n'
  import { entryLabel, type ScriptParam } from '../../lib/scripting'
  import { flagColors } from '../../theme/flagcolors'
  import { sidebar } from '../../stores/accounts'
  import { accountLabel } from '../../lib/format'
  import '../../theme/controls.css'

  /** The parameter's schema. */
  export let param: ScriptParam
  /** The current value; undefined shows the default. */
  export let value: unknown = undefined

  const dispatch = createEventDispatcher<{ change: unknown }>()
  const id = `param-${Math.random().toString(36).slice(2, 9)}`

  $: label = entryLabel($t, param)
  $: shown = value === undefined ? param.default : value
  $: text = shown === undefined || shown === null ? '' : Array.isArray(shown) ? shown.join(', ') : String(shown)
  $: enumItems = (param.options ?? []).map((o) => ({ value: o.value, label: entryLabel($t, o) }))
  $: accountItems = [
    { value: '0', label: $t('scripts.param.anyAccount') },
    ...($sidebar.data?.accounts ?? []).map((a) => ({ value: String(a.id), label: accountLabel(a) })),
  ]
  $: colorItems = [
    { value: '0', label: $t('scripts.param.noColor') },
    ...flagColors.map((c) => ({ value: String(c.index), label: $t(`flagColor.${c.name.toLowerCase()}`), color: c.hex })),
  ]
  $: chosen = Array.isArray(shown) ? (shown as unknown[]).map(String) : []

  function emit(v: unknown): void {
    dispatch('change', v)
  }

  function fromText(raw: string): unknown {
    switch (param.type) {
      case 'number':
        return raw.trim() === '' ? undefined : Number(raw)
      case 'list':
        return raw
          .split(',')
          .map((s) => s.trim())
          .filter((s) => s !== '')
      default:
        return raw
    }
  }

  function toggleOption(v: string): void {
    emit(chosen.includes(v) ? chosen.filter((x) => x !== v) : [...chosen, v])
  }
</script>

<div class="field">
  <label class="label" for={id}>{label}{#if param.required}<span class="req" aria-hidden="true">*</span>{/if}</label>
  {#if param.type === 'bool'}
    <ToggleSwitch {label} checked={shown === true} on:change={(e) => emit(e.detail)} />
  {:else if param.type === 'enum'}
    <Select {id} value={String(shown ?? '')} items={enumItems} on:change={(e) => emit(e.detail)} />
  {:else if param.type === 'account'}
    <Select {id} value={String(shown ?? 0)} items={accountItems} on:change={(e) => emit(Number(e.detail))} />
  {:else if param.type === 'color'}
    <Select {id} value={String(shown ?? 0)} items={colorItems} on:change={(e) => emit(Number(e.detail))} />
  {:else if param.type === 'list' && param.of === 'enum'}
    <div class="chips" role="group" aria-label={label} {id}>
      {#each param.options ?? [] as o (o.value)}
        <button type="button" class="cal-btn" class:on={chosen.includes(o.value)} aria-pressed={chosen.includes(o.value)} on:click={() => toggleOption(o.value)}>
          {entryLabel($t, o)}
        </button>
      {/each}
    </div>
  {:else if param.type === 'text'}
    <textarea {id} class="cal-field area" rows="3" value={text} on:change={(e) => emit(e.currentTarget.value)}></textarea>
  {:else if param.type === 'ref'}
    <p class="ref" {id}>{param.defaultRef ?? ''}</p>
  {:else}
    <input
      {id}
      class="cal-field"
      class:mono={param.type === 'regex' || param.type === 'duration'}
      type={param.type === 'number' ? 'number' : param.type === 'time' ? 'time' : 'text'}
      placeholder={param.type === 'duration' ? $t('scripts.param.durationHint') : param.type === 'folder' ? $t('scripts.param.folderHint') : ''}
      value={text}
      on:change={(e) => emit(fromText(e.currentTarget.value))}
    />
  {/if}
</div>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .req {
    margin-inline-start: var(--space-1);
    color: var(--text-tertiary);
  }

  .area {
    resize: vertical;
    font-family: var(--font-ui);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .ref {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
