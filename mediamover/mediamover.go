// Package mediamover hard-links finished downloads into a media library,
// leaving the torrent in place so it can keep seeding. Destinations are rendered
// from small Go templates so the library layout is configurable.
package mediamover

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/template"
)

// Paths carries the library locations and path templates the mover renders
// destinations from.
type Paths struct {
	TVDir          string
	MoviesDir      string
	TVTemplate     string
	MoviesTemplate string
}

// MediaMover moves downloaded files into their media locations.
type MediaMover struct {
	paths      Paths
	tvTmpl     *template.Template
	moviesTmpl *template.Template
}

// MovieInfo describes a movie. Field names match the movie path template
// ({{.MovieName}}, {{.Year}}).
type MovieInfo struct {
	MovieName string
	Year      int
}

// TVEpisodeInfo describes a single TV episode. Field names match the TV path
// template ({{.ShowName}}, {{.SeasonNumber}}, …).
type TVEpisodeInfo struct {
	ShowName      string
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  string
}

// SeasonPackInfo describes a downloaded season pack. Each episode file is
// matched against the TV template on its own, so every episode lands at its
// canonical per-episode destination.
type SeasonPackInfo struct {
	ShowName     string
	SeasonNumber int
	Episodes     []SeasonPackEpisode
}

// SeasonPackEpisode is one episode of a season pack. Titles are optional: a
// release name that carries no episode titles still imports, just without the
// {{.EpisodeTitle}} field filled in.
type SeasonPackEpisode struct {
	EpisodeNumber int
	Title         string
}

// New creates a MediaMover, parsing both path templates so syntax errors
// surface at startup instead of at the first move.
func New(paths Paths) (*MediaMover, error) {
	tvTmpl, err := parsePathTemplate(paths.TVTemplate)
	if err != nil {
		return nil, fmt.Errorf("invalid TV template: %w", err)
	}
	moviesTmpl, err := parsePathTemplate(paths.MoviesTemplate)
	if err != nil {
		return nil, fmt.Errorf("invalid movie template: %w", err)
	}
	return &MediaMover{paths: paths, tvTmpl: tvTmpl, moviesTmpl: moviesTmpl}, nil
}

// MoveMovie hard-links the largest video file of the download into the movie
// library (samples and extras are skipped) and returns the destination path.
func (m *MediaMover) MoveMovie(sourcePath string, info MovieInfo) (string, error) {
	return m.moveLargestVideo(sourcePath, m.moviesTmpl, m.paths.MoviesDir, info)
}

// MoveEpisode hard-links the largest video file of a single-episode download
// into the TV library and returns the destination path.
func (m *MediaMover) MoveEpisode(sourcePath string, info TVEpisodeInfo) (string, error) {
	return m.moveLargestVideo(sourcePath, m.tvTmpl, m.paths.TVDir, info)
}

// moveLargestVideo hard-links the largest video file under sourcePath to the
// destination rendered from tmpl under basePath.
func (m *MediaMover) moveLargestVideo(sourcePath string, tmpl *template.Template, basePath string, info any) (string, error) {
	if err := checkSource(sourcePath); err != nil {
		return "", err
	}
	src, err := largestVideoFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to locate video file: %w", err)
	}
	dest, err := m.destination(tmpl, basePath, info)
	if err != nil {
		return "", err
	}
	return m.linkWithExtension(src, dest)
}

// MoveSeasonPack hard-links every video file of a season pack into the TV
// library, matching each file against the template on its own. Files without an
// episode number (samples, extras) or from another season are skipped. It
// returns the season directory.
func (m *MediaMover) MoveSeasonPack(sourcePath string, info SeasonPackInfo) (string, error) {
	if err := checkSource(sourcePath); err != nil {
		return "", err
	}

	titles := make(map[int]string, len(info.Episodes))
	for _, ep := range info.Episodes {
		titles[ep.EpisodeNumber] = ep.Title
	}

	files, err := videoFiles(sourcePath)
	if err != nil {
		return "", err
	}

	destDir := ""
	moved := 0
	for _, src := range files {
		season, episode, ok := parseEpisodeNumber(src)
		if !ok || season != info.SeasonNumber {
			continue
		}
		dest, err := m.destination(m.tvTmpl, m.paths.TVDir, TVEpisodeInfo{
			ShowName:      info.ShowName,
			SeasonNumber:  info.SeasonNumber,
			EpisodeNumber: episode,
			EpisodeTitle:  titles[episode],
		})
		if err != nil {
			return "", err
		}
		finalDest, err := m.linkWithExtension(src, dest)
		if err != nil {
			return "", fmt.Errorf("failed to link %s: %w", src, err)
		}
		if moved == 0 {
			destDir = filepath.Dir(finalDest)
		}
		moved++
	}
	if moved == 0 {
		return "", fmt.Errorf("no episode files found in source directory: %s", sourcePath)
	}
	return destDir, nil
}

