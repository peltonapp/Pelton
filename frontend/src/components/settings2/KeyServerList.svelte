<script lang="ts">
  // the key servers the user added, with a field to add one. Only https
  // servers are accepted; the backend checks again when they are saved.
  import { createEventDispatcher } from 'svelte'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import '../../theme/controls.css'

  /** The servers, as typed; bindable. */
  export let servers: string[] = []

  const dispatch = createEventDispatcher<{ change: string[] }>()

  let draft = ''

  $: candidate = draft.trim().replace(/\/+$/, '')
  $: valid = /^(https:\/\/|hkps:\/\/)?[a-z0-9.-]+\.[a-z]{2,}(:\d+)?(\/\S*)?$/i.test(candidate) && !servers.includes(candidate)

  function add(): void {
    if (!valid) {
      return
    }
    servers = [...servers, candidate]
    draft = ''
    dispatch('change', servers)
  }

  function remove(server: string): void {
    servers = servers.filter((s) => s !== server)
    dispatch('change', servers)
  }
</script>

<div class="servers">
  {#if servers.length === 0}
    <p class="empty">{$t('autoEncrypt.servers.empty')}</p>
  {:else}
    <ul>
      {#each servers as server (server)}
        <li>
          <span class="name">{server}</span>
          <button type="button" class="cal-icon" aria-label={$t('autoEncrypt.servers.remove').replace('{server}', server)} on:click={() => remove(server)}>
            <Ph svg={xIcon} size={13} />
          </button>
        </li>
      {/each}
    </ul>
  {/if}
  <form class="add" on:submit|preventDefault={add}>
    <input
      class="cal-field"
      type="text"
      spellcheck="false"
      autocomplete="off"
      aria-label={$t('autoEncrypt.servers.label')}
      placeholder={$t('autoEncrypt.servers.placeholder')}
      bind:value={draft}
    />
    <button type="submit" class="cal-btn" disabled={!valid}>{$t('autoEncrypt.servers.add')}</button>
  </form>
</div>

<style>
  .servers {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .empty {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  ul {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-1) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-primary);
  }

  .add {
    display: flex;
    gap: var(--space-2);
  }

  .add input {
    flex: 1;
    min-width: 0;
  }
</style>
