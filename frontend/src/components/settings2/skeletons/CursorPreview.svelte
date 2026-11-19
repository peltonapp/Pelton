<script lang="ts">
  // a button with the pointer resting on it, drawn as the arrow or as the hand.
  import arrowIcon from '@phosphor-icons/core/assets/duotone/cursor-duotone.svg?raw'
  import handIcon from '@phosphor-icons/core/assets/duotone/hand-pointing-duotone.svg?raw'
  import Frame from './Frame.svelte'
  import Ph from '../../common/Ph.svelte'
  import { t } from '../../../lib/i18n'

  /** Shows the hand instead of the arrow. */
  export let hand: boolean = false
</script>

<Frame>
  <div class="stage">
    <span class="button">{$t('action.reply')}</span>
    <span class="pointer"><Ph svg={hand ? handIcon : arrowIcon} size={22} /></span>
  </div>
</Frame>

<style>
  .stage {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    height: 56px;
  }

  .button {
    padding: var(--space-3) var(--space-5);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-hover);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .pointer {
    position: absolute;
    top: 50%;
    inset-inline-start: 56%;
    display: inline-flex;
    color: var(--text-primary);
  }
</style>
