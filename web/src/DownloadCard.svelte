<script lang="ts">
  import MatchPanel from './MatchPanel.svelte'
  import type { Download } from './types'

  let {
    download,
    metadataEnabled = false,
    onclear,
    ondelete,
  }: {
    download: Download
    metadataEnabled?: boolean
    onclear: (hash: string) => void
    ondelete: (hash: string, deleteFiles: boolean) => void
  } = $props()

  let editing = $state(false)
  // confirmingDelete opens the inline keep-or-delete-files prompt; it avoids a
  // blocking browser dialog.
  let confirmingDelete = $state(false)

  function confirmDelete(deleteFiles: boolean) {
    confirmingDelete = false
    ondelete(download.hash, deleteFiles)
  }

  const percent = $derived(Math.round((download.progress ?? 0) * 100))
  const controllable = $derived(download.status !== 'moving')
  const match = $derived(download.match)
  const heading = $derived(match.title || download.name || download.hash)
  const season = $derived(match.season ?? 0)
  const episode = $derived(match.episode ?? 0)

  const matchLabels: Record<string, string> = {
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
    {#if match.mediaType === 'tv' && season > 0}
      <span class="badge">
        S{String(season).padStart(2, '0')}{#if episode > 0}E{String(episode).padStart(2, '0')}{/if}
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
    {#if controllable}
      <button
        class="remove icon"
        type="button"
        title="Clear from the list; the torrent keeps running in the client"
        aria-label="Clear"
        onclick={() => onclear(download.hash)}
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M18 6 6 18M6 6l12 12" />
        </svg>
      </button>
      <button
        class="remove danger icon"
        type="button"
        title="Remove the torrent from the download client"
        aria-label="Delete"
        onclick={() => (confirmingDelete = true)}
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M3 6h18" />
          <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
          <path d="M10 11v6M14 11v6" />
        </svg>
      </button>
    {/if}
  </div>

  {#if controllable && confirmingDelete}
    <div class="confirm">
      <span class="confirm-msg">Delete the downloaded files too?</span>
      <div class="confirm-actions">
        <button class="remove" type="button" onclick={() => confirmDelete(false)}>
          Keep files
        </button>
        <button class="remove danger" type="button" onclick={() => confirmDelete(true)}>
          Delete files
        </button>
        <button
          class="remove icon"
          type="button"
          title="Cancel"
          aria-label="Cancel"
          onclick={() => (confirmingDelete = false)}
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M18 6 6 18M6 6l12 12" />
          </svg>
        </button>
      </div>
    </div>
  {/if}

  {#if download.error}
    <p class="error small">{download.error}</p>
  {/if}

  {#if editing}
    <MatchPanel {download} {metadataEnabled} />
  {/if}
</article>
