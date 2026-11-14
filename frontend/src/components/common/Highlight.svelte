<script lang="ts">
  // text with the words of the running search marked.
  import { highlightParts } from '../../lib/highlight'

  /** The text to show. */
  export let text: string
  /** The words to mark; empty shows the text as it is. */
  export let terms: readonly string[] = []

  $: parts = terms.length > 0 ? highlightParts(text, terms) : null
</script>

{#if parts}{#each parts as part}{#if part.hit}<mark class="hl">{part.text}</mark>{:else}{part.text}{/if}{/each}{:else}{text}{/if}

<style>
  .hl {
    padding: 0;
    border-radius: var(--radius-control);
    background: var(--highlight-bg);
    color: var(--highlight-fg);
  }
</style>
