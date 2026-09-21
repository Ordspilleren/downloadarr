// Package release turns a torrent's display name into the little bit of media
// metadata the mover needs. The web UI only supplies a magnet link or a
// .torrent file, so there is no metadata provider to consult: the title, year
// and episode numbers are inferred from the release name ("Show.S01E03.1080p",
// "Movie.Title.2023.…").
// The inference is best-effort — a name that does not look like TV is treated
// as a movie, with whatever release tags could be stripped.
package release

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the kind of media a release name describes.
type Kind int

const (
	// KindMovie is a single feature film.
	KindMovie Kind = iota
	// KindEpisode is a single TV episode (SxxExx or NxNN).
	KindEpisode
	// KindSeason is a whole TV season / season pack (Sxx, no episode).
	KindSeason
)

// Info is the media metadata parsed from a release name.
type Info struct {
	Kind         Kind
	Title        string // movie title or show name
	Year         int    // movie release year, 0 when unknown
	Season       int    // TV season number
	Episode      int    // TV episode number (KindEpisode only)
	EpisodeTitle string // TV episode title, empty when the name carries none
}

var (
	// separators split a release name into tokens. Hyphens are deliberately
	// kept: they are part of release tags ("WEB-DL") and of many titles
	// ("Spider-Man"), while dots, underscores, plus signs, whitespace and the
	// brackets scene releases wrap tags in ("[1080p] [WEBRip] [YTS.GG]",
	// "(2026)") are the usual stand-ins for spaces.
	separatorRE = regexp.MustCompile(`[\s._+()\[\]]+`)

	// The episode and season markers may carry a prefix glued on by a hyphen
	// ("Lioness.2023-S03E06"), so an optional hyphenated prefix is allowed.
	episodeRE    = regexp.MustCompile(`(?i)^(?:.*-)?s(\d{1,2})e(\d{1,3})$`)
	altEpisodeRE = regexp.MustCompile(`(?i)^(?:.*-)?(\d{1,2})x(\d{1,3})$`)
	seasonRE     = regexp.MustCompile(`(?i)^(?:.*-)?s(\d{1,2})$`)
	yearRE       = regexp.MustCompile(`^(?:19|20)\d{2}$`)
)

// releaseTags are tokens that mark the end of a title. Everything from the
// first one onward is release metadata, not part of the name.
var releaseTags = map[string]bool{
	"2160p": true, "1080p": true, "720p": true, "480p": true, "4k": true,
	"uhd": true, "hdr": true, "10bit": true, "8bit": true, "sdr": true,
	"hevc": true, "x265": true, "x264": true, "h264": true, "h265": true,
	"avc": true, "av1": true,
	"bluray": true, "blu-ray": true, "bdrip": true, "brrip": true, "web": true,
	"webdl": true, "web-dl": true, "webrip": true, "hdrip": true, "hdtv": true,
	"dvdrip": true, "dvd": true, "remux": true, "cam": true, "ts": true,
	"telesync": true, "telecine": true, "screener": true,
	"aac": true, "ac3": true, "eac3": true, "dts": true, "dts-hd": true,
	"truehd": true, "atmos": true, "ddp5": true, "dd5": true, "flac": true,
	"mp3": true, "opus": true,
	"proper": true, "repack": true, "unrated": true,
	"imax": true, "multi": true, "dual": true, "internal": true, "limited": true,

	// Language tags scene releases put right after the episode marker. They
	// are not part of an episode title ("…S01E03.ITA.ENG.2160p").
	"eng": true, "ita": true, "ger": true, "fre": true, "fra": true,
	"hin": true, "tam": true, "tel": true, "mal": true, "jpn": true,
	"kor": true, "chi": true, "rus": true, "por": true, "vie": true,
	"tha": true,
}

// Parse infers media metadata from a torrent display name. It never fails: an
// unrecognised name becomes a movie whose title is the cleaned release name.
func Parse(name string) Info {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	tokens := strings.Fields(separatorRE.ReplaceAllString(strings.TrimSpace(name), " "))
	if len(tokens) == 0 {
		return Info{Kind: KindMovie, Title: name}
	}

	// A single episode (S01E03 / 1x03) wins over a season pack.
	for i, tok := range tokens {
		if season, episode, ok := parseEpisode(tok); ok {
			return Info{
				Kind:         KindEpisode,
				Title:        cleanShowTitle(tokens[:i]),
				Season:       season,
				Episode:      episode,
				EpisodeTitle: cleanTitle(tokens[i+1:]),
			}
		}
	}

	// "Show.S01.…" or "Show Season 1 …": a whole season.
	for i, tok := range tokens {
		if m := seasonRE.FindStringSubmatch(tok); m != nil {
			season, _ := strconv.Atoi(m[1])
			return Info{Kind: KindSeason, Title: cleanShowTitle(tokens[:i]), Season: season}
		}
		// The season spelled out ("Prehistoric Planet (2022) Season 1 S01");
		// require a number after it so titles like "Season of the Witch" stay
		// movies.
		if strings.EqualFold(tok, "season") && i+1 < len(tokens) {
			if n := atoi(tokens[i+1]); n > 0 && n <= 99 {
				return Info{Kind: KindSeason, Title: cleanShowTitle(tokens[:i]), Season: n}
			}
		}
	}

	// Everything else is treated as a movie; pull the release year out of the
	// tokens that precede the first release tag. The last such year is the
	// release year ("Blade Runner 2049 2017 1080p" → 2017).
	cut := firstTag(tokens)
	year, yearIdx := 0, -1
	for i := 0; i < cut; i++ {
		if yearRE.MatchString(tokens[i]) {
			year, yearIdx = atoi(tokens[i]), i
		}
	}
	if yearIdx > 0 {
		return Info{Kind: KindMovie, Title: strings.Join(tokens[:yearIdx], " "), Year: year}
	}
	return Info{Kind: KindMovie, Title: cleanTitle(tokens), Year: year}
}

// parseEpisode recognises the SxxExx and NxNN episode forms.
func parseEpisode(tok string) (season, episode int, ok bool) {
	if m := episodeRE.FindStringSubmatch(tok); m != nil {
		return atoi(m[1]), atoi(m[2]), true
	}
	if m := altEpisodeRE.FindStringSubmatch(tok); m != nil {
		return atoi(m[1]), atoi(m[2]), true
	}
	return 0, 0, false
}

// cleanTitle joins tokens into a title, stopping at the first release tag.
func cleanTitle(tokens []string) string {
	return strings.TrimSpace(strings.Join(tokens[:firstTag(tokens)], " "))
}

// cleanShowTitle joins the tokens before an episode or season marker into a
// show name. A trailing release year is dropped ("Lanterns.2026.S01E02" →
// "Lanterns"), but a title that is itself a year is kept, so "1923.S01E01"
// does not lose its name.
func cleanShowTitle(tokens []string) string {
	tokens = tokens[:firstTag(tokens)]
	if len(tokens) > 1 && yearRE.MatchString(tokens[len(tokens)-1]) {
		tokens = tokens[:len(tokens)-1]
	}
	return strings.TrimSpace(strings.Join(tokens, " "))
}

// firstTag returns the index of the first release tag in tokens, or len(tokens)
// when there is none.
func firstTag(tokens []string) int {
	for i, tok := range tokens {
		if releaseTags[strings.ToLower(tok)] {
			return i
		}
	}
	return len(tokens)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
