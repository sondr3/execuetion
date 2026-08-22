package pin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "pins.json")
	c := Open(path)
	if _, ok := c.Get("actions/checkout@v7"); ok {
		t.Fatal("empty cache returned a hit")
	}
	c.Put("actions/checkout@v7", checkoutSHA)
	if err := c.Save(); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	reopened := Open(path)
	sha, ok := reopened.Get("actions/checkout@v7")
	if !ok || sha != checkoutSHA {
		t.Errorf("reopened cache Get = %q, %v; want %q, true", sha, ok, checkoutSHA)
	}
}

func TestCacheSaveNoopWhenClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.json")
	c := Open(path)
	if err := c.Save(); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Save wrote a file without any Put")
	}
}

func TestCacheCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Open(path)
	if _, ok := c.Get("anything"); ok {
		t.Error("corrupt cache returned a hit")
	}
	c.Put("actions/checkout@v7", checkoutSHA)
	if err := c.Save(); err != nil {
		t.Fatalf("Save over corrupt file returned error: %v", err)
	}
	if sha, ok := Open(path).Get("actions/checkout@v7"); !ok || sha != checkoutSHA {
		t.Errorf("cache not recovered after corruption, got %q, %v", sha, ok)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("EXECUETION_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/user")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath returned error: %v", err)
	}
	// Never os.UserCacheDir's ~/Library/Caches, even on darwin.
	want := filepath.Join("/home/user", ".cache", "execuetion", "pins.json")
	if path != want {
		t.Errorf("DefaultPath = %q, want %q", path, want)
	}

	t.Setenv("XDG_CACHE_HOME", "/xdg")
	if path, _ := DefaultPath(); path != filepath.Join("/xdg", "execuetion", "pins.json") {
		t.Errorf("XDG_CACHE_HOME not honored, got %q", path)
	}

	t.Setenv("EXECUETION_CACHE_DIR", "/override")
	if path, _ := DefaultPath(); path != filepath.Join("/override", "pins.json") {
		t.Errorf("EXECUETION_CACHE_DIR not honored, got %q", path)
	}
}

func TestCacheFileFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.json")
	c := Open(path)
	c.Put("actions/checkout@v7", checkoutSHA)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"actions/checkout@v7"`, `"sha"`, checkoutSHA, `"resolved_at"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("cache file missing %q:\n%s", want, data)
		}
	}
}
