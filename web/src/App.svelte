<script lang="ts">
  import { onMount } from 'svelte'
  import DownloadCard from './DownloadCard.svelte'
  import { errorMessage, type ApiError, type Download, type StatusResponse } from './types'

  let downloads = $state<Download[]>([])
  let magnet = $state('')
  let submitting = $state(false)
  let uploading = $state(false)
  let dragging = $state(false)
  // dragenter/dragleave fire for every element the pointer crosses, so a
  // depth counter keeps the overlay from flickering over child elements.
  let dragDepth = 0
  let fileInput: HTMLInputElement | undefined = $state()
  let error = $state('')
  let loaded = $state(false)
  let connected = $state(false)
  let metadataEnabled = $state(false)

  async function add(event: SubmitEvent) {
    event.preventDefault()
    error = ''
    const value = magnet.trim()
    if (!value || submitting || uploading) return

    submitting = true
    try {
      const res = await fetch('/api/downloads', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ magnet: value }),
      })
      const body = (await res.json().catch(() => ({}))) as ApiError
      if (!res.ok) throw new Error(body.message || `Request failed (${res.status})`)
      magnet = ''
      // The new download arrives over the SSE stream below.
    } catch (err) {
      error = errorMessage(err)
    } finally {
      submitting = false
    }
  }

  // uploadFile sends a .torrent file as multipart form data to the same
  // endpoint, which detects the content type and parses the info hash.
  async function uploadFile(file: File) {
    if (uploading || submitting) return
    if (!file.name.toLowerCase().endsWith('.torrent')) {
      error = 'Only .torrent files can be added.'
      return
    }

    error = ''
    uploading = true
    const form = new FormData()
    form.append('file', file)
    try {
      const res = await fetch('/api/downloads', { method: 'POST', body: form })
      const body = (await res.json().catch(() => ({}))) as ApiError
      if (!res.ok) throw new Error(body.message || `Request failed (${res.status})`)
    } catch (err) {
      error = errorMessage(err)
    } finally {
      uploading = false
    }
  }

  function addFile(event: Event) {
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    if (file) void uploadFile(file)
    // Clear the picker so choosing the same file again re-uploads it.
    input.value = ''
  }

  // ── drag and drop ────────────────────────────────────────────

  function hasFiles(event: DragEvent): boolean {
    return !!event.dataTransfer && Array.from(event.dataTransfer.types).includes('Files')
  }

  function onDragEnter(event: DragEvent) {
    if (!hasFiles(event)) return
    event.preventDefault()
    dragDepth++
    dragging = true
  }

  function onDragOver(event: DragEvent) {
    if (!hasFiles(event)) return
    // Preventing the default is what allows the drop to fire at all.
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy'
  }

  function onDragLeave() {
    if (!dragging) return
    dragDepth = Math.max(0, dragDepth - 1)
    if (dragDepth === 0) dragging = false
  }

  function onDrop(event: DragEvent) {
    if (!hasFiles(event)) return
    event.preventDefault()
    dragDepth = 0
    dragging = false
    const file = event.dataTransfer?.files?.[0]
    if (file) void uploadFile(file)
  }

  async function remove(hash: string) {
    await fetch(`/api/downloads/${hash}`, { method: 'DELETE' })
    // The removal arrives over the SSE stream below.
  }

  onMount(() => {
    fetch('/api/status')
      .then((res) => (res.ok ? res.json() : null))
      .then((body: StatusResponse | null) => {
        if (body) metadataEnabled = body.metadataEnabled
      })
      .catch(() => {})

    const source = new EventSource('/api/events')

    source.onopen = () => {
      connected = true
    }
    source.onmessage = (event: MessageEvent<string>) => {
      try {
        downloads = JSON.parse(event.data) as Download[]
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

<svelte:window
  ondragenter={onDragEnter}
  ondragover={onDragOver}
  ondragleave={onDragLeave}
  ondrop={onDrop}
/>

<main>
  <header>
    <h1>download<span>arr</span></h1>
    <p>
      Paste a magnet link or upload a .torrent file.
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
    <button type="submit" disabled={submitting || uploading || !magnet.trim()}>
      {submitting ? 'Adding…' : 'Download'}
    </button>
    <button
      type="button"
      class="file"
      disabled={submitting || uploading}
      onclick={() => fileInput?.click()}
    >
      {uploading ? 'Uploading…' : '.torrent file'}
    </button>
    <input
      bind:this={fileInput}
      class="file-input"
      type="file"
      accept=".torrent,application/x-bittorrent"
      onchange={addFile}
    />
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

{#if dragging}
  <div class="dropzone" aria-hidden="true">
    <p>Drop the .torrent file to add it</p>
  </div>
{/if}
