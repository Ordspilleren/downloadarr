package mediamover

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMoveMovieKeepsExtensionWithDottedTitle guards against a title containing a
// period ("Coyote vs. Acme") being mistaken for a file that already carries an
// extension, which left the imported movie without its .mkv suffix.
func TestMoveMovieKeepsExtensionWithDottedTitle(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "Coyote.vs.Acme.2026.2160p.mkv")
	if err := os.WriteFile(src, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := New(Paths{
		MoviesDir:      t.TempDir(),
		MoviesTemplate: `{{.MovieName}}{{if .Year}} ({{.Year}}){{end}}/{{.MovieName}}{{if .Year}} ({{.Year}}){{end}}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	dest, err := m.MoveMovie(srcDir, MovieInfo{MovieName: "Coyote vs. Acme", Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Ext(dest); got != ".mkv" {
		t.Fatalf("destination %q: extension = %q, want %q", dest, got, ".mkv")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("destination was not created: %v", err)
	}
}

// TestMoveMovieRespectsTemplateExtension checks that a template which already
// ends in a video extension is not given a second one.
func TestMoveMovieRespectsTemplateExtension(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "Some.Movie.2020.mkv")
	if err := os.WriteFile(src, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := New(Paths{
		MoviesDir:      t.TempDir(),
		MoviesTemplate: `{{.MovieName}} ({{.Year}})/{{.MovieName}} ({{.Year}}).mkv`,
	})
	if err != nil {
		t.Fatal(err)
	}

	dest, err := m.MoveMovie(srcDir, MovieInfo{MovieName: "Some Movie", Year: 2020})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(dest) != ".mkv" {
		t.Fatalf("destination %q: extension = %q, want %q", dest, filepath.Ext(dest), ".mkv")
	}
	if filepath.Base(dest) != "Some Movie (2020).mkv" {
		t.Fatalf("destination base = %q, want %q", filepath.Base(dest), "Some Movie (2020).mkv")
	}
}
