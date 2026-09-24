// Package config loads the YAML configuration file and applies defaults.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Ordspilleren/downloadarr-lite/mediamover"
)

// Config is the root configuration.
type Config struct {
	General    GeneralConfig    `yaml:"general"`
	Downloader DownloaderConfig `yaml:"downloader"`
	MediaPaths MediaPathsConfig `yaml:"media_paths"`
	Metadata   MetadataConfig   `yaml:"metadata"`
	Trackers   []TrackerConfig  `yaml:"trackers"`
}

// TrackerConfig is one tracker search endpoint shown during keyword search.
// URL must contain "{query}".
type TrackerConfig struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// MetadataConfig holds the optional metadata-provider settings.
type MetadataConfig struct {
	TMDB TMDBConfig `yaml:"tmdb"`
}

// TMDBConfig holds the TMDB credentials. An empty API key disables TMDB, and
// the import then falls back to the release name alone.
type TMDBConfig struct {
	APIKey string `yaml:"api_key"`
}

// GeneralConfig holds the HTTP settings.
type GeneralConfig struct {
	ListenAddr string `yaml:"listen_addr"`
}

// DownloaderConfig holds the torrent-client settings.
type DownloaderConfig struct {
	Type         string   `yaml:"type"`
	URL          string   `yaml:"url"`
	Username     string   `yaml:"username"`
	Password     string   `yaml:"password"`
	Category     string   `yaml:"category"`
	PollInterval Duration `yaml:"poll_interval"`
}

// MediaPathsConfig describes where movies and TV episodes are moved.
type MediaPathsConfig struct {
	Movies MediaPathConfig `yaml:"movies"`
	TV     MediaPathConfig `yaml:"tv"`
}

// MediaPathConfig is a library root plus the path template beneath it.
type MediaPathConfig struct {
	Path     string `yaml:"path"`
	Template string `yaml:"template"`
}

// Duration is a time.Duration that unmarshals from a Go duration string such as
// "2s" or "1m".
type Duration time.Duration

// UnmarshalYAML parses a duration string.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Std returns the value as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// ToMoverPaths converts the media-path settings into the value the mover
// consumes, keeping the mover itself free of any config dependency.
func (m MediaPathsConfig) ToMoverPaths() mediamover.Paths {
	return mediamover.Paths{
		TVDir:          m.TV.Path,
		MoviesDir:      m.Movies.Path,
		TVTemplate:     m.TV.Template,
		MoviesTemplate: m.Movies.Template,
	}
}

// Load reads the config file at path, applies defaults, and validates the
// result. It has no filesystem side effects beyond reading the file.
func Load(path string) (*Config, error) {
	cfg := &Config{
		General:    GeneralConfig{ListenAddr: ":8080"},
		Downloader: DownloaderConfig{Type: "qbittorrent", PollInterval: Duration(2 * time.Second)},
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	return cfg, nil
}

// Validate checks the configuration for the mistakes that would otherwise
// surface as confusing runtime errors.
func (c *Config) Validate() error {
	if c.General.ListenAddr == "" {
		return fmt.Errorf("general.listen_addr is required")
	}
	switch c.Downloader.Type {
	case "qbittorrent":
		if c.Downloader.URL == "" {
			return fmt.Errorf("downloader.url is required for qbittorrent")
		}
	case "fake":
	default:
		return fmt.Errorf("downloader.type must be \"qbittorrent\" or \"fake\"")
	}
	if c.Downloader.PollInterval.Std() <= 0 {
		return fmt.Errorf("downloader.poll_interval must be greater than 0")
	}
	for _, media := range []struct {
		name string
		cfg  MediaPathConfig
	}{
		{"movies", c.MediaPaths.Movies},
		{"tv", c.MediaPaths.TV},
	} {
		if media.cfg.Path == "" {
			return fmt.Errorf("media_paths.%s.path is required", media.name)
		}
		if media.cfg.Template == "" {
			return fmt.Errorf("media_paths.%s.template is required", media.name)
		}
	}
	for i, tracker := range c.Trackers {
		if tracker.Name == "" {
			return fmt.Errorf("trackers[%d].name is required", i)
		}
		if tracker.URL == "" {
			return fmt.Errorf("trackers[%d].url is required", i)
		}
		if !strings.Contains(tracker.URL, "{query}") {
			return fmt.Errorf("trackers[%d].url must contain \"{query}\"", i)
		}
	}
	return nil
}
