<script lang="ts">
  // the places the monospaced font shows up: an address in a message header,
  // the key of a shortcut and a line of code.
  import Frame from './Frame.svelte'
  import Keycap from './Keycap.svelte'
  import { prefs } from '../../../stores/prefs'
  import { monoFontStack } from '../../../lib/fonts'
  import { sampleMessages } from './samples'

  const person = sampleMessages[0]

  $: stack = monoFontStack($prefs.monoFont) ?? 'var(--font-mono)'
</script>

<Frame>
  <div class="mono" style={`font-family:${stack}`}>
    <span class="addr">{person.name} &lt;{person.email}&gt;</span>
    <span class="keys"><Keycap>Ctrl</Keycap><Keycap>K</Keycap></span>
    <code class="code">const unread = inbox.filter(isUnread)</code>
  </div>
</Frame>

<style>
  .mono {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .addr {
    overflow-wrap: anywhere;
  }

  .keys {
    display: inline-flex;
    gap: var(--space-2);
  }

  .code {
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font: inherit;
    overflow-wrap: anywhere;
  }
</style>
