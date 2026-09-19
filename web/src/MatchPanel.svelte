<script lang="ts">
  import {
    errorMessage,
    type ApiError,
    type Candidate,
    type Download,
    type MatchInput,
    type MediaType,
  } from './types'

  let {
    download,
    metadataEnabled = false,
  }: {
    download: Download
    metadataEnabled?: boolean
  } = $props()

  let type = $state<MediaType>('movie')
  let season = $state(0)
  let episode = $state(0)
  let query = $state('')
  let results = $state<Candidate[] | null>(null)
  let searching = $state(false)
  let applying = $state(false)
  let error = $state('')

  let manualTitle = $state('')
  let manualYear = $state(0)

  // syncKey is deliberately not reactive: it lets the effect below re-seed the
  // form only when the match identity changes, not on every progress tick.
  let syncKey = ''

  const candidates = $derived(results ?? download.match?.candidates ?? [])

  $effect(() => {
    const m = download.match
    const key = `${m.tmdbId ?? 0}:${m.status ?? ''}:${m.mediaType ?? ''}:${m.season ?? 0}:${m.episode ?? 0}`
    if (key === syncKey) return
    syncKey = key
    type = m.mediaType ?? 'movie'
    season = m.season ?? 0
    episode = m.episode ?? 0
    manualTitle = m.title ?? ''
    manualYear = m.year ?? 0
    results = null
  })

  async function search(event: SubmitEvent) {
    event.preventDefault()
    const value = query.trim()
    if (!value || searching) return
    searching = true
    error = ''
    try {
      const res = await fetch(`/api/search?type=${type}&query=${encodeURIComponent(value)}`)
      const body = await res.json().catch(() => [])
      if (!res.ok) throw new Error((body as ApiError).message || `Search failed (${res.status})`)
      results = Array.isArray(body) ? (body as Candidate[]) : []
    } catch (err) {
      error = errorMessage(err)
    } finally {
      searching = false
    }
  }

  async function patch(body: MatchInput) {
    if (applying) return
    applying = true
    error = ''
    try {
      const res = await fetch(`/api/downloads/${download.hash}/match`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = (await res.json().catch(() => ({}))) as ApiError
      if (!res.ok) throw new Error(data.message || `Update failed (${res.status})`)
      results = null
    } catch (err) {
      error = errorMessage(err)
    } finally {
      applying = false
    }
  }

  function choose(candidate: Candidate) {
    const tv = candidate.mediaType === 'tv'
    patch({
      mediaType: candidate.mediaType,
      tmdbId: candidate.tmdbId,
      season: tv ? season : 0,
      episode: tv ? episode : 0,
    } as MatchInput)
  }

  function applyManual() {
    const tv = type === 'tv'
    patch({
      mediaType: type,
      title: manualTitle.trim(),
      year: manualYear,
      season: tv ? season : 0,
      episode: tv ? episode : 0,
    } as MatchInput)
  }
</script>

<div class="panel">
  <div class="panel-controls">
    <label class="field">
      <span>Type</span>
      <select bind:value={type}>
        <option value="movie">Movie</option>
        <option value="tv">TV</option>
      </select>
    </label>
    {#if type === 'tv'}
      <label class="field">
        <span>Season</span>
        <input type="number" min="0" bind:value={season} />
      </label>
      <label class="field">
        <span>Episode</span>
        <input type="number" min="0" bind:value={episode} />
      </label>
    {/if}
  </div>

  {#if metadataEnabled}
    <form class="search-row" onsubmit={search}>
      <input
        type="text"
        bind:value={query}
        placeholder="Search TMDB for the correct {type === 'tv' ? 'series' : 'movie'}…"
        aria-label="Search TMDB"
      />
      <button type="submit" disabled={searching || !query.trim()}>
        {searching ? 'Searching…' : 'Search'}
      </button>
    </form>
  {:else}
    <p class="muted small">TMDB is not configured; the release name is used as-is.</p>
  {/if}

  {#if error}
    <p class="error small">{error}</p>
  {/if}

  {#if candidates.length > 0}
    <ul class="candidates">
      {#each candidates as c (c.mediaType + '-' + c.tmdbId)}
        <li>
          <button type="button" class="candidate" onclick={() => choose(c)} disabled={applying}>
            {#if c.posterUrl}
              <img src={c.posterUrl} alt="" loading="lazy" />
            {:else}
              <span class="candidate-placeholder"></span>
            {/if}
            <span class="candidate-info">
              <span class="candidate-title">
                {c.title}{#if c.year}<span class="candidate-year">({c.year})</span>{/if}
              </span>
              <span class="candidate-type">{c.mediaType === 'tv' ? 'TV' : 'Movie'}</span>
              {#if c.overview}
                <span class="candidate-overview">{c.overview}</span>
              {/if}
            </span>
          </button>
        </li>
      {/each}
    </ul>
  {:else if metadataEnabled && !searching}
    <p class="muted small">
      {download.match?.status === 'pending'
        ? 'Looking up metadata…'
        : 'No candidates. Search above, or set the title manually.'}
    </p>
  {/if}

  <details class="manual">
    <summary>Set the title manually</summary>
    <div class="manual-row">
      <input type="text" bind:value={manualTitle} placeholder="Title" aria-label="Title" />
      <input class="year" type="number" bind:value={manualYear} placeholder="Year" aria-label="Year" />
      <button type="button" onclick={applyManual} disabled={applying || !manualTitle.trim()}>Apply</button>
    </div>
  </details>
</div>
