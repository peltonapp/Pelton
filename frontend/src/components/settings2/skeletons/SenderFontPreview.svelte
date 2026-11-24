<script lang="ts">
  // a short message body. with sender fonts on it keeps the typeface its
  // designer chose; off, it is set in the reader font like every other message.
  import Frame from './Frame.svelte'
  import { prefs } from '../../../stores/prefs'
  import { bodyFontStack } from '../../../lib/fonts'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Keeps the typeface the sender chose. */
  export let senderFonts: boolean = true

  const person = sampleMessages[0]

  $: reader = bodyFontStack($prefs.bodyFont) ?? 'var(--font-ui)'
</script>

<Frame>
  <div class="head">
    <span class="from">{person.name}</span>
    <span class="subject">{person.subject}</span>
  </div>
  <p class="body" class:designed={senderFonts} style={senderFonts ? undefined : `font-family:${reader}`}>
    {$t('preview.sender.sample')}
  </p>
</Frame>

<style>
  .head {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding-bottom: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-label);
  }

  .from {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .subject {
    color: var(--text-tertiary);
  }

  .body {
    margin: var(--space-3) 0 0;
    font-size: var(--fz-heading);
    line-height: 1.45;
    color: var(--text-primary);
  }

  .body.designed {
    font-family: serif;
  }
</style>
