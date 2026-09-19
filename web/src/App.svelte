<script>
  import { onMount } from 'svelte'
  import DownloadCard from './DownloadCard.svelte'

  let downloads = $state([])
  let magnet = $state('')
  let submitting = $state(false)
  let error = $state('')
  let loaded = $state(false)
  let connected = $state(false)
  let metadataEnabled = $state(false)

  async function add(event) {
    event.preventDefault()
    error = ''
    const value = magnet.trim()
    if (!value || submitting) return

    submitting = true
    try {
      const res = await fetch('/api/downloads', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ magnet: value }),
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(body.message || `Request failed (${res.status})`)
      magnet = ''
      // The new download arrives over the SSE stream below.
    } catch (err) {
      error = err.message
    } finally {
      submitting = false
    }
  }

  async function remove(hash) {
    await fetch(`/api/downloads/${hash}`, { method: 'DELETE' })
    // The removal arrives over the SSE stream below.
  }

  onMount(() => {
    fetch('/api/status')
      .then((res) => (res.ok ? res.json() : null))
      .then((body) => {
        if (body) metadataEnabled = body.metadataEnabled
      })
      .catch(() => {})

    const source = new EventSource('/api/events')

    source.onopen = () => {
      connected = true
    }
    source.onmessage = (event) => {
      try {
        downloads = JSON.parse(event.data)
        connected = true
        loaded = true
      } catch {
        // Ignore a malformed frame; the next one replaces the list anyway.
      }
    }
    // EventSource reconnects on its own; surface the gap to the user.
    source.onerror = () => {
      connected = false
    }

    return () => source.close()
  })
</script>

<main>
  <header>
    <h1>downloadarr<span>-lite</span></h1>
    <p>
      Paste a magnet link. Metadata is looked up automatically and can be
      corrected before the finished files are hard-linked into your library.
    </p>
  </header>

  <form onsubmit={add}>
    <input
      type="text"
      bind:value={magnet}
      placeholder="magnet:?xt=urn:btih:…"
      autocomplete="off"
      spellcheck="false"
      aria-label="Magnet link"
    />
    <button type="submit" disabled={submitting || !magnet.trim()}>
      {submitting ? 'Adding…' : 'Download'}
    </button>
  </form>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  <section class="downloads">
    {#if !loaded}
      <p class="muted">Loading…</p>
    {:else if downloads.length === 0}
      <p class="muted">No downloads yet. Add one above.</p>
    {:else}
      {#each downloads as d (d.hash)}
        <DownloadCard download={d} {metadataEnabled} onremove={remove} />
      {/each}
    {/if}
  </section>

  <p class="muted small">
    {#if connected}
      Live updates
    {:else}
      Connecting…
    {/if}
  </p>
</main>
