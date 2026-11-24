<script lang="ts">
  // the whole sieve script as text, for people who write it themselves and for
  // scripts Pelton cannot show as rules. the server checks it before it is
  // saved, and an error it reports is pinned beside the line it names.
  import { createEventDispatcher, tick } from 'svelte'
  import PieProgress from '../../common/PieProgress.svelte'
  import { t } from '../../../lib/i18n'

  /** The script as it is on the server. */
  export let source: string
  /** True while a check or save is running. */
  export let busy: boolean = false
  /** The server's objection to the text, '' for none. */
  export let problem: string = ''
  /** The line the objection names, 0 for none. */
  export let line: number = 0
  /** True once the server accepted the text as it stands. */
  export let valid: boolean = false

  const dispatch = createEventDispatcher<{ check: string; save: string; edit: void }>()

  let text = source
  let lastSource = source
  $: if (source !== lastSource) {
    lastSource = source
    text = source
  }

  let area: HTMLTextAreaElement
  let scrollTop = 0
  let lineHeight = 18
  let padTop = 0

  $: lines = text.split('\n').length
  $: dirty = text.replace(/\r\n/g, '\n') !== source.replace(/\r\n/g, '\n')
  $: bubbleTop = padTop + line * lineHeight - scrollTop

  function measure(): void {
    if (!area) {
      return
    }
    const style = getComputedStyle(area)
    const lh = parseFloat(style.lineHeight)
    if (Number.isFinite(lh) && lh > 0) {
      lineHeight = lh
    }
    const pad = parseFloat(style.paddingTop)
    padTop = Number.isFinite(pad) ? pad : 0
  }

  async function onInput(): Promise<void> {
    dispatch('edit')
    await tick()
    measure()
  }

  $: if (line > 0 && area) {
    measure()
  }
</script>

<div class="raw">
  <div class="editor">
    <div class="gutter" aria-hidden="true" style={`transform: translateY(${-scrollTop}px)`}>
      {#each Array.from({ length: lines }, (_, i) => i + 1) as n (n)}
        <span class:bad={n === line}>{n}</span>
      {/each}
    </div>
    <textarea
      bind:this={area}
      bind:value={text}
      class="code"
      rows="16"
      spellcheck="false"
      autocapitalize="off"
      aria-label={$t('sieve.raw.label')}
      aria-invalid={problem !== ''}
      on:input={onInput}
      on:scroll={() => (scrollTop = area.scrollTop)}
    ></textarea>
    {#if problem && line > 0}
      <div class="bubble" role="alert" style={`top: ${bubbleTop}px`}>
        <strong>{$t('sieve.raw.line').replace('{n}', String(line))}</strong>
        <span>{problem}</span>
      </div>
    {/if}
  </div>
  <div class="sv-bar">
    {#if problem && line === 0}
      <p class="sv-problem" role="alert">{problem}</p>
    {:else if valid && !problem}
      <p class="sv-note">{$t('sieve.raw.valid')}</p>
    {/if}
    {#if busy}
      <PieProgress label={$t('sieve.working')} />
    {/if}
    {#if dirty}
      <button type="button" class="s2-btn quiet" disabled={busy} on:click={() => (text = source)}>{$t('sieve.discard')}</button>
    {/if}
    <button type="button" class="s2-btn" disabled={busy} on:click={() => dispatch('check', text)}>{$t('sieve.raw.check')}</button>
    <button type="button" class="s2-btn primary" disabled={busy || !dirty} on:click={() => dispatch('save', text)}>
      {$t('sieve.raw.save')}
    </button>
  </div>
</div>

<style>
  .raw {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .editor {
    position: relative;
    display: flex;
    overflow: hidden;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-base);
  }

  .gutter,
  .code {
    font-family: var(--font-mono);
    font-size: var(--fz-label);
    line-height: 1.6;
  }

  .gutter {
    display: flex;
    flex-direction: column;
    flex-shrink: 0;
    min-width: 3.5ch;
    padding: var(--space-2) var(--space-2);
    border-inline-end: var(--hairline) solid var(--border-subtle);
    text-align: end;
    color: var(--text-tertiary);
    user-select: none;
    -webkit-user-select: none;
  }

  .gutter .bad {
    color: var(--danger);
    font-weight: var(--fw-semibold);
  }

  .code {
    flex: 1;
    min-width: 0;
    padding: var(--space-2) var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-primary);
    resize: vertical;
    white-space: pre;
    overflow-wrap: normal;
    overflow-x: auto;
    tab-size: 4;
  }

  .code:focus {
    outline: none;
  }

  .bubble {
    position: absolute;
    inset-inline-end: var(--space-3);
    max-width: 60%;
    display: flex;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-3);
    border-inline-start: 2px solid var(--danger);
    border-radius: var(--radius-control);
    background: var(--danger-bg);
    color: var(--text-primary);
    font-size: var(--fz-meta);
    box-shadow: var(--shadow-overlay);
    pointer-events: none;
  }

  .bubble strong {
    color: var(--danger);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
  }
</style>
