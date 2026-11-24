<script lang="ts">
  // the change to a library since a script that loads it was approved, as a
  // line diff.
  import type { ScriptLibraryNote } from '../../lib/scripting'

  /** The note holding the change. */
  export let note: ScriptLibraryNote
</script>

<pre class="diff">{#each note.diff as line, i (i)}<span class="line" class:add={line.op === '+'} class:del={line.op === '-'}>{line.op} {line.text}
</span>{/each}</pre>

<style>
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
</style>
