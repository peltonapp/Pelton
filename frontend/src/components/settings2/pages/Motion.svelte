<script lang="ts">
  // the Motion page: whether the interface animates and which cursor it shows
  // over clickable things.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import MotionSkeleton from '../skeletons/MotionSkeleton.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import { prefs, setReduceMotion, setHandCursor } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
</script>

<Card title={$t('settings2.page.motion')}>
  <Row id="reduce-motion" label={$t('settingsPanel.toggle.reduceMotion')} hint={$t('settingsPanel.hint.reduceMotion')}>
    <svelte:fragment slot="preview">
      <MotionSkeleton still={$prefs.reduceMotion} />
    </svelte:fragment>
    <ToggleSwitch
      checked={$prefs.reduceMotion}
      label={$t('settingsPanel.toggle.reduceMotion')}
      on:change={(e) => setReduceMotion(e.detail)}
    />
  </Row>
  <Row id="hand-cursor" label={$t('settingsPanel.toggle.handCursor')} hint={$t('settingsPanel.hint.handCursor')}>
    <ToggleSwitch
      checked={$prefs.handCursor}
      label={$t('settingsPanel.toggle.handCursor')}
      on:change={(e) => setHandCursor(e.detail)}
    />
  </Row>
</Card>
