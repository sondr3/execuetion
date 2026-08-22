package pin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// A Resolver resolves owner/repo@ref to a full commit SHA.
type Resolver interface {
	Resolve(ctx context.Context, owner, repo, ref string) (string, error)
}

// GitHub resolves references through the GitHub REST API. The commits
// endpoint dereferences branches, lightweight tags, and annotated tags to
// the commit SHA in a single call.
type GitHub struct {
	BaseURL string // default https://api.github.com
	Token   string // optional bearer token
	Client  *http.Client
}

// NewGitHub returns a resolver against api.github.com, authenticating with
// GITHUB_TOKEN (or GH_TOKEN) when set. Anonymous requests work but are
// rate-limited to 60/hour.
func NewGitHub() *GitHub {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return &GitHub{
		BaseURL: "https://api.github.com",
		Token:   token,
		Client:  http.DefaultClient,
	}
}

// Resolve fetches the commit SHA that ref points at in owner/repo.
func (g *GitHub) Resolve(ctx context.Context, owner, repo, ref string) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/commits/%s",
		g.BaseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	resp, err := g.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiErr)
		msg := strings.TrimSpace(apiErr.Message)
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
			resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return "", fmt.Errorf("GitHub API rate limit exceeded (set GITHUB_TOKEN to raise it): %s", msg)
		}
		return "", fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, msg)
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(body, &commit); err != nil {
		return "", fmt.Errorf("decoding GitHub API response: %w", err)
	}
	if !shaRE.MatchString(commit.SHA) {
		return "", fmt.Errorf("GitHub API returned invalid commit SHA %q", commit.SHA)
	}
	return commit.SHA, nil
}
