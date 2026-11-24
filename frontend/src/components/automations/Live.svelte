<script lang="ts">
  // a live region that stays mounted and is filled when a message appears, so
  // assistive tech reads it out. a region created already holding its text is
  // not announced.
  import { tick } from 'svelte'

  /** The message to announce; empty stays silent. */
  export let text = ''
  /** Interrupts the reader, for errors. Polite otherwise. */
  export let assertive = false

  let said = ''

  $: void say(text)

  async function say(next: string): Promise<void> {
    said = ''
    if (!next) {
      return
    }
    await tick()
    said = next
  }
</script>

<div class="sr-only" role={assertive ? 'alert' : 'status'}>{said}</div>
