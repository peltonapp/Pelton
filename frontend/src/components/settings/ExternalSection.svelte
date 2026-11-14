<script lang="ts">
  // the External settings: integrations that talk to services or agents outside
  // Pelton. Today that is the read-only MCP server (off by default) an AI agent
  // can connect to on loopback with a bearer token; future external integrations
  // live under the same category.
  import { onMount } from 'svelte'
  import { openURL } from '../../lib/browser'
  import { IconCheck, IconCopy, IconRefresh, IconExternalLink } from '@tabler/icons-svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import {
    getMCPConfig,
    setMCPEnabled,
    setMCPPort,
    regenerateMCPToken,
    mcpPermissions,
    setMCPPermission,
    agentActions,
    clearAgentActions,
    setVirusTotalEnabled,
    setVirusTotalApiKey,
    setVirusTotalAutoScanLinks,
    setVirusTotalAutoScanAttachments,
  } from '../../lib/api'
  import { virusTotal, loadVirusTotalConfig } from '../../stores/virustotal'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { MCPConfig, MCPPermission, AgentAction } from '../../lib/types'

  // where a VirusTotal account's api key is found, linked from the key field so
  // the user is not left to hunt for it.
  const apiKeyPage = 'https://www.virustotal.com/gui/my-apikey'

  let cfg: MCPConfig = { enabled: false, port: 8765, token: '', url: '', running: false }
  let loading = true
  let busy = false
  // the key is typed locally and only sent on save; the backend never sends a
  // stored key back, so an empty field with hasApiKey set means "unchanged".
  let apiKeyDraft = ''
  let vtBusy = false
  // the port field is edited locally and only committed on save, so typing does
  // not restart the server on every keystroke.
  let portDraft = 8765

  // what an agent is allowed to do, per tool, and the record of what it did.
  let permissions: MCPPermission[] = []
  let actions: AgentAction[] = []
  let permBusy = false

  // the groups exist so the list reads as three kinds of risk rather than seven
  // switches. Enforcement is per tool either way.
  const groupOrder = ['organise', 'delete', 'send']
  $: permissionGroups = groupOrder
    .map((name) => ({ name, tools: permissions.filter((p) => p.group === name) }))
    .filter((g) => g.tools.length > 0)

  onMount(async () => {
    await Promise.all([reload(), loadVirusTotalConfig(), reloadAgent()])
    loading = false
  })

  async function reloadAgent(): Promise<void> {
    try {
      ;[permissions, actions] = await Promise.all([mcpPermissions(), agentActions()])
    } catch {
      // nothing to show is the safe way to be wrong here: it under-reports what
      // an agent may do rather than claiming a permission that is not set.
    }
  }

  async function setPermission(tool: string, allowed: boolean): Promise<void> {
    permBusy = true
    try {
      await setMCPPermission(tool, allowed)
      await reloadAgent()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      permBusy = false
    }
  }

  // setGroup flips every tool in a group. It is a convenience over the same
  // per-tool setter, not a separate permission.
  async function setGroup(group: { tools: MCPPermission[] }, allowed: boolean): Promise<void> {
    permBusy = true
    try {
      for (const tool of group.tools) {
        if (tool.allowed !== allowed) {
          await setMCPPermission(tool.tool, allowed)
        }
      }
      await reloadAgent()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      permBusy = false
    }
  }

  async function clearLog(): Promise<void> {
    permBusy = true
    try {
      await clearAgentActions()
      await reloadAgent()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      permBusy = false
    }
  }

  async function reload(): Promise<void> {
    try {
      cfg = await getMCPConfig()
      portDraft = cfg.port
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onToggle(enabled: boolean): Promise<void> {
    busy = true
    try {
      await setMCPEnabled(enabled)
      await reload()
    } catch (err) {
      toastError(errorMessage(err))
      await reload()
    } finally {
      busy = false
    }
  }

  async function savePort(): Promise<void> {
    busy = true
    try {
      await setMCPPort(portDraft)
      await reload()
      toastSuccess($t('mcp.saved'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function regenerate(): Promise<void> {
    busy = true
    try {
      await regenerateMCPToken()
      await reload()
      toastSuccess($t('mcp.tokenRegenerated'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function copy(text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text)
      toastSuccess($t('mcp.copied'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // runVT wraps a settings write so every VirusTotal control shares the same
  // busy handling and refetches the state the backend actually ended up in.
  async function runVT(change: () => Promise<void>): Promise<void> {
    vtBusy = true
    try {
      await change()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      await loadVirusTotalConfig()
      vtBusy = false
    }
  }

  async function saveApiKey(): Promise<void> {
    const key = apiKeyDraft.trim()
    await runVT(() => setVirusTotalApiKey(key))
    apiKeyDraft = ''
    toastSuccess(key === '' ? $t('virustotal.keyCleared') : $t('virustotal.keySaved'))
  }

  // the ready-to-paste client config for an MCP host that speaks streamable HTTP.
  $: clientConfig = JSON.stringify(
    {
      mcpServers: {
        pelton: {
          url: cfg.url,
          headers: { Authorization: `Bearer ${cfg.token}` },
        },
      },
    },
    null,
    2,
  )
</script>

<h3>{$t('settingsPanel.category.external')}</h3>
<p class="hint">{$t('external.hint')}</p>

{#if loading}
  <p class="hint">{$t('mailboxes.loading')}</p>
{:else}
  <div class="card">
    <div class="card-head">
      <div>
        <span class="card-title">{$t('mcp.title')}</span>
        <span class="badge" class:on={cfg.running}>{cfg.running ? $t('mcp.running') : $t('mcp.stopped')}</span>
      </div>
      <ToggleSwitch checked={cfg.enabled} label={$t('mcp.title')} disabled={busy} on:change={(e) => onToggle(e.detail)} />
    </div>
    <p class="hint">{$t('mcp.hint')}</p>
    <p class="hint">{$t('mcp.injectionHint')}</p>

    {#if cfg.enabled}
      <div class="servers">
        <label class="field narrow">
          <span>{$t('mcp.port')}</span>
          <input type="number" bind:value={portDraft} min="1024" max="65535" />
        </label>
        <button type="button" class="ghost" on:click={savePort} disabled={busy || portDraft === cfg.port}>
          <IconCheck size={14} stroke={2} />
          {$t('mcp.save')}
        </button>
      </div>

      <label class="field">
        <span>{$t('mcp.url')}</span>
        <div class="copy-row">
          <input type="text" readonly value={cfg.url} />
          <button type="button" class="ghost" on:click={() => copy(cfg.url)} aria-label={$t('mcp.copy')}>
            <IconCopy size={14} stroke={2} />
          </button>
        </div>
      </label>

      <label class="field">
        <span>{$t('mcp.token')}</span>
        <div class="copy-row">
          <input type="text" readonly value={cfg.token} />
          <button type="button" class="ghost" on:click={() => copy(cfg.token)} aria-label={$t('mcp.copy')}>
            <IconCopy size={14} stroke={2} />
          </button>
          <button type="button" class="ghost" on:click={regenerate} disabled={busy} aria-label={$t('mcp.regenerate')}>
            <IconRefresh size={14} stroke={2} />
          </button>
        </div>
        <p class="hint">{$t('mcp.tokenHint')}</p>
      </label>

      <div class="field">
        <span>{$t('mcp.clientConfig')}</span>
        <p class="hint">{$t('mcp.clientConfigHint')}</p>
        <pre>{clientConfig}</pre>
        <button type="button" class="ghost self-start" on:click={() => copy(clientConfig)}>
          <IconCopy size={14} stroke={2} />
          {$t('mcp.copy')}
        </button>
      </div>

      <!-- what an agent may do beyond reading. Every tool is its own switch:
           filing your mail and mailing your contacts are not the same risk, and
           one permission should never imply another. -->
      <div class="field">
        <span>{$t('mcp.permissions')}</span>
        <p class="hint">{$t('mcp.permissionsHint')}</p>

        {#each permissionGroups as group (group.name)}
          <div class="perm-group">
            <div class="perm-head">
              <span class="perm-title">{$t(`mcp.group.${group.name}`)}</span>
              <button
                type="button"
                class="ghost small"
                disabled={permBusy}
                on:click={() => void setGroup(group, !group.tools.every((t) => t.allowed))}
              >
                {group.tools.every((t) => t.allowed) ? $t('mcp.group.none') : $t('mcp.group.all')}
              </button>
            </div>
            <p class="hint">{$t(`mcp.group.${group.name}.hint`)}</p>
            {#each group.tools as tool (tool.tool)}
              <div class="perm-row">
                <span class="row-label">{$t(`mcp.tool.${tool.tool}`)}</span>
                <ToggleSwitch
                  checked={tool.allowed}
                  label={$t(`mcp.tool.${tool.tool}`)}
                  disabled={permBusy}
                  on:change={(e) => void setPermission(tool.tool, e.detail)}
                />
              </div>
            {/each}
          </div>
        {/each}
      </div>

      <div class="field">
        <span>{$t('mcp.log')}</span>
        <p class="hint">{$t('mcp.logHint')}</p>
        {#if actions.length === 0}
          <p class="hint">{$t('mcp.logEmpty')}</p>
        {:else}
          <ul class="log">
            {#each actions as entry (entry.id)}
              <li class:failed={entry.error !== ''}>
                <span class="log-when">{entry.when}</span>
                <span class="log-what">{entry.summary}</span>
                {#if entry.error}
                  <span class="log-error">{entry.error}</span>
                {/if}
              </li>
            {/each}
          </ul>
          <button type="button" class="ghost self-start" disabled={permBusy} on:click={() => void clearLog()}>
            {$t('mcp.logClear')}
          </button>
        {/if}
      </div>
    {/if}
  </div>

  <div class="card">
    <div class="card-head">
      <span class="card-title">{$t('virustotal.title')}</span>
      <ToggleSwitch
        checked={$virusTotal.enabled}
        label={$t('virustotal.title')}
        disabled={vtBusy}
        on:change={(e) => runVT(() => setVirusTotalEnabled(e.detail))}
      />
    </div>
    <p class="hint">{$t('virustotal.hint')}</p>

    {#if $virusTotal.enabled}
      <label class="field">
        <span>{$t('virustotal.apiKey')}</span>
        <div class="copy-row">
          <input
            type="password"
            bind:value={apiKeyDraft}
            autocomplete="off"
            placeholder={$virusTotal.hasApiKey ? $t('virustotal.keyStored') : $t('virustotal.keyPlaceholder')}
          />
          <button type="button" class="ghost" on:click={saveApiKey} disabled={vtBusy || (apiKeyDraft.trim() === '' && !$virusTotal.hasApiKey)}>
            <IconCheck size={14} stroke={2} />
            {$t('virustotal.saveKey')}
          </button>
        </div>
        <p class="hint">
          {$t('virustotal.apiKeyHint')}
          <button type="button" class="link" on:click={() => openURL(apiKeyPage)}>
            {$t('virustotal.getApiKey')}
            <IconExternalLink size={12} stroke={1.8} />
          </button>
        </p>
      </label>

      {#if $virusTotal.hasApiKey}
        <div class="toggle-row">
          <div class="toggle-text">
            <span>{$t('virustotal.autoScanLinks')}</span>
            <p class="hint">{$t('virustotal.autoScanLinksHint')}</p>
          </div>
          <ToggleSwitch
            checked={$virusTotal.autoScanLinks}
            label={$t('virustotal.autoScanLinks')}
            disabled={vtBusy}
            on:change={(e) => runVT(() => setVirusTotalAutoScanLinks(e.detail))}
          />
        </div>

        <div class="toggle-row">
          <div class="toggle-text">
            <span>{$t('virustotal.autoScanAttachments')}</span>
            <p class="hint">{$t('virustotal.autoScanAttachmentsHint')}</p>
          </div>
          <ToggleSwitch
            checked={$virusTotal.autoScanAttachments}
            label={$t('virustotal.autoScanAttachments')}
            disabled={vtBusy}
            on:change={(e) => runVT(() => setVirusTotalAutoScanAttachments(e.detail))}
          />
        </div>

        <p class="hint last">{$t('virustotal.privacyNote')}</p>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .perm-group {
    margin-top: var(--space-3);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .perm-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .perm-title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .perm-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-1) 0;
  }

  .ghost.small {
    padding: var(--space-1) var(--space-2);
    font-size: var(--fz-meta);
  }

  .log {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: var(--space-2) 0;
    padding: 0;
    max-height: 220px;
    overflow-y: auto;
    list-style: none;
  }

  .log li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
  }
  .log li.failed {
    background: var(--danger-bg);
  }

  .log-when {
    flex-shrink: 0;
    color: var(--text-tertiary);
    font-family: var(--font-mono);
  }

  .log-what {
    color: var(--text-secondary);
  }

  .log-error {
    color: var(--danger);
  }

  h3 {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .hint {
    margin: 0 0 var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .card {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    padding: var(--space-4);
    background: var(--surface-raised);
  }

  .card + .card {
    margin-top: var(--space-4);
  }

  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    margin-bottom: var(--space-2);
  }

  .card-title {
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .badge {
    margin-inline-start: var(--space-2);
    padding: 2px var(--space-2);
    border-radius: var(--radius-pill, 999px);
    font-size: var(--fz-caption, 0.72rem);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
  }

  .badge.on {
    background: color-mix(in srgb, var(--accent) 18%, transparent);
    color: var(--accent);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-bottom: var(--space-3);
  }

  .field span {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .field input,
  .copy-row input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    outline: none;
    width: 100%;
  }

  .field input:focus {
    border-color: var(--accent);
  }

  .servers {
    display: flex;
    align-items: flex-end;
    gap: var(--space-3);
  }

  .servers .field.narrow {
    flex: 0 0 120px;
    margin-bottom: 0;
  }

  .copy-row {
    display: flex;
    gap: var(--space-2);
  }

  pre {
    margin: 0;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-caption, 0.75rem);
    overflow-x: auto;
    white-space: pre;
  }

  .ghost {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border-radius: var(--radius-control);
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    font-size: var(--fz-label);
    white-space: nowrap;
  }

  .ghost:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .self-start {
    align-self: flex-start;
  }

  .toggle-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .toggle-text {
    min-width: 0;
  }

  .toggle-text span {
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .toggle-text .hint {
    margin: 2px 0 0;
  }

  .hint.last {
    margin: var(--space-3) 0 0;
  }

  /* an inline text link that opens in the external browser; a button rather
     than an anchor so nothing can navigate the app's own window. */
  .link {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font-size: inherit;
    cursor: var(--cursor-action);
    text-decoration: underline;
  }
</style>
