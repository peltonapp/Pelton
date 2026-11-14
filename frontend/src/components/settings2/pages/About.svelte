<script lang="ts">
  // the About page: version, links, licenses, diagnostics and update checks.
  import { createEventDispatcher } from 'svelte'
  import Card from '../ui/Card.svelte'
  import AboutSection from '../../settings/AboutSection.svelte'
  import '../../../theme/settings2-sections-system.css'

  const dispatch = createEventDispatcher<{ rerunOnboarding: void }>()
</script>

<Card bare>
  <div class="s2-section" data-setting="about">
    <AboutSection on:rerunOnboarding={() => dispatch('rerunOnboarding')} />
  </div>
</Card>
