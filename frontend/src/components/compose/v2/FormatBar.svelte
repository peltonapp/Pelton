<script lang="ts">
  // the composer's one slim formatting bar. it drives the rich editor through
  // its run() and isActive(), and re-reads the active marks whenever the
  // editor reports a transaction (revision).
  import boldIcon from '@phosphor-icons/core/assets/duotone/text-b-duotone.svg?raw'
  import italicIcon from '@phosphor-icons/core/assets/duotone/text-italic-duotone.svg?raw'
  import underlineIcon from '@phosphor-icons/core/assets/duotone/text-underline-duotone.svg?raw'
  import strikeIcon from '@phosphor-icons/core/assets/duotone/text-strikethrough-duotone.svg?raw'
  import bulletIcon from '@phosphor-icons/core/assets/duotone/list-bullets-duotone.svg?raw'
  import numberIcon from '@phosphor-icons/core/assets/duotone/list-numbers-duotone.svg?raw'
  import quoteIcon from '@phosphor-icons/core/assets/duotone/quotes-duotone.svg?raw'
  import linkIcon from '@phosphor-icons/core/assets/duotone/link-duotone.svg?raw'
  import codeIcon from '@phosphor-icons/core/assets/duotone/code-duotone.svg?raw'
  import clearIcon from '@phosphor-icons/core/assets/duotone/eraser-duotone.svg?raw'
  import { tick } from 'svelte'
  import Ph from '../../common/Ph.svelte'
  import { tip } from '../../../lib/tip'
  import { shortcutLabel, t } from '../../../lib/i18n'
  import type RichEditor from '../RichEditor.svelte'

  /** The editor the bar formats. */
  export let editor: RichEditor | null
  /** Bumped by the editor on every transaction, so the active states refresh. */
  export let revision = 0

  type Command = Parameters<RichEditor['run']>[0]

  interface Item {
    command: Command | 'link'
    mark: string
    icon: string
    label: string
    keys?: string
  }

  $: groups = [
    [
      { command: 'bold', mark: 'bold', icon: boldIcon, label: $t('compose.toolbar.bold'), keys: shortcutLabel('mod+b') },
      { command: 'italic', mark: 'italic', icon: italicIcon, label: $t('compose.toolbar.italic'), keys: shortcutLabel('mod+i') },
      { command: 'underline', mark: 'underline', icon: underlineIcon, label: $t('compose2.format.underline'), keys: shortcutLabel('mod+u') },
      { command: 'strike', mark: 'strike', icon: strikeIcon, label: $t('compose2.format.strike'), keys: shortcutLabel('mod+shift+s') },
    ],
    [
      { command: 'bulletList', mark: 'bulletList', icon: bulletIcon, label: $t('compose.toolbar.bulletList') },
      { command: 'orderedList', mark: 'orderedList', icon: numberIcon, label: $t('compose.toolbar.numberedList') },
      { command: 'blockquote', mark: 'blockquote', icon: quoteIcon, label: $t('compose.toolbar.quote') },
    ],
    [
      { command: 'link', mark: 'link', icon: linkIcon, label: $t('compose.toolbar.link') },
      { command: 'code', mark: 'code', icon: codeIcon, label: $t('compose.toolbar.inlineCode') },
      { command: 'clear', mark: '', icon: clearIcon, label: $t('compose2.format.clear') },
    ],
  ] as Item[][]

  function on(mark: string, _revision: number): boolean {
    return mark !== '' && editor !== null && editor.isActive(mark)
  }

  function apply(item: Item): void {
    if (!editor) {
      return
    }
    if (item.command === 'link') {
      startLink()
      return
    }
    editor.run(item.command)
  }

  // linking has three shapes. selected text that is an address already is
  // linked to itself at once. other selected text, or an existing link, asks
  // for the address in the bar itself. with nothing selected a small panel
  // takes both the text and the address.
  let mode: 'bar' | 'inline' | 'panel' = 'bar'
  let address = ''
  let label = ''
  let existing = false
  let panelLeft = 0
  let barEl: HTMLDivElement
  let addressEl: HTMLInputElement
  let labelEl: HTMLInputElement

  // hrefFor turns what was typed into an address a link can carry, or '' when
  // it does not look like one.
  function hrefFor(text: string): string {
    const value = text.trim()
    if (/^(https?:\/\/|mailto:)\S+$/i.test(value)) {
      return value
    }
    if (/^[^\s@]+@[^\s@]+\.[a-z]{2,}$/i.test(value)) {
      return `mailto:${value}`
    }
    if (/^[\w-]+(\.[\w-]+)*\.[a-z]{2,}([/?#]\S*)?$/i.test(value)) {
      return `https://${value}`
    }
    return ''
  }

  async function startLink(): Promise<void> {
    if (!editor) {
      return
    }
    const current = editor.linkHref()
    const selected = editor.selectedText()
    existing = current !== ''
    if (!existing && selected !== '') {
      const direct = hrefFor(selected)
      if (direct) {
        editor.setLinkHref(direct)
        return
      }
    }
    address = current
    if (existing || selected !== '') {
      mode = 'inline'
      await tick()
      addressEl?.focus()
      addressEl?.select()
      return
    }
    label = ''
    const button = barEl?.querySelector<HTMLElement>('[data-command="link"]')
    panelLeft = button ? Math.max(0, button.offsetLeft - barEl.scrollLeft) : 0
    mode = 'panel'
    await tick()
    labelEl?.focus()
  }

  function cancelLink(): void {
    mode = 'bar'
    editor?.focus()
  }

  function commitLink(): void {
    if (!editor) {
      return
    }
    // an address without a scheme gets one; anything else typed is kept as it
    // is, since the user asked for exactly that.
    const href = address.trim() === '' ? '' : hrefFor(address) || address.trim()
    if (mode === 'panel') {
      if (href !== '') {
        editor.insertLink(label.trim() || address.trim(), href)
      }
    } else {
      editor.setLinkHref(href)
    }
    mode = 'bar'
  }

  function removeLink(): void {
    editor?.setLinkHref('')
    mode = 'bar'
  }

  function onLinkKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      event.preventDefault()
      event.stopPropagation()
      commitLink()
    } else if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      cancelLink()
    }
  }
