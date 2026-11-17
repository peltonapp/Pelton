<script lang="ts">
  // where a script started by hand is offered in the app: context menus, the
  // command palette, toolbars, a shortcut, the row buttons and swipes. the
  // places come from the registry, and only those whose value the script's
  // trigger takes are listed. a place's options, such as a toolbar button's
  // icon and label, are set beside its switch. it is saved as it is changed.
  import { createEventDispatcher } from 'svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import ParamField from './ParamField.svelte'
  import ScriptShortcutField from './ScriptShortcutField.svelte'
  import ScriptIconPicker from './ScriptIconPicker.svelte'
  import { t } from '../../lib/i18n'
  import { entryDescription, entryLabel, placementFits, type PlacementConfig, type Script, type ScriptCatalog, type ScriptPlacementDef } from '../../lib/scripting'
  import { setPlacements } from '../../stores/scripts'
  import { errorMessage, toastError } from '../../stores/toast'

  /** The script, as saved. */
  export let script: Script
  /** The registry. */
  export let catalog: ScriptCatalog

  const dispatch = createEventDispatcher<{ saved: void }>()

  $: fitting = catalog.placements.filter((p) => placementFits(p, script))
  $: configs = new Map(script.placements.map((c) => [c.id, c]))

  async function save(next: PlacementConfig[]): Promise<void> {
    try {
      await setPlacements(script.id, next)
      dispatch('saved')
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function defaults(p: ScriptPlacementDef): Record<string, unknown> {
    const out: Record<string, unknown> = {}
    for (const o of p.options ?? []) {
      if (o.type === 'icon') {
        out[o.name] = o.default ?? 'lightning'
      } else if (o.name === 'label') {
        out[o.name] = o.default ?? script.name
      } else if (o.default !== undefined) {
        out[o.name] = o.default
      }
    }
    return out
  }

  function toggle(p: ScriptPlacementDef, on: boolean): void {
    const rest = script.placements.filter((c) => c.id !== p.id)
    void save(on ? [...rest, { id: p.id, options: defaults(p) }] : rest)
  }

  function setOption(p: ScriptPlacementDef, name: string, value: unknown): void {
    void save(script.placements.map((c) => (c.id === p.id ? { ...c, options: { ...(c.options ?? {}), [name]: value } } : c)))
  }
</script>

{#if script.trigger.type !== 'manual'}
  <p class="hint">{$t('scripts.place.manualOnly')}</p>
{:else}
  {#if !script.enabled}
    <p class="hint">{$t('scripts.place.offWhileDisabled')}</p>
  {/if}
  <ul class="places">
    {#each fitting as p (p.id)}
      {@const config = configs.get(p.id)}
      <li class="place">
        <div class="head">
          <ToggleSwitch label={entryLabel($t, p)} checked={!!config} on:change={(e) => toggle(p, e.detail)} />
          <div class="text">
            <span class="name">{entryLabel($t, p)}</span>
            <span class="desc">{entryDescription($t, p)}</span>
          </div>
        </div>
        {#if config}
          {#each p.options ?? [] as o (o.name)}
            {#if o.type === 'icon'}
              <ScriptIconPicker label={entryLabel($t, o)} value={config.options?.[o.name]} on:change={(e) => setOption(p, o.name, e.detail)} />
            {:else}
              <div class="option"><ParamField param={o} value={config.options?.[o.name]} on:change={(e) => setOption(p, o.name, e.detail)} /></div>
            {/if}
          {/each}
          {#if p.surface === 'shortcut'}
            <ScriptShortcutField scriptId={script.id} />
          {/if}
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .places {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .place {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-block-end: var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .head {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
  }

  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .name {
    font-size: var(--fz-label);
  }

  .desc,
  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .option {
    max-width: calc(var(--control-height) * 10);
  }
</style>
