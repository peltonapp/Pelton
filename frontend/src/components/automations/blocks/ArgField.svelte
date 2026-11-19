<script lang="ts">
  // one parameter of a block or condition: a literal in the control its type
  // calls for, or a reference to a value in scope shown as a chip. the control
  // comes from the parameter's type alone; a type this editor does not know
  // gets a text field. a reference that is not in scope where the block now
  // stands stays, marked, until the user picks another or clears it.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import linkIcon from '@phosphor-icons/core/assets/duotone/brackets-curly-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/duotone/x-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import Select from '../../common/Select.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import RefMenu from './RefMenu.svelte'
  import { t } from '../../../lib/i18n'
  import { searchAddresses } from '../../../lib/api'
  import { accountLabel } from '../../../lib/format'
  import { entryDescription, entryLabel, type BlockArg, type ScriptParam } from '../../../lib/scripting'
  import { flagColors } from '../../../theme/flagcolors'
  import { sidebar } from '../../../stores/accounts'
  import { getEditor } from './context'
  import { re2Problem } from './re2'
  import { entriesFor, refInScope, refText, type ScopeEntry } from './scope'
  import '../../../theme/controls.css'

  /** The parameter's schema. */
  export let param: ScriptParam
  /** The argument as stored, or undefined when none is set. */
  export let arg: BlockArg | undefined
  /** The values in scope where the block stands. */
  export let scope: ScopeEntry[]
  /** A problem the editor found with this field, already in words, or ''. */
  export let problem = ''
  /** Makes the field's ids unique within the editor. */
  export let idBase: string

  const dispatch = createEventDispatcher<{ change: BlockArg | undefined }>()
  const { catalog, allEntries } = getEditor()
  const folderRoles = ['inbox', 'archive', 'trash', 'junk', 'sent', 'drafts']

  let refButton: HTMLButtonElement
  let menuOpen = false
  let people: string[] = []
  let peopleTimer: ReturnType<typeof setTimeout> | undefined

  $: id = `${idBase}-${param.name}`
  $: label = entryLabel($t, param)
  $: hint = entryDescription($t, param)
  $: fits = entriesFor(param, scope, $catalog)
  $: ref = arg?.ref ?? ''
  $: refOk = !ref || refInScope(ref, scope, $catalog)
  $: value = arg?.value
  $: shown = value === undefined ? param.default : value
  $: text = shown === undefined || shown === null ? '' : Array.isArray(shown) ? shown.join(', ') : String(shown)
  $: elem = param.type === 'list' ? (param.of ?? 'string') : param.type
  $: regexProblem = param.type === 'regex' && typeof value === 'string' && value !== '' ? re2Problem(value) : ''
  $: enumItems = (param.options ?? []).map((o) => ({ value: o.value, label: entryLabel($t, o) }))
  $: accountItems = [
    { value: '0', label: $t('scripts.param.anyAccount') },
    ...($sidebar.data?.accounts ?? []).map((a) => ({ value: String(a.id), label: accountLabel(a) })),
  ]
  $: colorItems = [
    { value: '0', label: $t('scripts.param.noColor') },
    ...flagColors.map((c) => ({ value: String(c.index), label: $t(`flagColor.${c.name.toLowerCase()}`), color: c.hex })),
  ]
  $: folders = [
    ...folderRoles,
    ...Object.values($sidebar.data?.foldersByAccount ?? {})
      .flat()
      .map((f) => f.imapPath || f.name),
  ].filter((f, i, all) => all.indexOf(f) === i)
  $: chosen = Array.isArray(shown) ? (shown as unknown[]).map(String) : []
  $: describedBy = [hint ? `${id}-hint` : '', problem || regexProblem || !refOk ? `${id}-problem` : ''].filter(Boolean).join(' ') || undefined
  $: invalid = !!problem || !!regexProblem || !refOk

  onDestroy(() => clearTimeout(peopleTimer))

  function convert(raw: string, type: string): unknown {
    if (type === 'number' || type === 'account' || type === 'color') {
      return raw.trim() === '' || Number.isNaN(Number(raw)) ? raw : Number(raw)
    }
    return raw
  }

  function setLiteral(v: unknown): void {
    dispatch('change', v === undefined || v === '' ? undefined : { value: v })
  }

  function fromText(raw: string): void {
    if (param.type === 'list') {
      const items = raw
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s !== '')
        .map((s) => convert(s, elem))
      setLiteral(items.length > 0 ? items : undefined)
      return
    }
    setLiteral(raw.trim() === '' ? undefined : convert(raw, param.type))
  }

  function toggleOption(v: string): void {
    const next = chosen.includes(v) ? chosen.filter((x) => x !== v) : [...chosen, v]
    setLiteral(next.length > 0 ? next : undefined)
  }

  function pickRef(r: string): void {
    menuOpen = false
    dispatch('change', { ref: r })
    refButton?.focus()
  }

  function lookup(q: string): void {
    clearTimeout(peopleTimer)
    peopleTimer = setTimeout(() => {
      searchAddresses(q, 8)
        .then((found) => (people = (found ?? []).map((p) => p.email)))
        .catch(() => (people = []))
    }, 150)
  }