</script>

<div class="wrap">
{#if mode === 'inline'}
  <div class="formatbar linkline">
    <Ph svg={linkIcon} size={15} />
    <input
      type="text"
      class="address"
      bind:this={addressEl}
      bind:value={address}
      placeholder={$t('compose2.link.placeholder')}
      aria-label={$t('compose.toolbar.linkUrlPrompt')}
      spellcheck="false"
      autocapitalize="off"
      on:keydown={onLinkKeydown}
    />
    {#if existing}
      <button type="button" class="text" on:click={removeLink}>{$t('compose2.link.remove')}</button>
    {/if}
    <button type="button" class="text" on:click={cancelLink}>{$t('compose2.link.cancel')}</button>
    <button type="button" class="text primary" on:click={commitLink}>{$t('compose2.link.apply')}</button>
  </div>
{:else}
<div class="formatbar" role="toolbar" aria-label={$t('compose.toolbar.ariaLabel')} bind:this={barEl}>
  {#each groups as group, g (g)}
    {#if g > 0}<span class="sep" aria-hidden="true"></span>{/if}
    {#each group as item (item.command)}
      <button
        type="button"
        class="fmt"
        class:on={on(item.mark, revision)}
        aria-label={item.label}
        data-command={item.command}
        aria-pressed={item.mark ? on(item.mark, revision) : undefined}
        use:tip={{ text: item.label, shortcut: item.keys, side: 'bottom' }}
        on:mousedown|preventDefault
        on:click={() => apply(item)}
      >
        <Ph svg={item.icon} size={15} />
      </button>
    {/each}
  {/each}
</div>
{/if}
{#if mode === 'panel'}
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="linkpanel" style={`inset-inline-start: ${panelLeft}px`} on:keydown={onLinkKeydown}>
    <label>
      <span>{$t('compose2.link.text')}</span>
      <input type="text" bind:this={labelEl} bind:value={label} placeholder={$t('compose2.link.textPlaceholder')} />
    </label>
    <label>
      <span>{$t('compose2.link.address')}</span>
      <input
        type="text"
        bind:this={addressEl}
        bind:value={address}
        placeholder={$t('compose2.link.placeholder')}
        spellcheck="false"
        autocapitalize="off"
      />
    </label>
    <div class="row">
      <button type="button" class="text" on:click={cancelLink}>{$t('compose2.link.cancel')}</button>
      <button type="button" class="text primary" disabled={address.trim() === ''} on:click={commitLink}>
        {$t('compose2.link.insert')}
      </button>
    </div>
  </div>
{/if}
</div>

<style>
  .wrap {
    position: relative;
  }

  /* the bar itself, asking where the selected text should link to. */
  .linkline {
    gap: var(--space-3);
    min-height: calc(var(--control-height) + var(--space-1));
    color: var(--text-tertiary);
    animation: link-in var(--motion-fast) var(--ease-out);
  }

  .address {
    flex: 1;
    min-width: 0;
    height: calc(var(--control-height) - var(--space-2));
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
  }

  .text {
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-3);
    flex-shrink: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .text:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .text.primary {
    color: var(--accent);
    font-weight: var(--fw-medium);
  }

  .text:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* the small panel for a link made from nothing: its text and its address. */
  .linkpanel {
    position: absolute;
    top: 100%;
    z-index: 20;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: 280px;
    max-width: calc(100% - 2 * var(--space-3));
    margin: var(--space-1) var(--space-3) 0;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    animation: link-in var(--motion-fast) var(--ease-out);
  }

  .linkpanel label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .linkpanel input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font-size: var(--fz-label);
    outline: none;
  }

  .linkpanel input:focus {
    border-color: var(--accent);
  }

  .row {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-1);
  }

  @keyframes link-in {
    from {
      opacity: 0;
      transform: translateY(-2px);
    }
  }

  .formatbar {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .sep {
    width: var(--hairline);
    align-self: stretch;
    margin: var(--space-2) var(--space-2);
    background: var(--border-default);
  }

  .fmt {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) - var(--space-2));
    height: calc(var(--control-height) - var(--space-2));
    flex-shrink: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .fmt:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .fmt:active {
    transform: scale(var(--press-scale));
  }

  .fmt.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }
</style>
