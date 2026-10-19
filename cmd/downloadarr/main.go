// Command downloadarr is a tiny web app for importing torrents: paste a
// magnet link or upload a .torrent file, the torrent client downloads it, and
// the mover hard-links the finished content into the movie or TV library.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ordspilleren/downloadarr/config"
	"github.com/Ordspilleren/downloadarr/downloader"
	"github.com/Ordspilleren/downloadarr/internal/fsutil"
	"github.com/Ordspilleren/downloadarr/internal/webui"
	"github.com/Ordspilleren/downloadarr/mediamover"
	"github.com/Ordspilleren/downloadarr/metadata"
	"github.com/Ordspilleren/downloadarr/queue"
	"github.com/Ordspilleren/downloadarr/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	paths := cfg.MediaPaths.ToMoverPaths()
	mover, err := mediamover.New(paths)
	if err != nil {
		log.Fatalf("Invalid path template: %v", err)
	}
	for _, dir := range []string{paths.MoviesDir, paths.TVDir} {
		if err := fsutil.EnsureWritable(dir); err != nil {
			log.Fatalf("Media path not writable: %v", err)
		}
	}

	client, err := downloader.New(cfg.Downloader.Type, downloader.Config{
		URL:      cfg.Downloader.URL,
		Username: cfg.Downloader.Username,
		Password: cfg.Downloader.Password,
		Category: cfg.Downloader.Category,
	})
	if err != nil {
		log.Fatalf("Failed to initialize downloader: %v", err)
	}

	tmdb := metadata.New(cfg.Metadata.TMDB.APIKey)
	q := queue.New(client, mover, tmdb, cfg.Downloader.PollInterval.Std())
	trackers := make([]server.Tracker, 0, len(cfg.Trackers))
	for _, t := range cfg.Trackers {
		trackers = append(trackers, server.Tracker{Name: t.Name, URL: t.URL})
	}
	srv := server.New(q, tmdb, webui.FS(), trackers)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Open(cfg.General.ListenAddr); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	log.Printf("downloadarr listening on %s", cfg.General.ListenAddr)

	select {
	case err := <-errCh:
		if err != nil {
			log.Printf("Server error: %v", err)
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Stop(shutdownCtx); err != nil {
		log.Printf("Failed to stop server: %v", err)
	}
	// Stop the watch loops; in-flight torrents keep downloading in the client.
	q.Close()
	log.Println("Stopped")
}
