<script>
  import MatchPanel from './MatchPanel.svelte'

  let { download, metadataEnabled = false, onremove } = $props()

  let editing = $state(false)

  const percent = $derived(Math.round((download.progress ?? 0) * 100))
  const removable = $derived(download.status === 'completed' || download.status === 'failed')
  const match = $derived(download.match ?? {})
  const heading = $derived(match.title || download.name || download.hash)

  const matchLabels = {
    pending: 'looking up…',
    matched: 'auto-matched',
    manual: 'corrected',
    fallback: 'release name',
  }
  const matchLabel = $derived(matchLabels[match.status] ?? match.status ?? '')
</script>

<article class="download">
  <div class="row">
    <div class="heading">
      {#if match.posterUrl}
        <img class="poster" src={match.posterUrl} alt="" loading="lazy" />
      {/if}
      <div class="titles">
        <span class="name" title={heading}>{heading}</span>
        {#if download.name && match.title && download.name !== match.title}
          <span class="subtitle" title={download.name}>{download.name}</span>
        {/if}
      </div>
    </div>
    <span class="status {download.status}">{download.status}</span>
  </div>

  <div class="row badges">
    <span class="badge">{match.mediaType === 'tv' ? 'TV' : 'Movie'}</span>
    {#if match.year}
      <span class="badge">{match.year}</span>
    {/if}
    {#if match.mediaType === 'tv' && match.season > 0}
      <span class="badge">
        S{String(match.season).padStart(2, '0')}{#if match.episode > 0}E{String(match.episode).padStart(2, '0')}{/if}
      </span>
    {/if}
    <span class="badge match-{match.status}">{matchLabel}</span>
  </div>

  <div class="bar" role="progressbar" aria-valuenow={percent} aria-valuemin="0" aria-valuemax="100">
    <div class="fill {download.status}" style="width: {percent}%"></div>
  </div>

  <div class="row meta">
    <span class="pct">{percent}%</span>
    {#if download.destination}
      <span class="dest" title={download.destination}>{download.destination}</span>
    {/if}
    <button class="link" type="button" onclick={() => (editing = !editing)}>
      {editing ? 'Close' : 'Fix match'}
    </button>
    {#if removable}
      <button class="remove" type="button" onclick={() => onremove(download.hash)}>Remove</button>
    {/if}
  </div>

  {#if download.error}
    <p class="error small">{download.error}</p>
  {/if}

  {#if editing}
    <MatchPanel {download} {metadataEnabled} />
  {/if}
</article>
