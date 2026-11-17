<script lang="ts">
  // what an assistant connected over MCP left on a script for the user: the
  // notice on a script it wrote, and a change it proposed, shown as a diff to
  // accept or reject. nothing an assistant sends changes a script the user
  // wrote, or one that is on, until it is accepted here.
  import { onDestroy } from 'svelte'
  import { t } from '../../lib/i18n'
  import { acceptScriptProposal, rejectScriptProposal, scriptProposal } from '../../lib/api'
  import { onAssistantScript } from '../../lib/events'
  import type { ScriptProposal } from '../../lib/assistantScripts'
  import type { Script } from '../../lib/scripting'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'

  /** The script open in the editor. */
  export let script: Script

  let proposal: ScriptProposal | null = null
  let busy = false
  let loadedFor = -1

  $: if (script.id !== loadedFor) {
    loadedFor = script.id
    proposal = null
    void load(script.id)
  }

  async function load(id: number): Promise<void> {
    try {
      const p = await scriptProposal(id)
      if (id === script.id) {
        proposal = p.exists ? p : null
      }
    } catch {
      proposal = null
    }
  }

  const stop = onAssistantScript((e) => {
    if (e.scriptId === script.id) {
      void load(script.id)
    }
  })
  onDestroy(stop)

  async function settle(accept: boolean): Promise<void> {
    busy = true
    try {
      if (accept) {
        await acceptScriptProposal(script.id)
      } else {
        await rejectScriptProposal(script.id)
      }
      proposal = null
      toastSuccess($t(accept ? 'scripts.assistant.accepted' : 'scripts.assistant.rejected'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }
</script>

{#if script.origin === 'assistant'}
  <p class="notice">{$t('scripts.origin.assistant.notice')}</p>
{/if}

{#if proposal}
  <section class="proposal" aria-label={$t('scripts.assistant.diff')}>
    <p class="notice">{$t('scripts.assistant.proposal')}</p>
    <p class="meta">{$t('scripts.assistant.proposedAt').replace('{when}', proposal.when)}</p>
    {#if proposal.addedPermissions.length > 0}
      <p class="meta added">{$t('scripts.assistant.added').replace('{list}', proposal.addedPermissions.join(', '))}</p>
    {/if}
    {#if proposal.removedPermissions.length > 0}
      <p class="meta">{$t('scripts.assistant.removed').replace('{list}', proposal.removedPermissions.join(', '))}</p>
    {/if}
    {#if proposal.resetsApproval}
      <p class="meta">{$t('scripts.assistant.resets')}</p>
    {/if}
    {#if proposal.problem}
      <p class="meta problem" role="alert">{proposal.problem}</p>
    {/if}
    <pre class="diff">{#each proposal.diff as line, i (i)}<span class="line" class:add={line.op === '+'} class:del={line.op === '-'}>{line.op} {line.text}
</span>{/each}</pre>
    <div class="actions">
      <button type="button" class="cal-btn" disabled={busy} on:click={() => void settle(false)}>{$t('scripts.assistant.reject')}</button>
      <button type="button" class="cal-btn primary" disabled={busy || proposal.problem !== ''} on:click={() => void settle(true)}>{$t('scripts.assistant.accept')}</button>
    </div>
  </section>
{/if}

<style>
  .notice {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-label);
  }

  .proposal {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .meta {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .meta.added,
  .meta.problem {
    color: var(--danger);
  }

  .diff {
    margin: 0;
    max-height: 320px;
    overflow: auto;
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    white-space: pre;
  }

  .line.add {
    background: var(--success-bg);
  }

  .line.del {
    background: var(--danger-bg);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
</style>
