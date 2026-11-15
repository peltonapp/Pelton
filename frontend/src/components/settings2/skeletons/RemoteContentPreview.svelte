<script lang="ts">
  // a newsletter with one picture and a tracking pixel. images blocked, the
  // message says so and the picture is a placeholder. images loaded, the picture
  // shows; the tracking pixel stays blocked only when that setting is on.
  import imageIcon from '@phosphor-icons/core/assets/duotone/image-duotone.svg?raw'
  import brokenIcon from '@phosphor-icons/core/assets/duotone/image-broken-duotone.svg?raw'
  import Frame from './Frame.svelte'
  import Ph from '../../common/Ph.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Remote images are loaded. */
  export let images: boolean = false
  /** Tracking pixels stay blocked even when images load. */
  export let blockPixels: boolean = true

  const person = sampleMessages[1]
</script>

<Frame>
  <div class="head">
    <span class="from">{person.name}</span>
    <span class="subject">{person.subject}</span>
  </div>
  {#if !images}
    <div class="note">{$t('detail.mailBody.remoteBlocked')}</div>
  {:else if blockPixels}
    <div class="note">{$t('detail.mailBody.pixelsKeptBlocked').replace('{count}', '2')}</div>
  {/if}
  <div class="picture" class:blocked={!images}>
    <Ph svg={images ? imageIcon : brokenIcon} size={22} />
  </div>
  <div class="foot">
    <span class="bar"></span>
    <span class="pixel" class:loaded={images && !blockPixels}></span>
  </div>
</Frame>

<style>
  .head {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding-bottom: var(--space-3);
    font-size: var(--fz-label);
  }

  .from {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .subject {
    color: var(--text-tertiary);
  }

  .note {
    margin-bottom: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .picture {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 52px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
  }

  .picture.blocked {
    border-style: dashed;
  }

  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: var(--space-3);
  }

  .bar {
    width: 40%;
    height: 4px;
    border-radius: 2px;
    background: var(--border-strong);
  }

  /* the pixel is one dot at the foot of the message: a dashed ring while it is
     blocked, filled once it loads and tells the sender the message was opened. */
  .pixel {
    width: 6px;
    height: 6px;
    border: 1px dashed var(--border-strong);
    border-radius: 50%;
  }

  .pixel.loaded {
    border: none;
    background: var(--danger);
  }
</style>
