<script lang="ts">
  // what a scanned source holds, with a checkbox for each part: accounts,
  // mail folders as a tree with their counts, address books, calendars and
  // filters, and a list of what will not come over and why.
  import { formatBytes } from '../../lib/format'
  import { canImportAccount, folderRows, type ImportInventory, type ImportSelection } from '../../lib/guidedimport'
  import { t } from '../../lib/i18n'

  /** The scanned source. */
  export let inventory: ImportInventory
  /** The picks, changed in place and handed back through binding. */
  export let selection: ImportSelection

  $: rows = folderRows(inventory.folders)

  type Group = keyof ImportSelection

  function toggle(group: Group, key: string, on: boolean): void {
    const next = new Set(selection[group])
    if (on) {
      next.add(key)
    } else {
      next.delete(key)
    }
    selection = { ...selection, [group]: next }
  }

  function setAll(group: Group, keys: string[], on: boolean): void {
    selection = { ...selection, [group]: on ? new Set(keys) : new Set<string>() }
  }

  $: accountKeys = inventory.accounts.filter(canImportAccount).map((a) => a.email)
  $: folderKeys = inventory.folders.map((f) => f.key)
  $: filterKeys = inventory.filters.filter((f) => f.mappable).map((f) => f.key)

  function accountNote(a: ImportInventory['accounts'][number]): string {
    if (a.exists) {
      return $t('import.guided.account.exists')
    }
    if (a.kind !== 'imap') {
      return $t('import.guided.account.pop3')
    }
    if (a.signInWithProvider) {
      return $t('import.guided.account.appPassword')
    }
    return `${a.imapHost}${a.imapPort ? ':' + a.imapPort : ''}`
  }

  function countLabel(n: number): string {
    return n === 1 ? $t('import.guided.oneItem') : $t('import.guided.nItems').replace('{n}', n.toLocaleString())
  }

  function noticeText(code: string, name: string): string {
    const text = $t(`import.guided.notice.${code}`)
    return name ? text.replace('{name}', name) : text.replace('{name}', '')
  }
</script>

