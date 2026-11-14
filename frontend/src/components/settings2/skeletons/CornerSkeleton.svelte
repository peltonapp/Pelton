<script lang="ts">
  // a picture of how rounded controls and cards are: a card holding a field and
  // a button, drawn with the radii of one corner style instead of the live ones.

  /** Which corner style to draw. */
  export let corners: 'square' | 'default' | 'round' = 'default'
  /** Height of the picture in px. */
  export let height: number = 84

  const radii = { square: [0, 0], default: [5, 5], round: [10, 12] }
  $: control = radii[corners][0]
  $: card = radii[corners][1]
</script>

<div class="frame" style={`height:${height}px`} aria-hidden="true">
  <div class="card" style={`border-radius:${card}px`}>
    <span class="field" style={`border-radius:${control}px`}></span>
    <span class="button" style={`border-radius:${control}px`}></span>
  </div>
</div>

<style>
  .frame {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0 12px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-sunken);
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    padding: 8px;
    border: var(--hairline) solid var(--border-strong);
    background: var(--surface-raised);
  }

  .field {
    display: block;
    height: 14px;
    border: var(--hairline) solid var(--border-strong);
    background: var(--surface-base);
  }

  .button {
    display: block;
    width: 40%;
    height: 14px;
    background: var(--accent);
  }
</style>
