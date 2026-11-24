<script lang="ts">
  // the head of a message with the technical badges under the subject. the
  // badges are the real ones, so each shows or hides with its own setting.
  import Frame from './Frame.svelte'
  import TechBadges from '../../common/TechBadges.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  const person = sampleMessages[1]
</script>

<Frame>
  <div class="head">
    <span class="from">{person.name}</span>
    <span class="subject">{person.subject}</span>
    <TechBadges accountEmail={person.email} folderName={$t('folders.role.inbox')} pgp="signed" auth="pass" />
  </div>
  <div class="lines">
    <span class="bar w90"></span>
    <span class="bar w80"></span>
    <span class="bar w50"></span>
  </div>
</Frame>

<style>
  .head {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    padding-bottom: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-label);
  }

  .from {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .subject {
    color: var(--text-secondary);
  }

  .lines {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding-top: var(--space-3);
  }

  .bar {
    display: block;
    height: 4px;
    border-radius: 2px;
    background: var(--border-strong);
  }

  .w50 { width: 50%; }
  .w80 { width: 80%; }
  .w90 { width: 90%; }
</style>
