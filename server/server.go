// Package server exposes the JSON API and serves the embedded web interface.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/Ordspilleren/downloadarr/metadata"
	"github.com/Ordspilleren/downloadarr/queue"
)

// Server owns the HTTP lifecycle.
type Server struct {
	echo     *echo.Echo
	q        *queue.Queue
	meta     *metadata.Client
	trackers []Tracker

	// ctx is cancelled by Stop. Long-lived handlers (the SSE stream) select on
	// it so they return promptly instead of making Shutdown wait out its
	// timeout on connections that only end when the client disconnects.
	ctx    context.Context
	cancel context.CancelFunc
}

// Tracker is a tracker search endpoint exposed to the web interface.
type Tracker struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// New builds the server: /api/downloads backs the web interface, and every
// other path is served from webFS (the embedded SPA).
func New(q *queue.Queue, meta *metadata.Client, webFS fs.FS, trackers []Tracker) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.RequestLogger())
	// Cap request bodies so a huge upload cannot exhaust memory. Torrent files
	// are tiny; 10 MiB leaves plenty of headroom.
	e.Use(middleware.BodyLimit("10M"))

	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{echo: e, q: q, meta: meta, trackers: trackers, ctx: ctx, cancel: cancel}
	api := e.Group("/api")
	api.GET("/downloads", s.listDownloads)
	api.POST("/downloads", s.addDownload)
	api.DELETE("/downloads/:hash", s.removeDownload)
	api.PATCH("/downloads/:hash/match", s.setMatch)
	api.GET("/search", s.search)
	api.GET("/status", s.status)
	api.GET("/trackers", s.listTrackers)
	api.GET("/events", s.streamEvents)
	e.GET("/*", spaHandler(webFS))

	return s
}

// Open starts the HTTP server and blocks until it stops.
func (s *Server) Open(addr string) error {
	return s.echo.Start(addr)
}

// Stop shuts the server down gracefully. It first signals long-lived handlers
// to return, then waits for the remaining connections to drain.
func (s *Server) Stop(ctx context.Context) error {
	s.cancel()
	return s.echo.Shutdown(ctx)
}

func (s *Server) listDownloads(c echo.Context) error {
	return c.JSON(http.StatusOK, s.q.List())
}

type addRequest struct {
	Magnet string `json:"magnet"`
}

