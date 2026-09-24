// Helpers for the dual-purpose add field: a magnet link downloads, anything
// else is treated as keywords and turned into tracker search links.

// isMagnetLink reports whether the trimmed input should be treated as a magnet.
export function isMagnetLink(value: string): boolean {
  return value.trim().toLowerCase().startsWith('magnet:')
}

// buildTrackerUrl substitutes URL-encoded keywords for every "{query}" in the
// template. Templates without the placeholder are validated away in Go.
export function buildTrackerUrl(template: string, query: string): string {
  return template.replaceAll('{query}', encodeURIComponent(query.trim()))
}
