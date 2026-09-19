// Package metadata looks up movie and TV metadata on TMDB. The magnet flow only
// yields a release name, so the queue uses this client to turn that name into a
// canonical title, year, and (for TV) episode titles. A client with no API key
// is disabled: the rest of the app then falls back to release-name parsing.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.themoviedb.org/3"
	posterBaseURL  = "https://image.tmdb.org/t/p/w200"
)

// ErrDisabled is returned by lookups when no API key is configured.
var ErrDisabled = errors.New("tmdb is not configured")

// MediaType distinguishes movies from TV shows.
type MediaType string

const (
	// Movie is a feature film.
	Movie MediaType = "movie"
	// TV is a television show.
	TV MediaType = "tv"
)

// Candidate is one TMDB search hit or a resolved entry. It doubles as the JSON
// shape the web interface renders in the match editor.
type Candidate struct {
	TMDBID    int       `json:"tmdbId"`
	MediaType MediaType `json:"mediaType"`
	Title     string    `json:"title"`
	Year      int       `json:"year,omitempty"`
	Overview  string    `json:"overview,omitempty"`
	PosterURL string    `json:"posterUrl,omitempty"`
}

// Episode is one episode of a TV season, used to title season-pack imports.
type Episode struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// Client is a small TMDB API client. It accepts either a v4 bearer token
// (the JWT form TMDB hands out) or a v3 API key.
type Client struct {
	http    *http.Client
	apiKey  string
	bearer  bool
	baseURL string
}

// New creates a TMDB client. An empty key yields a disabled client.
func New(apiKey string) *Client {
	key := strings.TrimSpace(apiKey)
	return &Client{
		http:    &http.Client{Timeout: 10 * time.Second},
		apiKey:  key,
		bearer:  strings.HasPrefix(key, "eyJ"),
		baseURL: defaultBaseURL,
	}
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c.apiKey != "" }

// Search returns TMDB matches for query, most relevant first.
func (c *Client) Search(ctx context.Context, mediaType MediaType, query string) ([]Candidate, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if mediaType == TV {
		return c.searchTV(ctx, query)
	}
	return c.searchMovie(ctx, query)
}

// Get resolves a single TMDB id to its canonical metadata.
func (c *Client) Get(ctx context.Context, mediaType MediaType, id int) (Candidate, error) {
	if mediaType == TV {
		var raw struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			FirstAirDate string `json:"first_air_date"`
			Overview     string `json:"overview"`
			PosterPath   string `json:"poster_path"`
		}
		if err := c.get(ctx, fmt.Sprintf("/tv/%d", id), nil, &raw); err != nil {
			return Candidate{}, err
		}
		return Candidate{TMDBID: raw.ID, MediaType: TV, Title: raw.Name,
			Year: yearOf(raw.FirstAirDate), Overview: raw.Overview, PosterURL: posterURL(raw.PosterPath)}, nil
	}

	var raw struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
		Overview    string `json:"overview"`
		PosterPath  string `json:"poster_path"`
	}
	if err := c.get(ctx, fmt.Sprintf("/movie/%d", id), nil, &raw); err != nil {
		return Candidate{}, err
	}
	return Candidate{TMDBID: raw.ID, MediaType: Movie, Title: raw.Title,
		Year: yearOf(raw.ReleaseDate), Overview: raw.Overview, PosterURL: posterURL(raw.PosterPath)}, nil
}

// TVSeason returns the episodes of one season, used to fill in episode titles
// when importing (season packs and single episodes alike).
func (c *Client) TVSeason(ctx context.Context, tvID, season int) ([]Episode, error) {
	var raw struct {
		Episodes []struct {
			Number int    `json:"episode_number"`
			Name   string `json:"name"`
		} `json:"episodes"`
	}
	if err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tvID, season), nil, &raw); err != nil {
		return nil, err
	}
	episodes := make([]Episode, 0, len(raw.Episodes))
	for _, e := range raw.Episodes {
		episodes = append(episodes, Episode{Number: e.Number, Title: e.Name})
	}
	return episodes, nil
}

func (c *Client) searchMovie(ctx context.Context, query string) ([]Candidate, error) {
	var raw struct {
		Results []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			Overview    string `json:"overview"`
			PosterPath  string `json:"poster_path"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/movie", url.Values{"query": {query}}, &raw); err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(raw.Results))
	for _, r := range raw.Results {
		out = append(out, Candidate{TMDBID: r.ID, MediaType: Movie, Title: r.Title,
			Year: yearOf(r.ReleaseDate), Overview: r.Overview, PosterURL: posterURL(r.PosterPath)})
	}
	return out, nil
}

func (c *Client) searchTV(ctx context.Context, query string) ([]Candidate, error) {
	var raw struct {
		Results []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			FirstAirDate string `json:"first_air_date"`
			Overview     string `json:"overview"`
			PosterPath   string `json:"poster_path"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/tv", url.Values{"query": {query}}, &raw); err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(raw.Results))
	for _, r := range raw.Results {
		out = append(out, Candidate{TMDBID: r.ID, MediaType: TV, Title: r.Name,
			Year: yearOf(r.FirstAirDate), Overview: r.Overview, PosterURL: posterURL(r.PosterPath)})
	}
	return out, nil
}

// get performs a TMDB request and decodes the JSON response into out.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	if params == nil {
		params = url.Values{}
	}
	if !c.bearer {
		params.Set("api_key", c.apiKey)
	}
	endpoint := c.baseURL + path
	if encoded := params.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.bearer {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tmdb %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// yearOf extracts the year from a TMDB "YYYY-MM-DD" date, 0 when absent.
func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(date[:4])
	return year
}

func posterURL(path string) string {
	if path == "" {
		return ""
	}
	return posterBaseURL + path
}
