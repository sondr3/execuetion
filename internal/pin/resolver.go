package pin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A Resolution is what a ref resolved to: the commit SHA, and when it could
// be discovered, the full release version behind a moving tag (v7 ->
// v7.0.2). Version is "" when there is nothing better than the ref itself
// (branches, prerelease-style tags).
type Resolution struct {
	SHA     string
	Version string
}

// A Resolver resolves owner/repo@ref to a commit SHA and full version.
type Resolver interface {
	Resolve(ctx context.Context, owner, repo, ref string) (Resolution, error)
}

// GitHub resolves references through the GitHub REST API. The commits
// endpoint dereferences branches, lightweight tags, and annotated tags to
// the commit SHA in a single call.
type GitHub struct {
	BaseURL string // default https://api.github.com
	Token   string // optional bearer token
	Client  *http.Client
}

// NewGitHub returns a resolver against api.github.com (or $GITHUB_API_URL,
// which Actions runners set — GitHub Enterprise included), authenticating
// with GITHUB_TOKEN or GH_TOKEN when set, falling back to the token the gh
// CLI is logged in with (`gh auth token`) when it is installed. Anonymous
// requests work but are rate-limited to 60/hour.
func NewGitHub() *GitHub {
	base := "https://api.github.com"
	if u := os.Getenv("GITHUB_API_URL"); u != "" {
		base = strings.TrimSuffix(u, "/")
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		token = ghAuthToken()
	}
	return &GitHub{
		BaseURL: base,
		Token:   token,
		Client:  http.DefaultClient,
	}
}

// ghAuthToken asks a locally installed gh CLI for its stored token. Any
// failure (gh not installed, not logged in, slow keychain) degrades to
// anonymous requests rather than failing the run.
func ghAuthToken() string {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, gh, "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var (
	// fullSemverRE matches refs that already are a concrete release.
	fullSemverRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)
	// shortTagRE matches moving release tags (v7, v7.1) worth expanding to
	// the concrete release behind them.
	shortTagRE = regexp.MustCompile(`^v?\d+(\.\d+)?$`)
)

// Resolve fetches the commit SHA that ref points at in owner/repo, and for
// short moving tags additionally discovers the full release version
// pointing at the same commit. Version discovery is best-effort: any
// failure there falls back to the bare SHA rather than erroring.
func (g *GitHub) Resolve(ctx context.Context, owner, repo, ref string) (Resolution, error) {
	sha, err := g.commitSHA(ctx, owner, repo, ref)
	if err != nil {
		return Resolution{}, err
	}
	version := ""
	switch {
	case fullSemverRE.MatchString(ref):
		version = ref
	case shortTagRE.MatchString(ref):
		version = g.fullVersion(ctx, owner, repo, ref, sha)
	}
	return Resolution{SHA: sha, Version: version}, nil
}

// get performs one authenticated API request and returns the response
// status, headers, and body (capped at 1MB). The body is fully read and
// closed here.
func (g *GitHub) get(ctx context.Context, u string) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, body, nil
}

// commitSHA resolves ref to a full commit SHA.
func (g *GitHub) commitSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/commits/%s",
		g.BaseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref))
	status, header, body, err := g.get(ctx, u)
	if err != nil {
		return "", err
	}

	if status != http.StatusOK {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiErr)
		msg := strings.TrimSpace(apiErr.Message)
		if msg == "" {
			msg = http.StatusText(status)
		}
		if (status == http.StatusForbidden || status == http.StatusTooManyRequests) &&
			header.Get("X-Ratelimit-Remaining") == "0" {
			return "", fmt.Errorf(
				"GitHub API rate limit exceeded (set GITHUB_TOKEN to raise it): %s",
				msg,
			)
		}
		return "", fmt.Errorf("GitHub API returned %d: %s", status, msg)
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

// fullVersion finds the deepest release tag under the moving tag ref
// (v7 -> v7.0.2) that points at the same commit. Returns "" when no such
// tag exists or on any API failure — the caller falls back to ref.
func (g *GitHub) fullVersion(ctx context.Context, owner, repo, ref, sha string) string {
	candidateRE := regexp.MustCompile(`^` + regexp.QuoteMeta(ref) + `\.\d+(\.\d+)*$`)
	var best string
	for page := 1; page <= 10; page++ {
		u := fmt.Sprintf("%s/repos/%s/%s/git/matching-refs/tags/%s?per_page=100&page=%d",
			g.BaseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref+"."), page)
		status, _, body, err := g.get(ctx, u)
		if err != nil || status != http.StatusOK {
			return ""
		}
		var refs []struct {
			Ref string `json:"ref"`
		}
		if err := json.Unmarshal(body, &refs); err != nil {
			return ""
		}
		for _, r := range refs {
			tag := strings.TrimPrefix(r.Ref, "refs/tags/")
			if candidateRE.MatchString(tag) && (best == "" || compareVersions(tag, best) > 0) {
				best = tag
			}
		}
		if len(refs) < 100 {
			break
		}
	}
	if best == "" {
		return ""
	}
	if bestSHA, err := g.commitSHA(ctx, owner, repo, best); err != nil || bestSHA != sha {
		return ""
	}
	return best
}

// compareVersions orders dotted numeric versions (v7.0.10 > v7.0.9). Both
// inputs are candidateRE matches, so every segment is numeric.
func compareVersions(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na != nb {
			return na - nb
		}
	}
	return 0
}
