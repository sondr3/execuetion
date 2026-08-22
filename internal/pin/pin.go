// Package pin rewrites uses: references in generated workflow YAML from
// mutable tags (actions/checkout@v7) to immutable commit SHAs with a
// trailing version comment, pinact-style. Resolutions are cached globally
// in ~/.cache/execuetion/pins.json so the same handful of actions shared
// across repos costs one GitHub API request ever.
package pin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A Ref is a pinnable action reference found on a uses: line. For reusable
// workflows (owner/repo/.github/workflows/x.yml@v1) Owner and Repo are the
// first two path segments; the rest of the path plays no part in resolution.
type Ref struct {
	Owner string
	Repo  string
	Ref   string
}

// Key is the cache key for this reference: "owner/repo@ref".
func (r Ref) Key() string {
	return r.Owner + "/" + r.Repo + "@" + r.Ref
}

// usesRE matches a uses: line in either output format: kyaml
// (`uses: "actions/checkout@v7",`) or block yaml (`- uses: actions/checkout@v7`).
// Go regexp has no backreferences, so the opening and closing quotes are
// captured separately and checked for equality in parseUsesLine. A
// YAML-shaped uses: line inside a `run: |` block scalar would false-positive
// here; like pinact, we accept that.
var usesRE = regexp.MustCompile(`^(\s*(?:- +)?['"]?uses['"]?\s*: +)(['"]?)([^\s'",]+)(['"]?)(,?)\s*(?:#.*)?$`)

var shaRE = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// A parsedLine is one rewritable uses: line split into the pieces that must
// be preserved verbatim (prefix, quoting, kyaml comma) and the reference.
type parsedLine struct {
	prefix string // indentation, optional dash, "uses: "
	quote  string // "", `"`, or "'"
	path   string // full action path before @, e.g. owner/repo/sub/dir
	ref    Ref
	comma  string // "," in kyaml, "" in block yaml
}

// parseUsesLine reports whether line is a pinnable uses: reference. Local
// actions (./…), docker:// images, values without @ or without an
// owner/repo prefix, refs that are already full commit SHAs, and mismatched
// quoting are all rejected.
func parseUsesLine(line string) (parsedLine, bool) {
	m := usesRE.FindStringSubmatch(line)
	if m == nil {
		return parsedLine{}, false
	}
	prefix, q1, value, q2, comma := m[1], m[2], m[3], m[4], m[5]
	if q1 != q2 {
		return parsedLine{}, false
	}
	at := strings.LastIndex(value, "@")
	if at <= 0 || at == len(value)-1 {
		return parsedLine{}, false
	}
	path, ref := value[:at], value[at+1:]
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "docker://") {
		return parsedLine{}, false
	}
	segments := strings.Split(path, "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return parsedLine{}, false
	}
	if shaRE.MatchString(ref) {
		return parsedLine{}, false
	}
	return parsedLine{
		prefix: prefix,
		quote:  q1,
		path:   path,
		ref:    Ref{Owner: segments[0], Repo: segments[1], Ref: ref},
		comma:  comma,
	}, true
}

// Refs scans one encoded workflow document and returns its pinnable
// references, deduplicated, in order of first appearance.
func Refs(data []byte) []Ref {
	var refs []Ref
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		parsed, ok := parseUsesLine(line)
		if !ok || seen[parsed.ref.Key()] {
			continue
		}
		seen[parsed.ref.Key()] = true
		refs = append(refs, parsed.ref)
	}
	return refs
}

// Rewrite returns data with every pinnable uses: line rewritten to the SHA
// from pins (keyed by Ref.Key), keeping the original ref as a trailing
// comment. Lines whose ref is missing from pins are left untouched.
func Rewrite(data []byte, pins map[string]string) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		parsed, ok := parseUsesLine(line)
		if !ok {
			continue
		}
		sha, ok := pins[parsed.ref.Key()]
		if !ok {
			continue
		}
		lines[i] = fmt.Sprintf("%s%s%s@%s%s%s # %s",
			parsed.prefix, parsed.quote, parsed.path, sha, parsed.quote, parsed.comma, parsed.ref.Ref)
	}
	return []byte(strings.Join(lines, "\n"))
}

// A Pinner resolves and rewrites action references across a set of encoded
// workflow documents.
type Pinner struct {
	Resolver Resolver
	Cache    *Cache
	// Update ignores cache reads and re-resolves every reference,
	// overwriting its cache entry (--update-pins).
	Update bool
}

// Pin rewrites every pinnable uses: reference in docs to its commit SHA.
// References are deduplicated across all documents so each unique
// owner/repo@ref costs at most one Resolver call; cached resolutions cost
// none. Resolution failures are collected per-ref and joined, and the cache
// is saved even then so successful resolutions persist.
func (p *Pinner) Pin(ctx context.Context, docs [][]byte) ([][]byte, error) {
	var order []Ref
	seen := make(map[string]bool)
	for _, doc := range docs {
		for _, r := range Refs(doc) {
			if !seen[r.Key()] {
				seen[r.Key()] = true
				order = append(order, r)
			}
		}
	}

	pins := make(map[string]string, len(order))
	var errs []error
	for _, r := range order {
		if !p.Update {
			if sha, ok := p.Cache.Get(r.Key()); ok {
				pins[r.Key()] = sha
				continue
			}
		}
		sha, err := p.Resolver.Resolve(ctx, r.Owner, r.Repo, r.Ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolving %s: %w", r.Key(), err))
			continue
		}
		p.Cache.Put(r.Key(), sha)
		pins[r.Key()] = sha
	}
	if err := p.Cache.Save(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	pinned := make([][]byte, len(docs))
	for i, doc := range docs {
		pinned[i] = Rewrite(doc, pins)
	}
	return pinned, nil
}
