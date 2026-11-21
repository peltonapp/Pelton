<script lang="ts">
  // which profile you are in, in the status bar, and the switcher behind it
  // (#270). Sending work mail from the private profile is the mistake profiles
  // exist to prevent, so this says which one you are writing from at all times.
  import { IconCheck, IconUser } from '@tabler/icons-svelte'
  import { profiles, currentProfile, switching, switchTo } from '../../stores/profiles'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { t } from '../../lib/i18n'

  let open = false

  function pick(id: number): void {
    open = false
    void switchTo(id)
  }

  function manage(): void {
    open = false
    openSettingsAt('profiles')
  }

  // clicking anywhere else closes the list, the way the other status-bar popups
  // behave.
  function onWindowClick(event: MouseEvent): void {
    if (!(event.target as HTMLElement)?.closest('.profile-chip')) {
      open = false
    }
  }
</script>

<svelte:window on:click={onWindowClick} />

<div class="profile-chip">
  <button
    type="button"
    class="chip"
    class:busy={$switching}
    aria-haspopup="listbox"
    aria-expanded={open}
    title={$t('profiles.current').replace('{name}', $currentProfile?.name ?? '')}
    on:click={() => (open = !open)}
  >
    <span class="glyph">
      {#if $currentProfile?.icon}
        {$currentProfile.icon}
      {:else}
        <IconUser size={13} stroke={1.8} />
      {/if}
    </span>
    <span class="name">{$currentProfile?.name ?? ''}</span>
  </button>

  {#if open}
    <div class="menu" role="listbox" aria-label={$t('profiles.switch')}>
      {#each $profiles as profile (profile.id)}
        <button
          type="button"
          role="option"
          aria-selected={profile.id === $currentProfile?.id}
          class="item"
          on:click={() => pick(profile.id)}
        >
          <span class="tick">
            {#if profile.id === $currentProfile?.id}
              <IconCheck size={13} stroke={2} />
            {/if}
          </span>
          <span class="glyph">{profile.icon}</span>
          <span class="label">{profile.name}</span>
        </button>
      {/each}
      <div class="sep"></div>
      <button type="button" class="item" on:click={manage}>
        <span class="tick"></span>
        <span class="label">{$t('profiles.manage')}</span>
      </button>
    </div>
  {/if}
</div>

<style>
  .profile-chip {
    position: relative;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0 var(--space-2);
    height: 100%;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    border-radius: var(--radius-control);
  }
  .chip:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .chip.busy {
    opacity: 0.6;
  }

  .glyph {
    display: inline-flex;
    align-items: center;
  }

  .name {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .menu {
    position: absolute;
    bottom: calc(100% + var(--space-1));
    inset-inline-start: 0;
    z-index: 320;
    min-width: 180px;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    text-align: start;
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }
  .item:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tick {
    display: inline-flex;
    justify-content: center;
    width: 14px;
    flex-shrink: 0;
    color: var(--accent);
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sep {
    height: var(--hairline);
    margin: var(--space-1) 0;
    background: var(--border-subtle);
  }
</style>
