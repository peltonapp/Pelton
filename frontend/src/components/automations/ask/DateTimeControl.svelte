<script lang="ts">
  // a date and a time of day, with the app's date picker; the answer is unix
  // seconds.
  import DateTimePicker from '../../common/DateTimePicker.svelte'
  import type { ScriptAsk } from '../../../lib/scripting'

  /** The question. */
  export let ask: ScriptAsk
  /** The picked time in unix seconds. */
  export let value: unknown = undefined

  const pad = (n: number) => String(n).padStart(2, '0')

  function local(secs: number): string {
    const d = new Date(secs * 1000)
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
  }

  let text = local(typeof value === 'number' ? value : Math.floor(Date.now() / 1000))
  $: ms = new Date(text).getTime()
  $: if (!Number.isNaN(ms)) {
    value = Math.floor(ms / 1000)
  }
</script>

<div class="when" aria-label={ask.prompt}>
  <DateTimePicker mode="datetime" bind:value={text} />
</div>

<style>
  .when {
    display: flex;
  }
</style>
