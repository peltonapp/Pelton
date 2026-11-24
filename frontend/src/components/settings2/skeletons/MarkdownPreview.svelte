<script lang="ts">
  // a line typed with Markdown marks around two phrases. with Markdown on the
  // marks turn into bold and italic text; off, they stay as typed.
  import Frame from './Frame.svelte'
  import { t } from '../../../lib/i18n'

  /** Formats the marks as the text is typed. */
  export let on: boolean = true
</script>

<Frame>
  <p class="line">
    {#if on}
      <strong>{$t('preview.markdown.bold')}</strong> <em>{$t('preview.markdown.italic')}</em>
    {:else}
      <span class="raw">**{$t('preview.markdown.bold')}** _{$t('preview.markdown.italic')}_</span>
    {/if}
    <span class="caret"></span>
  </p>
</Frame>

<style>
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-body);
    color: var(--text-primary);
  }

  strong {
    font-weight: var(--fw-bold);
  }

  .raw {
    font-family: var(--font-mono);
    font-size: var(--fz-label);
  }

  .caret {
    width: 2px;
    height: calc(var(--fz-body) + var(--space-1));
    border-radius: 1px;
    background: var(--accent);
  }
</style>
