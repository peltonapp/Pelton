<script lang="ts">
  // the small scan-result marker shown next to a scanned link or attachment.
  // a check for clean, a cross for flagged, a question mark for anything
  // VirusTotal has never analysed. hovering explains the result, and clicking
  // opens the full report in the external browser.
  import { openURL } from '../../lib/browser'
  import { IconCheck, IconX, IconQuestionMark, IconAlertTriangle } from '@tabler/icons-svelte'
  import { t } from '../../lib/i18n'
  import type { Verdict } from '../../lib/types'

  /** the result to render. */
  export let verdict: Verdict
  /** icon size in px, so a badge can sit in a dense row or on a card. */
  export let size = 13

  // a failed lookup is its own state: it says nothing about the target, so it
  // must not be shown as either clean or flagged.
  $: failed = verdict.error !== ''

  $: label = failed ? errorLabel(verdict.error) : verdictLabel(verdict)

  // a code the backend emits for the two failures worth explaining; anything
  // else is a message from the network layer and is shown as it came.
  function errorLabel(code: string): string {
    if (code === 'rate_limited') {
      return $t('virustotal.error.rateLimited')
    }
    if (code === 'unauthorized') {
      return $t('virustotal.error.unauthorized')
    }
    return code
  }

  function verdictLabel(v: Verdict): string {
    if (v.status === 'unknown') {
      return $t('virustotal.verdict.unknown')
    }
    const flagged = v.malicious + v.suspicious
    // $t has no interpolation, so the counts are composed here.
    return `${flagged}/${v.total} ${$t('virustotal.verdict.enginesFlagged')}`
  }

  function openReport(): void {
    if (verdict.permalink) {
      openURL(verdict.permalink)
    }
  }
</script>

<button
  type="button"
  class="badge"
  class:clean={!failed && verdict.status === 'clean'}
  class:flagged={!failed && verdict.status === 'flagged'}
  class:failed
  title={verdict.permalink ? `${label} - ${$t('virustotal.openReport')}` : label}
  aria-label={label}
  disabled={!verdict.permalink}
  on:click|stopPropagation={openReport}
>
  {#if failed}
    <IconAlertTriangle {size} stroke={1.9} />
  {:else if verdict.status === 'clean'}
    <IconCheck {size} stroke={2.2} />
  {:else if verdict.status === 'flagged'}
    <IconX {size} stroke={2.2} />
  {:else}
    <IconQuestionMark {size} stroke={2.2} />
  {/if}
</button>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    padding: 1px;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    /* unknown and any other unhandled state: deliberately neutral, since it is
       not a statement about the target either way. */
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    vertical-align: middle;
  }

  .badge:disabled {
    cursor: default;
  }

  .badge:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .badge.clean {
    color: var(--success);
  }

  .badge.flagged {
    color: var(--danger);
    background: var(--danger-bg);
  }

  .badge.failed {
    color: var(--warning);
  }
</style>
