<script lang="ts">
  import { buildTrackerUrl } from './tracker'
  import type { Tracker } from './types'

  let { query, trackers }: { query: string; trackers: Tracker[] } = $props()

  const ready = $derived(query.trim().length > 0)
  const label = $derived(query.trim())
</script>

{#if ready}
  <section class="trackers" aria-label="Tracker search links">
    <p class="tracker-heading">
      Search trackers for <strong>{label}</strong>
    </p>
    {#if trackers.length === 0}
      <p class="muted small">
        No trackers configured. Add a top-level <code>trackers</code> list to
        <code>config.yaml</code> to search from here.
      </p>
    {:else}
      <ul class="tracker-list">
        {#each trackers as t (t.name + t.url)}
          <li>
            <a
              class="tracker"
              href={buildTrackerUrl(t.url, query)}
              target="_blank"
              rel="noopener noreferrer"
            >
              <span class="tracker-name">{t.name}</span>
              <span class="tracker-url">{t.url}</span>
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}
