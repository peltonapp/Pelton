<script lang="ts">
  // the error case of EmptyState with the generic title and an optional retry.
  // used wherever an async load can fail so the user sees the reason instead
  // of a blank column.
  import EmptyState from './EmptyState.svelte'
  import { t } from '../../lib/i18n'

  /** What failed, shown under the title. */
  export let message: string
  /** Reruns the failed load; no retry button without it. */
  export let onRetry: (() => void) | null = null
</script>

<EmptyState
  kind="error"
  title={$t('common.errorState.title')}
  detail={message}
  actionLabel={onRetry ? $t('common.errorState.retry') : ''}
  onAction={onRetry}
/>
