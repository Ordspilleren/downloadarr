package queue

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Ordspilleren/downloadarr-lite/metadata"
	"github.com/Ordspilleren/downloadarr-lite/release"
)

// seedMatch turns a parsed release name into the initial metadata guess.
func seedMatch(info release.Info) Match {
	m := Match{
		Title:        info.Title,
		Year:         info.Year,
		Season:       info.Season,
		Episode:      info.Episode,
		EpisodeTitle: info.EpisodeTitle,
	}
	if info.Kind == release.KindMovie {
		m.MediaType = metadata.Movie
	} else {
		m.MediaType = metadata.TV
	}
	return m
}

// startMatchIfNeeded seeds the match from the torrent name when the magnet had
// no display name, and starts the TMDB lookup. It is idempotent.
func (q *Queue) startMatchIfNeeded(hash string) {
	q.mu.Lock()
	d := q.downloads[hash]
	if d == nil || d.matchStarted || !q.metadata.Enabled() || d.Name == "" {
		q.mu.Unlock()
		return
	}
	d.matchStarted = true
	d.matchDone = make(chan struct{})
	d.Match = seedMatch(release.Parse(d.Name))
	d.Match.Status = MatchPending
	q.notifyLocked()
	q.mu.Unlock()

	go q.matchDownload(hash)
}

// matchDownload searches TMDB for the release and records the best hit. It
// always signals completion so an import waiting on the match does not hang.
func (q *Queue) matchDownload(hash string) {
	defer q.finishMatch(hash)

	d := q.get(hash)
	if d == nil {
		return
	}
	query := strings.TrimSpace(d.Match.Title)
	if query == "" {
		query = strings.TrimSpace(d.Name)
	}
	if query == "" {
		return
	}

	ctx, cancel := context.WithTimeout(q.ctx, 15*time.Second)
	defer cancel()

	candidates, err := q.metadata.Search(ctx, d.Match.MediaType, query)
	if err != nil {
		log.Printf("Metadata lookup for %q failed: %v", query, err)
		q.markFallback(hash)
		return
	}
	if len(candidates) == 0 {
		q.markFallback(hash)
		return
	}

	best := pickCandidate(candidates, d.Match.Year)
	q.update(hash, func(d *Download) bool {
		if d.Match.Status != MatchPending {
			return false // the user already corrected it
		}
		d.Match.Status = MatchMatched
		d.Match.TMDBID = best.TMDBID
		d.Match.Title = best.Title
		d.Match.Year = best.Year
		d.Match.Overview = best.Overview
		d.Match.PosterURL = best.PosterURL
		d.Match.Candidates = candidates
		return true
	})
}

// markFallback switches a still-pending match to the release-name guess.
func (q *Queue) markFallback(hash string) {
	q.update(hash, func(d *Download) bool {
		if d.Match.Status != MatchPending {
			return false
		}
		d.Match.Status = MatchFallback
		return true
	})
}

// waitForMatch blocks until the pending TMDB lookup finishes, the app shuts
// down, or the timeout elapses.
func (q *Queue) waitForMatch(hash string) {
	q.mu.Lock()
	var done chan struct{}
	if d := q.downloads[hash]; d != nil {
		done = d.matchDone
	}
	q.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	case <-q.ctx.Done():
	}
}

// finishMatch closes a download's matchDone channel exactly once.
func (q *Queue) finishMatch(hash string) {
	q.mu.Lock()
	d := q.downloads[hash]
	if d != nil && d.matchDone != nil {
		close(d.matchDone)
		d.matchDone = nil
	}
	q.mu.Unlock()
}

// MatchInput is the correction the web interface sends when the user picks a
// different movie or series.
type MatchInput struct {
	MediaType metadata.MediaType `json:"mediaType"`
	TMDBID    int                `json:"tmdbId"`
	Title     string             `json:"title"`
	Year      int                `json:"year"`
	Season    int                `json:"season"`
	Episode   int                `json:"episode"`
}

// SetMatch replaces a download's metadata with the user's choice, resolving a
// TMDB id to canonical details. A title without a TMDB id is accepted as a
// manual override for content TMDB does not have.
func (q *Queue) SetMatch(hash string, in MatchInput) (*Download, error) {
	if q.get(hash) == nil {
		return nil, ErrNotFound
	}
	if in.MediaType != metadata.TV {
		in.MediaType = metadata.Movie
	}

	if in.TMDBID > 0 && q.metadata.Enabled() {
		c, err := q.metadata.Get(q.ctx, in.MediaType, in.TMDBID)
		if err != nil {
			return nil, err
		}
		if !q.update(hash, func(d *Download) bool {
			d.Match = Match{
				MediaType:    c.MediaType,
				Status:       MatchManual,
				TMDBID:       c.TMDBID,
				Title:        c.Title,
				Year:         c.Year,
				Overview:     c.Overview,
				PosterURL:    c.PosterURL,
				Season:       in.Season,
				Episode:      in.Episode,
				EpisodeTitle: d.Match.EpisodeTitle,
			}
			return true
		}) {
			return nil, ErrNotFound
		}
		return q.get(hash), nil
	}

	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, errors.New("a TMDB id or a title is required")
	}
	if !q.update(hash, func(d *Download) bool {
		d.Match = Match{
			MediaType:    in.MediaType,
			Status:       MatchManual,
			Title:        in.Title,
			Year:         in.Year,
			Season:       in.Season,
			Episode:      in.Episode,
			EpisodeTitle: d.Match.EpisodeTitle,
		}
		return true
	}) {
		return nil, ErrNotFound
	}
	return q.get(hash), nil
}

// pickCandidate chooses the best search hit, preferring an exact release-year
// match and then a neighbouring year; TMDB relevance order breaks ties.
func pickCandidate(candidates []metadata.Candidate, year int) metadata.Candidate {
	if year > 0 {
		for _, c := range candidates {
			if c.Year == year {
				return c
			}
		}
		for _, c := range candidates {
			if abs(c.Year-year) == 1 {
				return c
			}
		}
	}
	return candidates[0]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
