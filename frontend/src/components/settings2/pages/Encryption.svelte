<script lang="ts">
  // the Encryption page: imported OpenPGP keys, which key each account signs
  // with, searching inside encrypted mail and S/MIME revocation checks.
  import Card from '../ui/Card.svelte'
  import EncryptionSection from '../../settings/EncryptionSection.svelte'
  import '../../../theme/settings2-sections-system.css'
</script>

<Card bare>
  <div class="s2-section" data-setting="keys">
    <EncryptionSection />
  </div>
</Card>
