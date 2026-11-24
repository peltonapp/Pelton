<script lang="ts">
  // a tiny app window: a folder column, three list rows and a reading area,
  // drawn from surface, text and border tokens. put it under a .theme-light or
  // .theme-dark ancestor to pin which theme it shows.
</script>

<div class="window" aria-hidden="true">
  <div class="side">
    <span class="bar w70"></span>
    <span class="bar w50"></span>
    <span class="bar w60"></span>
  </div>
  <div class="list">
    {#each [0, 1, 2] as i (i)}
      <span class="item" class:on={i === 1}><span class="bar w80"></span><span class="bar w50"></span></span>
    {/each}
  </div>
  <div class="read">
    <span class="bar w60 strong"></span>
    <span class="bar w90"></span>
    <span class="bar w80"></span>
    <span class="bar w40"></span>
  </div>
</div>

<style>
  .window {
    display: grid;
    grid-template-columns: 22% 34% 1fr;
    width: 100%;
    height: 100%;
    background: var(--surface-base);
    color: var(--text-primary);
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 8px 6px;
    background: var(--surface-sunken);
  }

  .list {
    display: flex;
    flex-direction: column;
    background: var(--surface-raised);
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .item {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 6px;
  }

  .item + .item {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .item.on {
    background: var(--selection-bg);
  }

  .read {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px;
    background: var(--surface-raised);
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .bar.strong {
    height: 4px;
    background: var(--text-tertiary);
  }

  .w40 { width: 40%; }
  .w50 { width: 50%; }
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w80 { width: 80%; }
  .w90 { width: 90%; }
</style>
