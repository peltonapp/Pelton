<script lang="ts">
  // a reply being written: the new text, the quoted original and the signature.
  // the signature sits above the quoted block or below it.
  import Frame from './Frame.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Puts the signature above the quoted text. */
  export let above: boolean = false

  const sender = sampleMessages[0].name
</script>

<Frame>
  <div class="reply" class:above>
    <p class="new">{$t('preview.reply.text')}</p>
    <div class="sig">
      <span class="bar w40 strong"></span>
      <span class="bar w30"></span>
    </div>
    <div class="quote">
      <span class="wrote">{$t('preview.reply.wrote').replace('{name}', sender)}</span>
      <span class="bar w90"></span>
      <span class="bar w70"></span>
    </div>
  </div>
</Frame>

<style>
  .reply {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .new {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .sig {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    order: 2;
  }

  .quote {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-inline-start: var(--space-3);
    border-inline-start: 2px solid var(--border-strong);
    order: 1;
  }

  .above .sig {
    order: 1;
  }

  .above .quote {
    order: 2;
  }

  .wrote {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .bar {
    display: block;
    height: 4px;
    border-radius: 2px;
    background: var(--border-strong);
  }

  .bar.strong {
    background: var(--text-tertiary);
  }

  .w30 { width: 30%; }
  .w40 { width: 40%; }
  .w70 { width: 70%; }
  .w90 { width: 90%; }
</style>
