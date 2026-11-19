<script lang="ts">
  // the picture of a contact: its initials on a flat disc. Contacts carry no
  // photo Pelton shows, and a lookup for one would send the address to a
  // third party, so this stays local.
  import { initials } from '../../lib/contactlist'
  import type { Contact } from '../../lib/types'

  /** The contact to draw. */
  export let contact: Contact
  /** Edge length in px. */
  export let size: number = 32

  $: letters = initials(contact)
</script>

<span class="avatar" style={`width:${size}px;height:${size}px;font-size:${Math.round(size * 0.38)}px`} aria-hidden="true">
  {letters}
</span>

<style>
  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: 50%;
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-weight: var(--fw-medium);
    line-height: 1;
    user-select: none;
  }
</style>