// HasMultipleVideoFiles reports whether the content at path holds more than one
// video file. A single file reports false even when its extension is not a
// video extension: that file is the whole content.
func HasMultipleVideoFiles(path string) (bool, error) {
	files, err := videoFiles(path)
	if err != nil {
		return false, err
	}
	return len(files) > 1, nil
}

// destination renders tmpl for info under basePath.
func (m *MediaMover) destination(tmpl *template.Template, basePath string, info any) (string, error) {
	var result strings.Builder
	if err := tmpl.Execute(&result, info); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	return filepath.Join(basePath, result.String()), nil
}

// linkWithExtension hard-links src to dest, appending src's extension when the
// rendered template did not already include one. It returns the destination the
// file was actually linked to.
//
// Whether the template included an extension is judged against the known video
// extensions rather than against the mere presence of a dot: a movie title such
// as "Coyote vs. Acme" ends in a dot-separated word, and treating that as an
// extension would leave the file without its .mkv suffix.
func (m *MediaMover) linkWithExtension(src, dest string) (string, error) {
	if !videoExtensions[strings.ToLower(filepath.Ext(dest))] {
		dest += filepath.Ext(src)
	}
	return dest, linkFile(src, dest)
}

// parsePathTemplate builds the template with the functions path templates may
// use, so validation and rendering agree on the available function set.
func parsePathTemplate(s string) (*template.Template, error) {
	return template.New("path").Funcs(template.FuncMap{
		"padZeros": func(n int) string { return fmt.Sprintf("%02d", n) },
		"sanitize": sanitize,
	}).Parse(s)
}

// videoExtensions lists the extensions treated as media video files.
var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
	".mpg": true, ".mpeg": true, ".ts": true, ".m2ts": true,
	".mts": true, ".vob": true, ".ogv": true, ".3gp": true,
	".rmvb": true, ".m2v": true,
}

// videoFiles returns every video file under path, or the file itself when path
// is a single file (single-file torrents keep their content).
func videoFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if videoExtensions[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan source directory: %w", err)
	}
	return files, nil
}

// largestVideoFile returns the largest video file under sourcePath. If
// sourcePath is itself a file it is returned as-is.
func largestVideoFile(sourcePath string) (string, error) {
	files, err := videoFiles(sourcePath)
	if err != nil {
		return "", err
	}
	var largest string
	var largestSize int64 = -1
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.Size() > largestSize {
			largest, largestSize = path, info.Size()
		}
	}
	if largest == "" {
		return "", fmt.Errorf("no video files found in source directory: %s", sourcePath)
	}
	return largest, nil
}

// episodeNumberPattern recognises the S01E03 and 1x03 forms as a standalone
// token (guarded by non-digits so "1080p" cannot match).
var episodeNumberPattern = regexp.MustCompile(`(?:^|[^0-9])(?:s(\d{1,2})e(\d{1,2})|(\d{1,2})x(\d{1,2}))(?:[^0-9]|$)`)

// parseEpisodeNumber extracts the season and episode number from a file name.
func parseEpisodeNumber(path string) (season, episode int, ok bool) {
	m := episodeNumberPattern.FindStringSubmatch(strings.ToLower(filepath.Base(path)))
	if m == nil {
		return 0, 0, false
	}
	if m[1] != "" {
		return atoi(m[1]), atoi(m[2]), true
	}
	return atoi(m[3]), atoi(m[4]), true
}

// checkSource verifies the downloaded content exists.
func checkSource(sourcePath string) error {
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("source does not exist: %s", sourcePath)
	}
	return nil
}

// linkFile hard-links sourcePath to destPath, leaving the source in place so the
// torrent can keep seeding. Across filesystems (where hard links are
// impossible) it falls back to copying.
func linkFile(sourcePath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	// Replace any existing destination so re-processing works.
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing destination: %w", err)
	}
	if err := os.Link(sourcePath, destPath); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("failed to hard link %s: %w", sourcePath, err)
	}
	log.Printf("Warning: cannot hard link across filesystems, copying %s instead", sourcePath)
	return copyFile(sourcePath, destPath)
}

// copyFile copies a single file, used when hard linking across filesystems.
func copyFile(sourcePath, destPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}
	return os.Chmod(destPath, info.Mode())
}

// sanitize replaces characters that are invalid in a path component.
func sanitize(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(name)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
