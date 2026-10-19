// Package queue tracks torrent downloads from add to import. It polls the
// download client for progress, looks up metadata for each release, and once a
// torrent completes hands the content to the mover. Torrents can be added as a
// magnet link or an uploaded .torrent file. State is kept in memory: this is a
// deliberately small tool, not a durable job system, so the list is empty
// again after a restart.
package queue

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/Ordspilleren/downloadarr/downloader"
	"github.com/Ordspilleren/downloadarr/mediamover"
	"github.com/Ordspilleren/downloadarr/metadata"
	"github.com/Ordspilleren/downloadarr/release"
)

// ErrDuplicate is returned by Add when the info hash is already tracked.
var ErrDuplicate = errors.New("download already exists")

// ErrNotFound is returned when a download hash is not tracked.
var ErrNotFound = errors.New("download not found")

// ErrImporting is returned when a download cannot be removed because it is
// already being imported into the library.
var ErrImporting = errors.New("download is being imported")

// Status is the lifecycle state of a download.
type Status string

const (
	// StatusDownloading means the client is still fetching the torrent.
	StatusDownloading Status = "downloading"
	// StatusMoving means the torrent finished and the mover is importing it.
	StatusMoving Status = "moving"
	// StatusCompleted means the content was imported into the library.
	StatusCompleted Status = "completed"
	// StatusFailed means the download or the import failed; Error says why.
	StatusFailed Status = "failed"
)

// Match status values.
const (
	// MatchPending means a TMDB lookup is in flight.
	MatchPending = "pending"
	// MatchMatched means TMDB was matched automatically.
	MatchMatched = "matched"
	// MatchManual means the user picked or overrode the metadata.
	MatchManual = "manual"
	// MatchFallback means no TMDB match; the release name is used as-is.
	MatchFallback = "fallback"
)

// Match is the metadata associated with a download. It starts as a guess parsed
// from the release name and is refined by TMDB, either automatically after the
// magnet is added or by the user.
type Match struct {
	MediaType    metadata.MediaType   `json:"mediaType"`
	Status       string               `json:"status"`
	TMDBID       int                  `json:"tmdbId,omitempty"`
	Title        string               `json:"title"`
	Year         int                  `json:"year,omitempty"`
	Season       int                  `json:"season,omitempty"`
	Episode      int                  `json:"episode,omitempty"`
	EpisodeTitle string               `json:"episodeTitle,omitempty"`
	Overview     string               `json:"overview,omitempty"`
	PosterURL    string               `json:"posterUrl,omitempty"`
	Candidates   []metadata.Candidate `json:"candidates,omitempty"`
}

// Download is one tracked torrent.
type Download struct {
	Hash        string    `json:"hash"`
	Name        string    `json:"name"`
	Status      Status    `json:"status"`
	Progress    float64   `json:"progress"`
	Destination string    `json:"destination,omitempty"`
	Error       string    `json:"error,omitempty"`
	AddedAt     time.Time `json:"addedAt"`
	Match       Match     `json:"match"`

	// matchStarted/matchDone coordinate the asynchronous TMDB lookup with the
	// import: the importer waits on matchDone so it never moves a torrent with
	// only the release-name guess while a lookup is still running.
	matchStarted bool
	matchDone    chan struct{}

	// removing is set while a removal is talking to the download client, so
	// the watch loop defers an import until the removal settles and a duplicate
	// add is still rejected. It is in-memory only and never serialized.
	removing bool
}

func (d *Download) clone() *Download {
	c := *d
	c.Match.Candidates = append([]metadata.Candidate(nil), d.Match.Candidates...)
	return &c
}