// addDownload accepts either a JSON body with a magnet link or a
// multipart/form-data body with a .torrent file.
func (s *Server) addDownload(c echo.Context) error {
	if strings.HasPrefix(c.Request().Header.Get(echo.HeaderContentType), "multipart/form-data") {
		return s.addTorrentFile(c)
	}

	var req addRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	req.Magnet = strings.TrimSpace(req.Magnet)
	if req.Magnet == "" {
		return fail(c, http.StatusBadRequest, "magnet link is required")
	}

	d, err := s.q.Add(req.Magnet)
	switch {
	case errors.Is(err, queue.ErrDuplicate):
		return fail(c, http.StatusConflict, "this magnet is already being downloaded")
	case err != nil:
		return fail(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusCreated, d)
}

// addTorrentFile handles the multipart branch of addDownload: it reads the
// uploaded .torrent and registers it by its info hash.
func (s *Server) addTorrentFile(c echo.Context) error {
	header, err := c.FormFile("file")
	if err != nil {
		return fail(c, http.StatusBadRequest, "a .torrent file is required")
	}
	src, err := header.Open()
	if err != nil {
		return fail(c, http.StatusBadRequest, "could not read the uploaded file")
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		return fail(c, http.StatusBadRequest, "could not read the uploaded file")
	}

	d, err := s.q.AddFile(data, header.Filename)
	switch {
	case errors.Is(err, queue.ErrDuplicate):
		return fail(c, http.StatusConflict, "this torrent is already being downloaded")
	case err != nil:
		return fail(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusCreated, d)
}

// removeDownload covers the two ways to take a download off the list. By
// default it clears the entry, leaving the torrent untouched in the client.
// With ?deleteFromClient=true it instead removes the torrent from the client;
// the optional deleteFiles query parameter also deletes the downloaded data.
func (s *Server) removeDownload(c echo.Context) error {
	hash := c.Param("hash")

	if deleteFromClient, _ := strconv.ParseBool(c.QueryParam("deleteFromClient")); deleteFromClient {
		deleteFiles, _ := strconv.ParseBool(c.QueryParam("deleteFiles"))
		switch err := s.q.Delete(hash, deleteFiles); {
		case errors.Is(err, queue.ErrNotFound):
			return fail(c, http.StatusNotFound, "download not found")
		case errors.Is(err, queue.ErrImporting):
			return fail(c, http.StatusConflict, "download is being imported")
		case err != nil:
			return fail(c, http.StatusBadGateway, "could not remove the download from the client")
		}
		return c.NoContent(http.StatusNoContent)
	}

	switch err := s.q.Clear(hash); {
	case errors.Is(err, queue.ErrNotFound):
		return fail(c, http.StatusNotFound, "download not found")
	case errors.Is(err, queue.ErrImporting):
		return fail(c, http.StatusConflict, "download is being imported")
	}
	return c.NoContent(http.StatusNoContent)
}

// setMatch records the metadata correction the user made for a download.
func (s *Server) setMatch(c echo.Context) error {
	var in queue.MatchInput
	if err := c.Bind(&in); err != nil {
		return fail(c, http.StatusBadRequest, "invalid request body")
	}
	d, err := s.q.SetMatch(c.Param("hash"), in)
	switch {
	case errors.Is(err, queue.ErrNotFound):
		return fail(c, http.StatusNotFound, "download not found")
	case err != nil:
		return fail(c, http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, d)
}

// search proxies a TMDB lookup so the user can find a better match than the
// automatic one.
func (s *Server) search(c echo.Context) error {
	query := strings.TrimSpace(c.QueryParam("query"))
	if query == "" {
		return fail(c, http.StatusBadRequest, "query is required")
	}
	if !s.meta.Enabled() {
		return fail(c, http.StatusServiceUnavailable, "TMDB is not configured")
	}
	mediaType := metadata.MediaType(c.QueryParam("type"))
	if mediaType != metadata.TV {
		mediaType = metadata.Movie
	}
	results, err := s.meta.Search(c.Request().Context(), mediaType, query)
	if err != nil {
		return fail(c, http.StatusBadGateway, err.Error())
	}
	return c.JSON(http.StatusOK, results)
}

// status tells the web interface which optional integrations are active.
func (s *Server) status(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]bool{"metadataEnabled": s.meta.Enabled()})
}

// listTrackers returns the configured tracker search endpoints. The slice is
// normalized to an empty one so the JSON is [] when none are configured.
func (s *Server) listTrackers(c echo.Context) error {
	trackers := s.trackers
	if trackers == nil {
		trackers = []Tracker{}
	}
	return c.JSON(http.StatusOK, trackers)
}

// streamEvents pushes the full download list over Server-Sent Events whenever
// it changes, giving the web interface live updates without polling. Each frame
// is a JSON array of downloads; a periodic comment keeps idle connections from
// being reaped by proxies.
func (s *Server) streamEvents(c echo.Context) error {
	res := c.Response()
	res.Header().Set(echo.HeaderContentType, "text/event-stream")
	res.Header().Set(echo.HeaderCacheControl, "no-cache")
	res.Header().Set("Connection", "keep-alive")
	// Disable response buffering in nginx and similar reverse proxies.
	res.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := res.Writer.(http.Flusher)
	if !ok {
		return fail(c, http.StatusInternalServerError, "streaming is not supported")
	}
	res.WriteHeader(http.StatusOK)
	flusher.Flush()

	updates, unsubscribe := s.q.Subscribe()
	defer unsubscribe()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-c.Request().Context().Done():
			return nil
		case <-heartbeat.C:
			if _, err := io.WriteString(res, ": keep-alive\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		case list, ok := <-updates:
			if !ok {
				return nil
			}
			if err := writeEvent(res, list); err != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}

// writeEvent writes one SSE frame. The payload is JSON, which marshals without
// newlines, so a single data line is enough.
func writeEvent(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// spaHandler serves webFS and falls back to index.html for unknown paths, so a
// single-page app with client-side routes still loads on a deep link.
func spaHandler(webFS fs.FS) echo.HandlerFunc {
	fileServer := http.FileServer(http.FS(webFS))
	return func(c echo.Context) error {
		p := strings.TrimPrefix(c.Request().URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(webFS, p); err != nil {
			// Unknown path: serve the SPA entry point, keeping the browser URL
			// (and thus any client-side route) intact.
			c.Request().URL.Path = "/"
		}
		fileServer.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

func fail(c echo.Context, code int, msg string) error {
	return c.JSON(code, map[string]string{"message": msg})
}
