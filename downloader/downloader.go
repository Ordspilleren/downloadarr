// Package downloader talks to a torrent client. It supports qBittorrent and a
// small in-memory fake used for development and demos.
package downloader

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/go-qbittorrent"

	"github.com/Ordspilleren/downloadarr-lite/release"
)

// ErrNotFound is returned by GetTorrent while the client does not (yet) know a
// hash. Callers can treat it as transient directly after AddTorrent.
var ErrNotFound = errors.New("torrent not found in client")

// Config holds the settings for the qBittorrent client.
type Config struct {
	URL      string
	Username string
	Password string
	Category string
}

// TorrentInfo is the download state the queue polls.
type TorrentInfo struct {
	Name        string
	Progress    float64 // 0.0 – 1.0
	Complete    bool
	ContentPath string
}

// Client is the download-client interface the queue depends on.
type Client interface {
	// AddTorrent hands a magnet link to the client. It must be idempotent
	// enough that a duplicate add does not fail the request.
	AddTorrent(ctx context.Context, magnet string) error
	// AddTorrentFile hands the raw bytes of a .torrent file to the client.
	AddTorrentFile(ctx context.Context, data []byte) error
	// GetTorrent reports the state of a torrent by info hash, or ErrNotFound.
	GetTorrent(ctx context.Context, hash string) (TorrentInfo, error)
}

// New creates a client by type: "qbittorrent" or "fake".
func New(clientType string, cfg Config) (Client, error) {
	switch clientType {
	case "qbittorrent":
		return newQBittorrent(cfg)
	case "fake":
		return newFake(), nil
	default:
		return nil, fmt.Errorf("unsupported downloader type %q (want qbittorrent or fake)", clientType)
	}
}

// ──────────────────────────────────────────────────────────────
// qBittorrent
// ──────────────────────────────────────────────────────────────

type qbittorrentClient struct {
	client *qbittorrent.Client
	config Config
}

func newQBittorrent(cfg Config) (*qbittorrentClient, error) {
	client := qbittorrent.NewClient(qbittorrent.Config{
		Host:     cfg.URL,
		Username: cfg.Username,
		Password: cfg.Password,
	})
	if err := client.Login(); err != nil {
		return nil, fmt.Errorf("failed to login to qBittorrent: %w", err)
	}
	return &qbittorrentClient{client: client, config: cfg}, nil
}

func (q *qbittorrentClient) AddTorrent(ctx context.Context, magnet string) error {
	options := &qbittorrent.TorrentAddOptions{Paused: false, Category: q.config.Category}
	if _, err := q.client.AddTorrentFromUrlCtx(ctx, magnet, options.Prepare()); err != nil {
		return fmt.Errorf("failed to add magnet link: %w", err)
	}
	return nil
}

func (q *qbittorrentClient) AddTorrentFile(ctx context.Context, data []byte) error {
	options := &qbittorrent.TorrentAddOptions{Paused: false, Category: q.config.Category}
	if _, err := q.client.AddTorrentFromMemoryCtx(ctx, data, options.Prepare()); err != nil {
		return fmt.Errorf("failed to add torrent file: %w", err)
	}
	return nil
}

func (q *qbittorrentClient) GetTorrent(ctx context.Context, hash string) (TorrentInfo, error) {
	torrents, err := q.client.GetTorrentsCtx(ctx, qbittorrent.TorrentFilterOptions{Hashes: []string{hash}})
	if err != nil {
		return TorrentInfo{}, fmt.Errorf("failed to get torrent status: %w", err)
	}
	if len(torrents) == 0 {
		return TorrentInfo{}, fmt.Errorf("%w: %s", ErrNotFound, hash)
	}
	t := torrents[0]
	switch t.State {
	case qbittorrent.TorrentStateError, qbittorrent.TorrentStateMissingFiles:
		return TorrentInfo{}, fmt.Errorf("torrent %s is in state %s", hash, t.State)
	}
	path := t.ContentPath
	if path == "" {
		path = filepath.Join(t.SavePath, t.Name)
	}
	return TorrentInfo{
		Name:        t.Name,
		Progress:    t.Progress,
		Complete:    t.Progress >= 1,
		ContentPath: path,
	}, nil
}

// ──────────────────────────────────────────────────────────────
// Fake client (development)
// ──────────────────────────────────────────────────────────────

