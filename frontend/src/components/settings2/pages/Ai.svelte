<script lang="ts">
  // the AI page: the language model providers scripts may use. it is the only
  // place AI is set up, and nothing is sent anywhere until a provider is added
  // and a script is allowed to use it.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import AiProviders from '../../ai/AiProviders.svelte'
  import { t } from '../../../lib/i18n'
</script>

<Card title={$t('ai.title')} description={$t('ai.description')}>
  <Row id="ai-providers" label={$t('ai.providers')} hint={$t('ai.providers.hint')} stacked>
    <AiProviders />
  </Row>
</Card>
