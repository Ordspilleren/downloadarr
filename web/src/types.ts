// API types mirroring the JSON served by the Go backend (see queue/queue.go,
// queue/match.go, and metadata/metadata.go). Keep these in sync with the struct
// tags on the server.

export type MediaType = 'movie' | 'tv'

export type DownloadStatus = 'downloading' | 'moving' | 'completed' | 'failed'

export type MatchStatus = 'pending' | 'matched' | 'manual' | 'fallback'

// Candidate is one TMDB search hit, rendered in the match editor.
export interface Candidate {
  tmdbId: number
  mediaType: MediaType
  title: string
  year?: number
  overview?: string
  posterUrl?: string
}

// Match is the metadata associated with a download.
export interface Match {
  mediaType: MediaType
  status: MatchStatus
  tmdbId?: number
  title: string
  year?: number
  season?: number
  episode?: number
  episodeTitle?: string
  overview?: string
  posterUrl?: string
  candidates?: Candidate[]
}

// Download is one tracked magnet.
export interface Download {
  hash: string
  name: string
  status: DownloadStatus
  progress: number
  destination?: string
  error?: string
  addedAt: string
  match: Match
}

// MatchInput is the correction sent when the user picks a different title.
export interface MatchInput {
  mediaType: MediaType
  tmdbId: number
  title: string
  year: number
  season: number
  episode: number
}

// StatusResponse reports which optional integrations are active.
export interface StatusResponse {
  metadataEnabled: boolean
}

// ApiError is the shape the server returns for failed requests.
export interface ApiError {
  message?: string
}

// errorMessage extracts a human-readable message from an unknown thrown value.
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