</script>

<div class="field" class:invalid>
  <div class="top">
    <label class="label" for={id}>{label}{#if param.required}<span class="req" aria-hidden="true">*</span>{/if}</label>
    {#if fits.length > 0 || ref}
      <button
        type="button"
        class="ref-btn"
        class:on={!!ref}
        bind:this={refButton}
        aria-label={$t('scripts.blocks.ref.use').replace('{name}', label)}
        title={$t('scripts.blocks.ref.use').replace('{name}', label)}
        aria-haspopup="listbox"
        aria-expanded={menuOpen}
        on:click={() => (menuOpen = !menuOpen)}
      >
        <Ph svg={linkIcon} size={14} />
      </button>
    {/if}
  </div>

  {#if ref}
    <div class="chip" class:bad={!refOk} id={id} role="group" aria-label={`${label}: ${refText(ref, $allEntries, $t)}`} aria-describedby={describedBy}>
      <button type="button" class="chip-text" on:click={() => (menuOpen = true)}>{refText(ref, $allEntries, $t)}</button>
      <button type="button" class="chip-x" aria-label={$t('scripts.blocks.ref.clear')} on:click={() => dispatch('change', undefined)}>
        <Ph svg={xIcon} size={12} />
      </button>
    </div>
  {:else if param.type === 'ref'}
    <button type="button" class="chip muted" {id} aria-describedby={describedBy} on:click={() => (menuOpen = true)}>
      {param.defaultRef ? $t('scripts.blocks.ref.default').replace('{name}', refText(param.defaultRef, $allEntries, $t)) : $t('scripts.blocks.ref.pick')}
    </button>
  {:else if param.type === 'bool'}
    <ToggleSwitch {label} checked={shown === true} on:change={(e) => setLiteral(e.detail)} />
  {:else if param.type === 'enum'}
    <Select {id} value={String(shown ?? '')} items={enumItems} on:change={(e) => setLiteral(e.detail)} />
  {:else if param.type === 'account'}
    <Select {id} value={String(shown ?? 0)} items={accountItems} on:change={(e) => setLiteral(Number(e.detail))} />
  {:else if param.type === 'color'}
    <Select {id} value={String(shown ?? 0)} items={colorItems} on:change={(e) => setLiteral(Number(e.detail))} />
  {:else if param.type === 'list' && elem === 'enum'}
    <div class="chips" role="group" aria-label={label} {id}>
      {#each param.options ?? [] as o (o.value)}
        <button type="button" class="cal-btn" class:on={chosen.includes(o.value)} aria-pressed={chosen.includes(o.value)} on:click={() => toggleOption(o.value)}>
          {entryLabel($t, o)}
        </button>
      {/each}
    </div>
  {:else if param.type === 'text'}
    <textarea {id} class="cal-field area" rows="3" value={text} aria-invalid={invalid} aria-describedby={describedBy} on:change={(e) => fromText(e.currentTarget.value)}></textarea>
  {:else}
    <input
      {id}
      class="cal-field"
      class:mono={param.type === 'regex' || param.type === 'duration'}
      type={param.type === 'number' ? 'number' : param.type === 'time' ? 'time' : 'text'}
      list={elem === 'folder' ? `${id}-folders` : elem === 'address' ? `${id}-people` : undefined}
      placeholder={param.type === 'duration'
        ? $t('scripts.param.durationHint')
        : param.type === 'folder'
          ? $t('scripts.param.folderHint')
          : param.type === 'list'
            ? $t('scripts.blocks.listHint')
            : ''}
      value={text}
      aria-invalid={invalid}
      aria-describedby={describedBy}
      spellcheck={param.type === 'regex' ? false : undefined}
      on:input={(e) => {
        if (elem === 'address') lookup(e.currentTarget.value)
        if (param.type === 'regex') value = e.currentTarget.value
      }}
      on:change={(e) => fromText(e.currentTarget.value)}
    />
    {#if elem === 'folder'}
      <datalist id={`${id}-folders`}>
        {#each folders as f (f)}<option value={f}></option>{/each}
      </datalist>
    {:else if elem === 'address'}
      <datalist id={`${id}-people`}>
        {#each people as p (p)}<option value={p}></option>{/each}
      </datalist>
    {/if}
  {/if}

  {#if param.type === 'regex' && !ref}
    <p class="hint" id={`${id}-hint`}>{$t('scripts.blocks.regex.hint')}</p>
  {:else if hint}
    <p class="hint" id={`${id}-hint`}>{hint}</p>
  {/if}
  {#if !refOk}
    <p class="problem" id={`${id}-problem`}>{$t('scripts.blocks.issue.badRef')}</p>
  {:else if regexProblem}
    <p class="problem" id={`${id}-problem`}>{$t(regexProblem)}</p>
  {:else if problem}
    <p class="problem" id={`${id}-problem`}>{problem}</p>
  {/if}
</div>

{#if menuOpen && refButton}
  <RefMenu anchor={refButton} entries={fits} current={ref} on:pick={(e) => pickRef(e.detail)} on:close={() => (menuOpen = false)} />
{/if}

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-1);
    min-height: var(--chip-height);
  }

  .label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .req {
    margin-inline-start: var(--space-1);
    color: var(--text-tertiary);
  }

  .ref-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--chip-height);
    height: var(--chip-height);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .ref-btn:hover,
  .ref-btn.on {
    background: var(--surface-hover);
    color: var(--accent);
  }

  .ref-btn:focus-visible,
  .chip button:focus-visible,
  .chip.muted:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    align-self: flex-start;
    max-width: 100%;
    min-height: var(--control-height);
    border: var(--hairline) solid color-mix(in srgb, var(--accent) 40%, transparent);
    border-radius: var(--radius-control);
    background: var(--selection-bg);
    color: var(--text-primary);
    font-size: var(--fz-label);
  }

  .chip.bad {
    border-color: var(--danger);
    background: var(--danger-bg);
    color: var(--danger);
    text-decoration: line-through;
    text-decoration-thickness: var(--hairline);
  }

  .chip.muted {
    padding: 0 var(--space-2);
    border-style: dashed;
    border-color: var(--border-default);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .chip-text,
  .chip-x {
    display: inline-flex;
    align-items: center;
    min-height: var(--control-height);
    border: none;
    background: transparent;
    color: inherit;
    font: inherit;
    cursor: var(--cursor-action);
  }

  .chip-text {
    padding: 0 var(--space-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip-x {
    padding: 0 var(--space-1);
    color: var(--text-tertiary);
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .area {
    resize: vertical;
    font-family: var(--font-ui);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .invalid :global(.cal-field) {
    border-color: var(--danger);
  }

  .hint,
  .problem {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .problem {
    color: var(--danger);
  }
</style>
