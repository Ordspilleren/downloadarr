package queue

import (
	"log"

	"github.com/Ordspilleren/downloadarr/mediamover"
	"github.com/Ordspilleren/downloadarr/metadata"
)

// importContent moves a completed download into the library using its matched
// metadata. A TV release with an episode number is imported as a single
// episode, unless the content holds several video files (an episode grab that
// was actually a pack), in which case the whole pack is imported.
func (q *Queue) importContent(d *Download, contentPath string) (string, error) {
	m := d.Match
	if m.MediaType == metadata.TV {
		if m.Season > 0 && m.Episode > 0 {
			multi, err := mediamover.HasMultipleVideoFiles(contentPath)
			if err != nil {
				return "", err
			}
			if multi {
				return q.mover.MoveSeasonPack(contentPath, q.seasonPackInfo(d))
			}
			title := m.EpisodeTitle
			if looked := q.lookupEpisodeTitle(d); looked != "" {
				title = looked
			}
			return q.mover.MoveEpisode(contentPath, mediamover.TVEpisodeInfo{
				ShowName:      m.Title,
				SeasonNumber:  m.Season,
				EpisodeNumber: m.Episode,
				EpisodeTitle:  title,
			})
		}
		return q.mover.MoveSeasonPack(contentPath, q.seasonPackInfo(d))
	}

	title := m.Title
	if title == "" {
		title = d.Name
	}
	return q.mover.MoveMovie(contentPath, mediamover.MovieInfo{MovieName: title, Year: m.Year})
}

// seasonPackInfo builds the mover input for a season pack, fetching episode
// titles from TMDB when the pack is matched to a known show.
func (q *Queue) seasonPackInfo(d *Download) mediamover.SeasonPackInfo {
	info := mediamover.SeasonPackInfo{ShowName: d.Match.Title, SeasonNumber: d.Match.Season}
	episodes, err := q.seasonEpisodes(d)
	if err != nil {
		log.Printf("Metadata: season %d of %q: %v", d.Match.Season, d.Match.Title, err)
		return info
	}
	for _, e := range episodes {
		info.Episodes = append(info.Episodes, mediamover.SeasonPackEpisode{EpisodeNumber: e.Number, Title: e.Title})
	}
	return info
}

// lookupEpisodeTitle returns the matched episode's title from TMDB, or "".
func (q *Queue) lookupEpisodeTitle(d *Download) string {
	episodes, err := q.seasonEpisodes(d)
	if err != nil {
		return ""
	}
	for _, e := range episodes {
		if e.Number == d.Match.Episode {
			return e.Title
		}
	}
	return ""
}

// seasonEpisodes fetches the episodes of the matched season, or nil when no
// lookup is possible (no TMDB id, no season, or TMDB disabled).
func (q *Queue) seasonEpisodes(d *Download) ([]metadata.Episode, error) {
	if d.Match.TMDBID <= 0 || d.Match.Season <= 0 || !q.metadata.Enabled() {
		return nil, nil
	}
	return q.metadata.TVSeason(q.ctx, d.Match.TMDBID, d.Match.Season)
}