// fakeClient simulates a torrent client: a torrent "completes" a few seconds
// after being added and materialises a plausible file layout under a temp
// directory. It lets the full magnet → download → move pipeline be exercised
// without a running qBittorrent.
type fakeClient struct {
	mu    sync.Mutex
	added map[string]time.Time
	names map[string]string
}

func newFake() *fakeClient {
	return &fakeClient{added: make(map[string]time.Time), names: make(map[string]string)}
}

func (f *fakeClient) AddTorrent(ctx context.Context, magnet string) error {
	hash, err := ParseInfoHash(magnet)
	if err != nil {
		return err
	}
	f.register(hash, MagnetDisplayName(magnet))
	return nil
}

func (f *fakeClient) AddTorrentFile(ctx context.Context, data []byte) error {
	info, err := ParseTorrentFile(data)
	if err != nil {
		return err
	}
	f.register(info.Hash, info.Name)
	return nil
}

// register records a torrent under its info hash, keeping the first name seen.
func (f *fakeClient) register(hash, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.added[hash]; !ok {
		f.added[hash] = time.Now()
	}
	if _, ok := f.names[hash]; !ok {
		f.names[hash] = name
	}
}

func (f *fakeClient) GetTorrent(ctx context.Context, hash string) (TorrentInfo, error) {
	f.mu.Lock()
	added, ok := f.added[hash]
	name := f.names[hash]
	f.mu.Unlock()
	if !ok {
		return TorrentInfo{}, fmt.Errorf("%w: %s", ErrNotFound, hash)
	}
	if name == "" {
		name = hash
	}
	progress := time.Since(added).Seconds() / 3
	if progress > 1 {
		progress = 1
	}
	info := TorrentInfo{Name: name, Progress: progress, Complete: progress >= 1}
	if info.Complete {
		path, err := fakeContent(hash, name)
		if err != nil {
			return TorrentInfo{}, err
		}
		info.ContentPath = path
	}
	return info, nil
}

// MagnetDisplayName extracts the display name (dn) from a magnet link, which
// is the release name the torrent client will report once metadata arrives.
func MagnetDisplayName(magnet string) string {
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	return u.Query().Get("dn")
}

// fakeContent creates the files a completed fake torrent contains. Episode and
// season names get SxxExx files so the season-pack import path is exercised
// too; everything else becomes a single feature file.
func fakeContent(hash, name string) (string, error) {
	dir := filepath.Join(os.TempDir(), "downloadarr-lite-fake", hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	info := release.Parse(name)
	base := strings.ReplaceAll(info.Title, "/", "_")
	if base == "" {
		base = "download"
	}
	files := map[string]int64{}
	switch info.Kind {
	case release.KindSeason:
		for n := 1; n <= 3; n++ {
			files[fmt.Sprintf("%s.S%02dE%02d.mkv", base, info.Season, n)] = 1 << 20
		}
	case release.KindEpisode:
		files[fmt.Sprintf("%s.S%02dE%02d.mkv", base, info.Season, info.Episode)] = 2 << 20
	default:
		files[base+".mkv"] = 2 << 20
	}
	for rel, size := range files {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, make([]byte, size), 0644); err != nil {
				return "", err
			}
		}
	}
	return dir, nil
}

// ──────────────────────────────────────────────────────────────
// Magnet link parsing
// ──────────────────────────────────────────────────────────────

// ParseInfoHash extracts the btih info hash from a magnet link, accepting both
// the 40-char hex and 32-char base32 forms and returning lowercase hex.
func ParseInfoHash(magnet string) (string, error) {
	u, err := url.Parse(magnet)
	if err != nil {
		return "", fmt.Errorf("failed to parse magnet link: %w", err)
	}
	if u.Scheme != "magnet" {
		return "", errors.New("not a magnet link")
	}
	for _, xt := range u.Query()["xt"] {
		const prefix = "urn:btih:"
		if !strings.HasPrefix(xt, prefix) {
			continue
		}
		value := strings.TrimPrefix(xt, prefix)
		switch {
		case len(value) == 40:
			raw, err := hex.DecodeString(value)
			if err != nil {
				continue
			}
			return hex.EncodeToString(raw), nil
		case len(value) == 32:
			raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(value))
			if err != nil {
				continue
			}
			return hex.EncodeToString(raw), nil
		}
	}
	return "", errors.New("no btih info hash found in magnet link")
}
