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
      const url = window.prompt($t('compose.toolbar.linkUrlPrompt'), editor.linkHref())
      if (url !== null) {
        editor.setLinkHref(url.trim())
      }
      return
    }
    editor.run(item.command)
  }
</script>

<div class="formatbar" role="toolbar" aria-label={$t('compose.toolbar.ariaLabel')}>
  {#each groups as group, g (g)}
    {#if g > 0}<span class="sep" aria-hidden="true"></span>{/if}
    {#each group as item (item.command)}
      <button
        type="button"
        class="fmt"
        class:on={on(item.mark, revision)}
        aria-label={item.label}
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

<style>
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
