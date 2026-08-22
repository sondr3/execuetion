package pin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// An entry is one cached resolution. ResolvedAt is informational only;
// entries never expire — --update-pins is the refresh mechanism.
type entry struct {
	SHA        string    `json:"sha"`
	Version    string    `json:"version,omitempty"`
	ResolvedAt time.Time `json:"resolved_at"`
}

// A Cache is the persistent owner/repo@ref -> SHA store shared by every
// repo the binary runs against.
type Cache struct {
	path    string
	entries map[string]entry
	dirty   bool
}

// DefaultPath returns the pin cache location: $EXECUETION_CACHE_DIR/pins.json
// if set, else $XDG_CACHE_HOME/execuetion/pins.json, else
// ~/.cache/execuetion/pins.json. Deliberately not os.UserCacheDir, which
// would scatter the cache into ~/Library/Caches on darwin.
func DefaultPath() (string, error) {
	if dir := os.Getenv("EXECUETION_CACHE_DIR"); dir != "" {
		return filepath.Join(dir, "pins.json"), nil
	}
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "execuetion", "pins.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "execuetion", "pins.json"), nil
}

// Open loads the cache at path. A missing file is an empty cache; a corrupt
// file is warned about and treated as empty rather than failing the run,
// since the worst case is a re-resolve.
func Open(path string) *Cache {
	c := &Cache{path: path, entries: make(map[string]entry), dirty: false}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		fmt.Fprintf(os.Stderr, "warning: ignoring corrupt pin cache %s: %v\n", path, err)
		c.entries = make(map[string]entry)
	}
	return c
}

// Get returns the cached resolution for key, if any.
func (c *Cache) Get(key string) (Resolution, bool) {
	e, ok := c.entries[key]
	return Resolution{SHA: e.SHA, Version: e.Version}, ok
}

// Put records a resolution and marks the cache dirty.
func (c *Cache) Put(key string, res Resolution) {
	c.entries[key] = entry{SHA: res.SHA, Version: res.Version, ResolvedAt: time.Now().UTC()}
	c.dirty = true
}

// An Entry is one cached resolution with its key, as exposed by All.
type Entry struct {
	Key        string
	SHA        string
	Version    string
	ResolvedAt time.Time
}

// All returns every cached resolution, sorted by key (--dump).
func (c *Cache) All() []Entry {
	entries := make([]Entry, 0, len(c.entries))
	for key, e := range c.entries {
		entries = append(
			entries,
			Entry{Key: key, SHA: e.SHA, Version: e.Version, ResolvedAt: e.ResolvedAt},
		)
	}
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Key, b.Key) })
	return entries
}

// Save writes the cache back atomically (temp file + rename in the same
// directory). It is a no-op unless a Put happened since Open.
func (c *Cache) Save() error {
	if !c.dirty {
		return nil
	}
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(c.path)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("saving pin cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "pins-*.json")
	if err != nil {
		return fmt.Errorf("saving pin cache: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("saving pin cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("saving pin cache: %w", err)
	}
	if err := os.Rename(tmp.Name(), c.path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("saving pin cache: %w", err)
	}
	c.dirty = false
	return nil
}
