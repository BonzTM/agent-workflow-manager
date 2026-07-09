package sqlite

import (
	"errors"
	"path/filepath"
	"strings"
)

// Config holds the settings needed to open a SQLite-backed repository.
type Config struct {
	Path string
}

// Validate reports an error when the config cannot open a database, i.e.
// when Path is empty or whitespace.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Path) == "" {
		return errors.New("sqlite path is required")
	}
	return nil
}

// NormalizedPath returns the cleaned filesystem path to the database file,
// or an empty string when Path is unset.
func (c Config) NormalizedPath() string {
	trimmed := strings.TrimSpace(c.Path)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}
