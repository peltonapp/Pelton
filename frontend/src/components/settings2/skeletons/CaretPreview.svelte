<script lang="ts">
  // a line being typed, one character at a time. the caret either glides to
  // each new position or jumps there, which is the whole difference between
  // smooth typing on and off. with reduced motion the line is drawn finished.
  import { onDestroy, onMount } from 'svelte'
  import Frame from './Frame.svelte'
  import { dur } from '../../../lib/motion'
  import { t } from '../../../lib/i18n'

  /** Whether the caret glides between characters. */
  export let smooth: boolean = true

  const stepMs = 130
  const holdSteps = 12

  let shown = 0
  let width = 0
  let timer: ReturnType<typeof setInterval> | undefined

  $: chars = Array.from($t('preview.caret.sample'))
  $: typed = chars.slice(0, shown).join('')

  onMount(() => {
    if (dur(1) === 0) {
      shown = chars.length
      return
    }
    timer = setInterval(() => {
      shown = shown >= chars.length + holdSteps ? 0 : shown + 1
    }, stepMs)
  })

  onDestroy(() => clearInterval(timer))
</script>

<Frame>
  <div class="field">
    <span class="text" bind:clientWidth={width}>{typed}</span>
    <span class="caret" class:glide={smooth} style={`transform:translateX(${width}px)`}></span>
  </div>
  <div class="rule"></div>
</Frame>

<style>
  .field {
    position: relative;
    height: calc(var(--fz-body) * 1.6);
    font-size: var(--fz-body);
    line-height: 1.6;
    color: var(--text-primary);
    white-space: pre;
  }

  .text {
    display: inline-block;
    height: 100%;
  }

  .caret {
    position: absolute;
    inset-block: 15%;
    inset-inline-start: 0;
    width: 2px;
    border-radius: 1px;
    background: var(--accent);
  }

  .caret.glide {
    transition: transform var(--motion-base) var(--ease-out);
  }

  .rule {
    height: var(--hairline);
    margin-top: var(--space-2);
    background: var(--border-default);
  }
</style>
