<script lang="ts">
  // a circular sender avatar. it tries the configured remote photo candidates in
  // order (BIMI logo, Gravatar - per the user's fallback chain) and, when they
  // all fail or none exist, draws a generated placeholder ("pfp") in the chosen
  // style. the placeholder is a deterministic inline SVG, so it needs no network
  // and is stable per sender. accent stays reserved for selection and links.
  import { photosFor } from '../../lib/avatar'
  import { pfpForSender, type PfpStyle } from '../../lib/pfp'
  import { prefs } from '../../stores/prefs'

  export let name: string = ''
  export let email: string = ''
  export let size: number = 32
  // colored=false renders a neutral disc (used by skeletons), bypassing photos.
  export let colored: boolean = true

  // the generated placeholder for the current style; always available as the
  // final fallback and shown immediately while remote candidates resolve.
  $: placeholder = pfpForSender(($prefs.avatarStyle as PfpStyle) ?? 'initials', name, email)

  // remote candidates for this sender under the current source preference. we
  // try them in order via the <img> error handler; index advances on each fail.
  let candidates: string[] = []
  let attempt = 0
  $: void resolveCandidates($prefs.avatarSource, email, colored)
  async function resolveCandidates(source: string, em: string, isColored: boolean): Promise<void> {
    candidates = []
    attempt = 0
    if (!isColored || !em) {
      return
    }
    const found = await photosFor(source, em)
    if (source === $prefs.avatarSource && em === email) {
      candidates = found ?? []
    }
  }

  // the url currently being shown: the active remote candidate, else the pfp.
  $: current = attempt < candidates.length ? candidates[attempt] : placeholder

  function onError(): void {
    // advance to the next remote candidate, or fall through to the placeholder.
    if (attempt < candidates.length) {
      attempt += 1
    }
  }
</script>

{#if colored}
  <img
    class="avatar"
    style={`width:${size}px;height:${size}px`}
    src={current}
    alt=""
    aria-hidden="true"
    draggable="false"
    on:error={onError}
  />
{:else}
  <span
    class="avatar neutral"
    style={`width:${size}px;height:${size}px`}
    aria-hidden="true"
  ></span>
{/if}

<style>
  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 999px;
    flex-shrink: 0;
    object-fit: cover;
    background: var(--surface-sunken);
  }

  .avatar.neutral {
    border: var(--hairline) solid var(--border-subtle);
  }
</style>
