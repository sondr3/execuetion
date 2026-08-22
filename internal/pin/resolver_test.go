package pin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tagServer serves the two endpoints Resolve uses: commits/{ref} from shas,
// and matching-refs/tags/{prefix} from tags.
func tagServer(t *testing.T, shas map[string]string, tags []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/commits/"):
			ref := r.URL.Path[strings.LastIndex(r.URL.Path, "/commits/")+len("/commits/"):]
			sha, ok := shas[ref]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message": "No commit found"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"sha": sha})
		case strings.Contains(r.URL.Path, "/git/matching-refs/tags/"):
			prefix := r.URL.Path[strings.LastIndex(r.URL.Path, "/tags/")+len("/tags/"):]
			var refs []map[string]string
			for _, tag := range tags {
				if strings.HasPrefix(tag, prefix) {
					refs = append(refs, map[string]string{"ref": "refs/tags/" + tag})
				}
			}
			json.NewEncoder(w).Encode(refs)
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestGitHubResolveShortTagDiscoversFullVersion(t *testing.T) {
	srv := tagServer(t,
		map[string]string{"v7": checkoutSHA, "v7.0.10": checkoutSHA, "v7.0.9": setupGoSHA},
		[]string{"v7.0.0", "v7.0.9", "v7.0.10", "v7.0.10-rc1"})
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	res, err := g.Resolve(context.Background(), "actions", "checkout", "v7")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	// v7.0.10 beats v7.0.9 numerically; the -rc1 prerelease is filtered out.
	if res.SHA != checkoutSHA || res.Version != "v7.0.10" {
		t.Errorf("Resolve = %+v, want SHA %s version v7.0.10", res, checkoutSHA)
	}
}

func TestGitHubResolveFullVersionMismatchedCommit(t *testing.T) {
	// The deepest tag under v7 points at a different commit than v7 itself
	// (tag not yet moved); the comment must fall back to the bare ref.
	srv := tagServer(t,
		map[string]string{"v7": checkoutSHA, "v7.0.1": setupGoSHA},
		[]string{"v7.0.1"})
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	res, err := g.Resolve(context.Background(), "actions", "checkout", "v7")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.SHA != checkoutSHA || res.Version != "" {
		t.Errorf("Resolve = %+v, want SHA %s and empty version", res, checkoutSHA)
	}
}

func TestGitHubResolveFullSemverRef(t *testing.T) {
	srv := tagServer(t, map[string]string{"v7.1.2": checkoutSHA}, nil)
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	res, err := g.Resolve(context.Background(), "actions", "checkout", "v7.1.2")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.SHA != checkoutSHA || res.Version != "v7.1.2" {
		t.Errorf("Resolve = %+v, want version v7.1.2 without discovery", res)
	}
}

func TestGitHubResolveBranchRef(t *testing.T) {
	srv := tagServer(t, map[string]string{"main": checkoutSHA}, nil)
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	res, err := g.Resolve(context.Background(), "actions", "checkout", "main")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.SHA != checkoutSHA || res.Version != "" {
		t.Errorf("Resolve = %+v, want empty version for branch ref", res)
	}
}

func TestGitHubResolveHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		if strings.Contains(r.URL.Path, "/git/matching-refs/") {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`{"sha": "` + checkoutSHA + `"}`))
	}))
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Token: "secret", Client: srv.Client()}
	res, err := g.Resolve(context.Background(), "actions", "checkout", "v7")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.SHA != checkoutSHA {
		t.Errorf("Resolve = %+v, want %q", res, checkoutSHA)
	}
}

func TestGitHubResolveNotFound(t *testing.T) {
	srv := tagServer(t, map[string]string{}, nil)
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	_, err := g.Resolve(context.Background(), "actions", "checkout", "v99")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "No commit found") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error missing status or message: %v", err)
	}
}

func TestGitHubResolveRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message": "API rate limit exceeded"}`))
	}))
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	_, err := g.Resolve(context.Background(), "actions", "checkout", "v7")
	if err == nil {
		t.Fatal("expected error for rate limit")
	}
	if !strings.Contains(err.Error(), "rate limit") ||
		!strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("error should mention rate limit and GITHUB_TOKEN: %v", err)
	}
}

func TestGitHubResolveInvalidSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"sha": "not-a-sha"}`))
	}))
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := g.Resolve(context.Background(), "actions", "checkout", "v7"); err == nil {
		t.Fatal("expected error for invalid SHA in response")
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign
	}{
		{"v7.0.10", "v7.0.9", 1},
		{"v7.0.9", "v7.0.10", -1},
		{"v7.1.0", "v7.0.99", 1},
		{"v7.0.0", "v7.0.0", 0},
		{"v7.0.0", "v7.0", 0},
	}
	for _, tt := range tests {
		got := compareVersions(tt.a, tt.b)
		switch {
		case tt.want > 0 && got <= 0, tt.want < 0 && got >= 0, tt.want == 0 && got != 0:
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// fakeGh puts a stub gh executable on PATH and clears the token env vars.
func fakeGh(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake gh stub is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "gh"),
		[]byte("#!/bin/sh\n"+script),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
}

func TestNewGitHubTokenFromGhCLI(t *testing.T) {
	fakeGh(t, `echo fake-gh-token`)
	if g := NewGitHub(); g.Token != "fake-gh-token" {
		t.Errorf("Token = %q, want fake-gh-token", g.Token)
	}
}

func TestNewGitHubEnvTokenWins(t *testing.T) {
	fakeGh(t, `echo fake-gh-token`)
	t.Setenv("GITHUB_TOKEN", "env-token")
	if g := NewGitHub(); g.Token != "env-token" {
		t.Errorf("Token = %q, want env-token", g.Token)
	}
}

func TestNewGitHubGhNotLoggedIn(t *testing.T) {
	fakeGh(t, `exit 1`)
	if g := NewGitHub(); g.Token != "" {
		t.Errorf("Token = %q, want anonymous", g.Token)
	}
}

func TestNewGitHubNoGh(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	if g := NewGitHub(); g.Token != "" {
		t.Errorf("Token = %q, want anonymous", g.Token)
	}
}

func TestNewGitHubAPIURL(t *testing.T) {
	t.Setenv("GITHUB_API_URL", "https://ghe.example.com/api/v3/")
	t.Setenv("GITHUB_TOKEN", "x")
	if g := NewGitHub(); g.BaseURL != "https://ghe.example.com/api/v3" {
		t.Errorf("BaseURL = %q, want GHES URL without trailing slash", g.BaseURL)
	}
}
