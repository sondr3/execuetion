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
	c.Put("actions/checkout@v7", Resolution{SHA: checkoutSHA, Version: "v7.0.2"})
	if err := c.Save(); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	reopened := Open(path)
	res, ok := reopened.Get("actions/checkout@v7")
	if !ok || res.SHA != checkoutSHA || res.Version != "v7.0.2" {
		t.Errorf(
			"reopened cache Get = %+v, %v; want SHA %q and version v7.0.2",
			res,
			ok,
			checkoutSHA,
		)
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
	c.Put("actions/checkout@v7", Resolution{SHA: checkoutSHA, Version: "v7.0.2"})
	if err := c.Save(); err != nil {
		t.Fatalf("Save over corrupt file returned error: %v", err)
	}
	if res, ok := Open(path).Get("actions/checkout@v7"); !ok || res.SHA != checkoutSHA {
		t.Errorf("cache not recovered after corruption, got %+v, %v", res, ok)
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
	c.Put("actions/checkout@v7", Resolution{SHA: checkoutSHA, Version: "v7.0.2"})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"actions/checkout@v7"`, `"sha"`, checkoutSHA, `"version"`, `"v7.0.2"`, `"resolved_at"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("cache file missing %q:\n%s", want, data)
		}
	}
}

func TestCacheAll(t *testing.T) {
	c := Open(filepath.Join(t.TempDir(), "pins.json"))
	c.Put("b/b@v2", Resolution{SHA: setupGoSHA})
	c.Put("a/a@v1", Resolution{SHA: checkoutSHA, Version: "v1.0.0"})
	all := c.All()
	if len(all) != 2 || all[0].Key != "a/a@v1" || all[1].Key != "b/b@v2" {
		t.Fatalf("All not sorted by key: %+v", all)
	}
	if all[0].SHA != checkoutSHA || all[0].Version != "v1.0.0" || all[0].ResolvedAt.IsZero() {
		t.Errorf("All[0] = %+v", all[0])
	}
}
