package pin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/actions/checkout/commits/v7" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		w.Write([]byte(`{"sha": "` + checkoutSHA + `", "commit": {}}`))
	}))
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Token: "secret", Client: srv.Client()}
	sha, err := g.Resolve(context.Background(), "actions", "checkout", "v7")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if sha != checkoutSHA {
		t.Errorf("Resolve = %q, want %q", sha, checkoutSHA)
	}
}

func TestGitHubResolveNoTokenNoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("unexpected Authorization header %q", got)
		}
		w.Write([]byte(`{"sha": "` + checkoutSHA + `"}`))
	}))
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := g.Resolve(context.Background(), "actions", "checkout", "v7"); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
}

func TestGitHubResolveNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "No commit found for SHA: v99"}`))
	}))
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
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message": "API rate limit exceeded"}`))
	}))
	defer srv.Close()

	g := &GitHub{BaseURL: srv.URL, Client: srv.Client()}
	_, err := g.Resolve(context.Background(), "actions", "checkout", "v7")
	if err == nil {
		t.Fatal("expected error for rate limit")
	}
	if !strings.Contains(err.Error(), "rate limit") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
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