<div class="summary">
  {#if inventory.accounts.length > 0}
    <fieldset>
      <legend>
        <span>{$t('import.guided.section.accounts')}</span>
        {#if accountKeys.length > 1}
          <button type="button" class="all" on:click={() => setAll('accounts', accountKeys, selection.accounts.size < accountKeys.length)}>
            {selection.accounts.size < accountKeys.length ? $t('import.tb.all') : $t('import.tb.none_')}
          </button>
        {/if}
      </legend>
      <p class="hint">{$t('import.guided.account.hint')}</p>
      {#each inventory.accounts as a (a.email)}
        <label class="row" class:off={!canImportAccount(a)}>
          <input
            type="checkbox"
            checked={selection.accounts.has(a.email)}
            disabled={!canImportAccount(a)}
            on:change={(e) => toggle('accounts', a.email, e.currentTarget.checked)}
          />
          <span class="name">{a.email}</span>
          <span class="meta">{accountNote(a)}</span>
        </label>
      {/each}
    </fieldset>
  {/if}

  {#if rows.length > 0}
    <fieldset>
      <legend>
        <span>{$t('import.guided.section.mail')}</span>
        {#if folderKeys.length > 1}
          <button type="button" class="all" on:click={() => setAll('folders', folderKeys, selection.folders.size < folderKeys.length)}>
            {selection.folders.size < folderKeys.length ? $t('import.tb.all') : $t('import.tb.none_')}
          </button>
        {/if}
      </legend>
      <div class="tree">
        {#each rows as row (row.folder.key)}
          <label class="row" style:padding-inline-start="calc({row.depth} * var(--space-4))">
            <input
              type="checkbox"
              checked={selection.folders.has(row.folder.key)}
              on:change={(e) => toggle('folders', row.folder.key, e.currentTarget.checked)}
              aria-label={row.folder.path.join(' / ') || $t('import.guided.looseMessages')}
            />
            <span class="name">{row.label || $t('import.guided.looseMessages')}</span>
            {#if row.folder.serverCopy}
              <span class="tag">{$t('import.guided.serverCopy')}</span>
            {/if}
            <span class="meta">{countLabel(row.folder.count)} · {formatBytes(row.folder.sizeBytes)}</span>
          </label>
        {/each}
      </div>
      {#if inventory.folders.some((f) => f.serverCopy)}
        <p class="hint">{$t('import.guided.serverCopyHint')}</p>
      {/if}
    </fieldset>
  {/if}

  {#if inventory.addressBooks.length > 0}
    <fieldset>
      <legend><span>{$t('import.guided.section.contacts')}</span></legend>
      {#each inventory.addressBooks as b (b.key)}
        <label class="row">
          <input
            type="checkbox"
            checked={selection.addressBooks.has(b.key)}
            on:change={(e) => toggle('addressBooks', b.key, e.currentTarget.checked)}
          />
          <span class="name">{b.name}</span>
          <span class="meta">{countLabel(b.count)}</span>
        </label>
      {/each}
    </fieldset>
  {/if}

  {#if inventory.calendars.length > 0}
    <fieldset>
      <legend><span>{$t('import.guided.section.calendars')}</span></legend>
      {#each inventory.calendars as c (c.key)}
        <label class="row">
          <input
            type="checkbox"
            checked={selection.calendars.has(c.key)}
            on:change={(e) => toggle('calendars', c.key, e.currentTarget.checked)}
          />
          <span class="name">{c.name}</span>
          <span class="meta">{countLabel(c.count)}</span>
        </label>
      {/each}
    </fieldset>
  {/if}

  {#if inventory.filters.length > 0}
    <fieldset>
      <legend>
        <span>{$t('import.guided.section.filters')}</span>
        {#if filterKeys.length > 1}
          <button type="button" class="all" on:click={() => setAll('filters', filterKeys, selection.filters.size < filterKeys.length)}>
            {selection.filters.size < filterKeys.length ? $t('import.tb.all') : $t('import.tb.none_')}
          </button>
        {/if}
      </legend>
      <p class="hint">{$t('import.guided.filters.hint')}</p>
      {#each inventory.filters as f (f.key)}
        <div class="filter">
          <label class="row" class:off={!f.mappable}>
            <input
              type="checkbox"
              checked={selection.filters.has(f.key)}
              disabled={!f.mappable}
              on:change={(e) => toggle('filters', f.key, e.currentTarget.checked)}
            />
            <span class="name">{f.name}</span>
            <span class="meta">{f.account}</span>
          </label>
          {#if f.unmappable.length > 0}
            <p class="dropped">
              {f.mappable ? $t('import.guided.filters.partly') : $t('import.guided.filters.none')}
              {f.unmappable.join('; ')}
            </p>
          {/if}
        </div>
      {/each}
    </fieldset>
  {/if}

  {#if inventory.notices.length > 0}
    <section class="notices" aria-labelledby="import-notices">
      <h3 id="import-notices">{$t('import.guided.section.notices')}</h3>
      <ul>
        {#each inventory.notices as n, i (i)}
          <li>{noticeText(n.code, n.name)}</li>
        {/each}
      </ul>
    </section>
  {/if}
</div>

<style>
  .summary {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  fieldset {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    border: none;
    min-width: 0;
  }

  legend {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
    width: 100%;
    padding: 0 0 var(--space-1);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .all {
    border: none;
    background: none;
    padding: 0;
    font-size: var(--fz-meta);
    color: var(--link);
    cursor: var(--cursor-action);
  }

  .tree {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    max-height: 16rem;
    overflow-y: auto;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    padding-block: var(--space-1);
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .row.off {
    color: var(--text-tertiary);
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    margin-inline-start: auto;
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .tag {
    flex-shrink: 0;
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .hint,
  .dropped {
    margin: 0;
    font-size: var(--fz-meta);
    line-height: 1.4;
    color: var(--text-tertiary);
  }

  .dropped {
    padding-inline-start: calc(var(--space-4) + var(--space-2));
  }

  .notices {
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
  }

  .notices h3 {
    margin: 0 0 var(--space-1);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .notices ul {
    margin: 0;
    padding-inline-start: var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }
</style>
