<script lang="ts">
  // what the list and the grid show without a contact to show: progress on the
  // first load, a way back out of a search or filter that matched nothing, or
  // the first contact to add.
  import { createEventDispatcher } from 'svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import peopleIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
  import EmptyState from '../common/EmptyState.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { contacts, contactsLoading } from '../../stores/contacts'
  import { contactOrg, contactQuery } from '../../stores/contactsview'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ create: void }>()

  function clearFilter(): void {
    contactQuery.set('')
    contactOrg.set(null)
  }
</script>

{#if $contactsLoading && $contacts.length === 0}
  <div class="loading"><PieProgress size={20} label={$t('contacts.loading')} /></div>
{:else if $contacts.length > 0}
  <EmptyState
    kind="filtered"
    icon={searchIcon}
    title={$t('contacts.noMatchesTitle')}
    detail={$t('contacts.noMatchesDetail')}
    actionLabel={$t('common.clearFilter')}
    onAction={clearFilter}
  />
{:else}
  <EmptyState
    icon={peopleIcon}
    title={$t('contacts.emptyBookTitle')}
    detail={$t('contacts.emptyBookDetail')}
    actionLabel={$t('contacts.new')}
    onAction={() => dispatch('create')}
  />
{/if}

<style>
  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-5);
  }
</style>
