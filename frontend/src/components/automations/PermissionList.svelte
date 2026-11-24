<script lang="ts">
  // the permissions a script declares, as the registry describes them: each
  // with its risk and what it allows. editable, every registered permission
  // shows with a checkbox; read only, just the declared ones with whether the
  // user approved them.
  import { createEventDispatcher } from 'svelte'
  import { t } from '../../lib/i18n'
  import { entryDescription, entryLabel, permissionDef, type ScriptCatalog } from '../../lib/scripting'

  /** The registry. */
  export let catalog: ScriptCatalog
  /** The declared permissions. */
  export let declared: string[]
  /** The approved permissions, shown in the read only list. */
  export let approved: string[] = []
  /** Lets the user declare and drop permissions. */
  export let editable = false
  /** A permission the trigger needs, which cannot be dropped. */
  export let locked = ''

  const dispatch = createEventDispatcher<{ change: string[] }>()

  $: rows = editable ? [...catalog.permissions.map((p) => p.id), ...declared.filter((p) => p.includes(':'))] : declared

  function toggle(id: string, on: boolean): void {
    dispatch('change', on ? [...declared, id] : declared.filter((p) => p !== id))
  }
</script>

{#if rows.length === 0}
  <p class="none">{$t('scripts.perm.none')}</p>
{:else}
  <ul class="perms">
    {#each rows as id (id)}
      {@const def = permissionDef(catalog, id)}
      <li class="perm">
        {#if editable}
          <input
            type="checkbox"
            id={`perm-${id}`}
            checked={declared.includes(id)}
            disabled={id === locked}
            on:change={(e) => toggle(id, e.currentTarget.checked)}
          />
        {/if}
        <label class="text" for={editable ? `perm-${id}` : undefined}>
          <span class="name">
            {def ? entryLabel($t, def) : id}
            {#if id.includes(':')}<code class="target">{id.slice(id.indexOf(':') + 1)}</code>{/if}
            {#if def}<span class="risk {def.risk}">{$t(`scripts.risk.${def.risk}`)}</span>{/if}
            {#if !editable && !approved.includes(id)}<span class="pending">{$t('scripts.perm.pending')}</span>{/if}
          </span>
          {#if def}<span class="desc">{entryDescription($t, def)}</span>{/if}
        </label>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .perms {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .perm {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
  }

  .perm input {
    margin-top: var(--space-1);
    accent-color: var(--accent);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .name {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .desc {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .target {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .risk,
  .pending {
    padding: 0 var(--space-1);
    border: var(--hairline) solid currentColor;
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
  }

  .risk.low {
    color: var(--text-tertiary);
  }

  .risk.medium,
  .pending {
    color: var(--warning);
  }

  .risk.high {
    color: var(--danger);
  }

  .none {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
