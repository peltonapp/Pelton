<script lang="ts">
  // the menu of the app. in the window it is Pelton's own bar, with or without
  // an icon beside each entry; outside the window it is the system's bar along
  // the top of the screen.
  import fileIcon from '@phosphor-icons/core/assets/duotone/file-duotone.svg?raw'
  import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-duotone.svg?raw'
  import viewIcon from '@phosphor-icons/core/assets/duotone/eye-duotone.svg?raw'
  import Frame from './Frame.svelte'
  import MiniWindow from './MiniWindow.svelte'
  import Ph from '../../common/Ph.svelte'
  import { t } from '../../../lib/i18n'

  /** Draws the menu bar inside the window. */
  export let inApp: boolean = true
  /** Puts an icon beside each entry. */
  export let icons: boolean = false

  $: entries = [
    { key: 'file', label: $t('menu.file'), icon: fileIcon },
    { key: 'mailbox', label: $t('menu.mailbox'), icon: mailIcon },
    { key: 'view', label: $t('menu.view'), icon: viewIcon },
  ]
</script>

<Frame sunken>
  <div class="screen">
    {#if !inApp}
      <div class="menu system">
        {#each entries as e (e.key)}
          <span class="entry">{e.label}</span>
        {/each}
      </div>
    {/if}
    <div class="window">
      {#if inApp}
        <div class="menu">
          {#each entries as e (e.key)}
            <span class="entry">
              {#if icons}
                <Ph svg={e.icon} size={12} />
              {/if}
              {e.label}
            </span>
          {/each}
        </div>
      {/if}
      <div class="body"><MiniWindow /></div>
    </div>
  </div>
</Frame>

<style>
  .screen {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .window {
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-base);
    overflow: hidden;
  }

  .menu {
    display: flex;
    gap: var(--space-4);
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-default);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .menu.system {
    border: none;
    border-radius: var(--radius-control);
    background: var(--surface-hover);
  }

  .entry {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .body {
    height: 56px;
  }
</style>