// Queue watches downloads and imports completed ones.
type Queue struct {
	client   downloader.Client
	mover    *mediamover.MediaMover
	metadata *metadata.Client
	poll     time.Duration

	mu        sync.Mutex
	order     []string
	downloads map[string]*Download
	subs      map[chan []Download]struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a Queue. poll is how often each active download is checked.
func New(client downloader.Client, mover *mediamover.MediaMover, meta *metadata.Client, poll time.Duration) *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{
		client:    client,
		mover:     mover,
		metadata:  meta,
		poll:      poll,
		downloads: make(map[string]*Download),
		subs:      make(map[chan []Download]struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Close stops all watch loops, leaving in-flight torrents in the client.
func (q *Queue) Close() { q.cancel() }

// Add validates the magnet, registers it, and starts watching it. A metadata
// lookup is kicked off in the background; the caller gets the release-name
// guess immediately. It returns ErrDuplicate when the hash is already tracked.
func (q *Queue) Add(magnet string) (*Download, error) {
	hash, err := downloader.ParseInfoHash(magnet)
	if err != nil {
		return nil, err
	}
	return q.add(hash, downloader.MagnetDisplayName(magnet), func(ctx context.Context) error {
		return q.client.AddTorrent(ctx, magnet)
	})
}

// AddFile validates an uploaded .torrent file, registers it, and starts
// watching it. fallbackName is used as the display name when the file carries
// none. It returns ErrDuplicate when the hash is already tracked.
func (q *Queue) AddFile(data []byte, fallbackName string) (*Download, error) {
	info, err := downloader.ParseTorrentFile(data)
	if err != nil {
		return nil, err
	}
	name := info.Name
	if name == "" {
		name = fallbackName
	}
	return q.add(info.Hash, name, func(ctx context.Context) error {
		return q.client.AddTorrentFile(ctx, data)
	})
}

// add registers a download under hash and asks the client to fetch it. Magnet
// links and .torrent files share the rest of the lifecycle from here on.
func (q *Queue) add(hash, displayName string, fetch func(context.Context) error) (*Download, error) {
	match := seedMatch(release.Parse(displayName))
	if q.metadata.Enabled() {
		match.Status = MatchPending
	} else {
		match.Status = MatchFallback
	}

	q.mu.Lock()
	if _, ok := q.downloads[hash]; ok {
		q.mu.Unlock()
		return nil, ErrDuplicate
	}
	d := &Download{Hash: hash, Status: StatusDownloading, AddedAt: time.Now(), Match: match}
	if q.metadata.Enabled() && displayName != "" {
		beginMatchLocked(d)
	}
	q.downloads[hash] = d
	q.order = append(q.order, hash)
	q.notifyLocked()
	q.mu.Unlock()

	if err := fetch(q.ctx); err != nil {
		q.fail(hash, err.Error())
		return q.get(hash), err
	}
	go q.watch(hash)
	if d.matchStarted {
		go q.matchDownload(hash)
	}
	return q.get(hash), nil
}

// List returns every tracked download, newest first.
func (q *Queue) List() []Download {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.listLocked()
}

// Subscribe returns a channel that receives the full download list whenever it
// changes, plus a function that unsubscribes and closes the channel. The
// current list is delivered immediately. The channel holds only the newest
// snapshot, so a slow consumer may miss intermediate states but never blocks
// the queue.
func (q *Queue) Subscribe() (<-chan []Download, func()) {
	ch := make(chan []Download, 1)
	q.mu.Lock()
	q.subs[ch] = struct{}{}
	ch <- q.listLocked()
	q.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			q.mu.Lock()
			if _, ok := q.subs[ch]; ok {
				delete(q.subs, ch)
				close(ch)
			}
			q.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

// listLocked builds the newest-first snapshot. Callers must hold q.mu.
func (q *Queue) listLocked() []Download {
	out := make([]Download, 0, len(q.order))
	for i := len(q.order) - 1; i >= 0; i-- {
		if d, ok := q.downloads[q.order[i]]; ok {
			out = append(out, *d.clone())
		}
	}
	return out
}

// notifyLocked pushes the current list to every subscriber. Callers must hold
// q.mu. Sends are non-blocking: an unread snapshot is simply replaced.
func (q *Queue) notifyLocked() {
	if len(q.subs) == 0 {
		return
	}
	snapshot := q.listLocked()
	for ch := range q.subs {
		select {
		case <-ch: // drop the stale snapshot
		default:
		}
		select {
		case ch <- snapshot:
		default:
		}
	}
}

// Clear forgets a download without touching the download client: the torrent
// keeps downloading or seeding, it just leaves the in-memory list. A download
// that is being removed or imported still needs its entry, so it cannot be
// cleared yet.
func (q *Queue) Clear(hash string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.downloads[hash]
	if !ok {
		return ErrNotFound
	}
	if d.Status == StatusMoving || d.removing {
		return ErrImporting
	}
	q.removeLocked(hash)
	q.notifyLocked()
	return nil
}

// Delete removes a download from the download client and forgets it. A
// download that is still fetching is cancelled; when deleteFiles is set the
// downloaded data (for an incomplete download, the partial files) is deleted
// too. The only state that cannot be deleted is an import already in progress,
// which needs the entry to finish.
func (q *Queue) Delete(hash string, deleteFiles bool) error {
	q.mu.Lock()
	d, ok := q.downloads[hash]
	if !ok {
		q.mu.Unlock()
		return ErrNotFound
	}
	if d.Status == StatusMoving || d.removing {
		q.mu.Unlock()
		return ErrImporting
	}
	// Mark the download while the client call is in flight: the watch loop
	// defers an import until the removal settles, and a duplicate add is still
	// rejected because the entry stays registered.
	d.removing = true
	q.mu.Unlock()

	// A hash the client already forgot is not an error: the goal is for it to
	// be gone.
	if err := q.client.RemoveTorrent(q.ctx, hash, deleteFiles); err != nil && !errors.Is(err, downloader.ErrNotFound) {
		// Let the watch loop and match lookups resume; the entry stays tracked
		// so the user can retry.
		q.update(hash, func(d *Download) bool {
			d.removing = false
			return true
		})
		return err
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.removeLocked(hash)
	q.notifyLocked()
	return nil
}

// removeLocked deletes a download and its order entry. Callers must hold q.mu.
func (q *Queue) removeLocked(hash string) {
	delete(q.downloads, hash)
	for i, h := range q.order {
		if h == hash {
			q.order = append(q.order[:i], q.order[i+1:]...)
			break
		}
	}
}

// watch polls one torrent until it completes, then imports it.
func (q *Queue) watch(hash string) {
	ticker := time.NewTicker(q.poll)
	defer ticker.Stop()
	misses := 0
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
		}

		info, err := q.client.GetTorrent(q.ctx, hash)
		if err != nil {
			if errors.Is(err, downloader.ErrNotFound) {
				if q.get(hash) == nil {
					return // the download was removed
				}
				// The client may not list the torrent in the moment right after
				// AddTorrent; give it a grace window before giving up.
				if misses++; misses >= 30 {
					q.fail(hash, "torrent disappeared from the client")
					return
				}
				continue
			}
			q.fail(hash, err.Error())
			return
		}
		misses = 0

		if !q.update(hash, func(d *Download) bool {
			changed := d.Progress != info.Progress
			d.Progress = info.Progress
			if d.Name == "" && info.Name != "" {
				d.Name = info.Name
				changed = true
			}
			return changed
		}) {
			return // entry was removed
		}
		// A magnet without a display name only becomes identifiable once the
		// client reports the torrent's name.
		q.startMatchIfNeeded(hash)
		if info.Complete {
			// Keep polling when an import is deferred because a removal is in
			// flight; the next tick either imports or finds the entry gone.
			if q.importTorrent(hash, info) {
				return
			}
		}
	}
}

// claimImport reports whether the caller may import the torrent, and whether
// the watch loop should keep polling. While a removal is in flight it returns
// (false, true) so the watcher retries once the removal settles; for any other
// non-downloading state it returns (false, false) so the watcher stops.
func (q *Queue) claimImport(hash, name string) (claimed, retry bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.downloads[hash]
	if !ok || d.Status != StatusDownloading {
		return false, false
	}
	if d.removing {
		return false, true
	}
	d.Status = StatusMoving
	d.Progress = 1
	if d.Name == "" {
		d.Name = name
	}
	q.notifyLocked()
	return true, false
}

// importTorrent moves a completed torrent's content into the library. It
// returns whether the watch loop should stop: false means an import is
// deferred because a removal is in flight, so the caller should keep polling.
func (q *Queue) importTorrent(hash string, info downloader.TorrentInfo) bool {
	// Give an in-flight TMDB lookup a moment to finish, so the import uses the
	// matched metadata rather than the release-name guess.
	q.waitForMatch(hash)

	claimed, retry := q.claimImport(hash, info.Name)
	if !claimed {
		return !retry
	}

	d := q.get(hash)
	if d == nil {
		return true
	}
	dest, err := q.importContent(d, info.ContentPath)
	if err != nil {
		q.fail(hash, err.Error())
		return true
	}
	q.update(hash, func(d *Download) bool {
		d.Status = StatusCompleted
		d.Destination = dest
		return true
	})
	log.Printf("Imported %q -> %s", d.Match.Title, dest)
	return true
}

// fail records the error on a tracked download.
func (q *Queue) fail(hash, msg string) {
	q.update(hash, func(d *Download) bool {
		d.Status = StatusFailed
		d.Error = msg
		return true
	})
	log.Printf("Download %s failed: %s", hash, msg)
}

// get returns a copy of a tracked download, or nil.
func (q *Queue) get(hash string) *Download {
	q.mu.Lock()
	defer q.mu.Unlock()
	if d, ok := q.downloads[hash]; ok {
		return d.clone()
	}
	return nil
}

// update mutates a tracked download under the lock, notifying subscribers when
// fn reports a change. It returns false when the entry no longer exists.
func (q *Queue) update(hash string, fn func(*Download) bool) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, ok := q.downloads[hash]
	if !ok {
		return false
	}
	if fn(d) {
		q.notifyLocked()
	}
	return true
}
