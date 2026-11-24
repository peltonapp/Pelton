<script lang="ts">
  // the certificate a mail server presented that did not verify, laid out for
  // the user to review before trusting it (#446). The wizard, the sync failure
  // dialog and the mailbox editor all show it the same way, so a Proton Mail
  // Bridge or self-hosted certificate reads identically wherever it turns up.
  // Trusting pins these exact certificates for this mailbox only; nothing here
  // turns verification off.
  import { createEventDispatcher } from 'svelte'
  import { IconAlertTriangle } from '@tabler/icons-svelte'
  import type { UntrustedCert } from '../../lib/types'
  import { formatRelative } from '../../lib/format'
  import { t } from '../../lib/i18n'

  export let certs: UntrustedCert[] = []
  export let busy = false

  const dispatch = createEventDispatcher<{ trust: UntrustedCert[] }>()

  // one server's certificate is often the other's too (Proton Bridge serves the
  // same one on both ports), so it is shown once with both servers named.
  $: unique = certs.reduce<{ cert: UntrustedCert; servers: string[] }[]>((acc, c) => {
    const server = `${c.server.toUpperCase()} ${c.host}:${c.port}`
    const seen = acc.find((x) => x.cert.fingerprint === c.fingerprint)
    if (seen) {
      seen.servers.push(server)
    } else {
      acc.push({ cert: c, servers: [server] })
    }
    return acc
  }, [])

  function validity(c: UntrustedCert): string {
    const from = c.notBefore ? new Date(c.notBefore).toLocaleDateString() : '?'
    const until = c.notAfter ? new Date(c.notAfter).toLocaleDateString() : '?'
    return `${from} - ${until}`
  }

  function expired(c: UntrustedCert): boolean {
    return c.notAfter !== '' && Date.parse(c.notAfter) < Date.now()
  }
</script>

<div class="cert-review">
  <p class="warning">
    <IconAlertTriangle size={15} stroke={1.8} />
    <span>{$t('certs.review.warning')}</span>
  </p>
  {#each unique as { cert, servers } (cert.fingerprint)}
    <dl class="cert">
      <dt>{$t('certs.review.servers')}</dt>
      <dd>{servers.join(', ')}</dd>
      <dt>{$t('certs.review.subject')}</dt>
      <dd>{cert.subject || '-'}</dd>
      <dt>{$t('certs.review.issuer')}</dt>
      <dd>{cert.selfSigned ? $t('certs.review.selfSigned') : cert.issuer || '-'}</dd>
      {#if cert.names.length > 0}
        <dt>{$t('certs.review.names')}</dt>
        <dd>{cert.names.join(', ')}</dd>
      {/if}
      <dt>{$t('certs.review.valid')}</dt>
      <dd>
        {validity(cert)}
        {#if expired(cert)}
          <span class="expired">{$t('certs.review.expired').replace('{when}', formatRelative(Date.parse(cert.notAfter), $t))}</span>
        {/if}
      </dd>
      <dt>{$t('certs.review.fingerprint')}</dt>
      <dd class="fingerprint">{cert.display}</dd>
      {#if cert.reason}
        <dt>{$t('certs.review.reason')}</dt>
        <dd class="reason">{cert.reason}</dd>
      {/if}
    </dl>
  {/each}
  <p class="hint">{$t('certs.review.hint')}</p>
  <button type="button" class="trust" disabled={busy} on:click={() => dispatch('trust', certs)}>
    {busy ? $t('certs.review.trusting') : $t('certs.review.trust')}
  </button>
</div>

<style>
  .cert-review {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin: 0 0 var(--space-4);
    padding: var(--space-3);
    border-radius: var(--radius-control);
    background: var(--warning-bg);
    color: var(--text-primary);
    font-size: var(--fz-label);
    line-height: 1.5;
  }

  .warning {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0;
    font-weight: var(--fw-medium);
  }

  .warning :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
    color: var(--warning);
  }

  .cert {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-1) var(--space-3);
    margin: 0;
  }

  .cert dt {
    color: var(--text-secondary);
  }

  .cert dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .fingerprint,
  .reason {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }

  .expired {
    margin-left: var(--space-2);
    color: var(--warning);
  }

  .hint {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }

  .trust {
    align-self: flex-start;
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }

  .trust:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .trust:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
