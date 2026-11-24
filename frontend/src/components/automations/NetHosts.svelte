<script lang="ts">
  // adds a web host a script may send requests to, declared as "net:<host>".
  // each host is approved with the rest of the script's permissions and shows
  // in the permission list, where it can be dropped again.
  import { createEventDispatcher } from 'svelte'
  import { t } from '../../lib/i18n'

  /** The declared permissions. */
  export let declared: string[]

  const dispatch = createEventDispatcher<{ change: string[] }>()
  let host = ''

  // the host part of an address or a bare host name, lower case, in the
  // characters a permission target allows.
  function hostOf(text: string): string {
    let s = text.trim().toLowerCase()
    try {
      if (s.includes('://')) {
        s = new URL(s).hostname
      }
    } catch {
      return ''
    }
    s = s.replace(/\/.*$/, '').replace(/:\d+$/, '')
    return /^[a-z0-9_.-]+$/.test(s) ? s : ''
  }

  $: clean = hostOf(host)

  function add(): void {
    const perm = `net:${clean}`
    if (clean && !declared.includes(perm)) {
      dispatch('change', [...declared, perm])
    }
    host = ''
  }
</script>

<form class="hosts" on:submit|preventDefault={add}>
  <label class="label" for="net-host">{$t('scripts.net.label')}</label>
  <div class="row">
    <input id="net-host" type="text" bind:value={host} placeholder={$t('scripts.net.placeholder')} spellcheck="false" autocomplete="off" />
    <button type="submit" class="cal-btn" disabled={!clean}>{$t('scripts.perm.add')}</button>
  </div>
  <p class="hint">{$t('scripts.net.hint')}</p>
</form>

<style>
  .hosts {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin-top: var(--space-2);
  }

  .label {
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .row {
    display: flex;
    gap: var(--space-2);
  }

  .row input {
    flex: 1;
    min-width: 0;
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
