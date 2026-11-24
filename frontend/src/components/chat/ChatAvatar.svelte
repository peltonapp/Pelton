<script context="module" lang="ts">
  // avatars load once per mxc address for the whole app; a failed one is
  // remembered too, so a broken picture is not asked for on every redraw.
  const cache = new Map<string, Promise<string>>()

  function load(mxc: string): Promise<string> {
    let hit = cache.get(mxc)
    if (!hit) {
      hit = chatAvatar(mxc).catch(() => '')
      cache.set(mxc, hit)
    }
    return hit
  }
</script>

<script lang="ts">
  // a round picture for a person or room in chat: the mxc avatar from the
  // homeserver when there is one, otherwise initials on a colour derived from
  // the id.
  import { chatAvatar } from '../../lib/api'
  import { avatarColor, initials } from '../../lib/format'

  /** The mxc address of the picture, or empty. */
  export let mxc: string = ''
  /** The name the initials are taken from. */
  export let name: string = ''
  /** A stable id (user or room) the fallback colour is derived from. */
  export let seed: string = ''
  /** Edge length; a css length, a token by default. */
  export let size: string = 'var(--chat-avatar)'

  let src = ''
  $: void resolve(mxc)

  async function resolve(addr: string): Promise<void> {
    src = ''
    if (!addr.startsWith('mxc://')) {
      return
    }
    const url = await load(addr)
    if (addr === mxc) {
      src = url
    }
  }
</script>

<span class="avatar" style={`--size:${size};--bg:${avatarColor(seed || name)}`} aria-hidden="true">
  {#if src}
    <img {src} alt="" />
  {:else}
    <span class="initials">{initials(name, seed.replace(/^[@!#]/, ''))}</span>
  {/if}
</span>

<style>
  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--size);
    height: var(--size);
    border-radius: 50%;
    overflow: hidden;
    background: var(--bg);
    color: var(--text-inverse);
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .initials {
    font-size: calc(var(--size) * 0.4);
    font-weight: var(--fw-semibold);
    line-height: 1;
    letter-spacing: 0.02em;
  }
</style>
