<script lang="ts">
  // the test run choice for a script with AI steps: ask the model, so a
  // branch on its answer can be tried, or leave it out. off by default; the
  // caller runs testScriptAskingAI instead of testScript while it is on.
  import { t } from '../../lib/i18n'

  /** Whether the next test run asks the model. Two-way bindable. */
  export let checked = false
</script>

<label class="toggle" title={$t('ai.test.askModel.hint')}>
  <input type="checkbox" bind:checked />
  {$t('ai.test.askModel')}
</label>

<style>
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }
</style>
