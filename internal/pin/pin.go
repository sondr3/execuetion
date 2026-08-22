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

// A Ref is an action reference from a uses: line. For reusable workflows
// (owner/repo/.github/workflows/x.yml@v1) Owner and Repo are the first two
// path segments; the rest of the path plays no part in resolution.
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
// (`uses: "actions/checkout@v7",`) or block yaml (`- uses: actions/checkout@v7`),
// with or without a trailing version comment. Go regexp has no
// backreferences, so the opening and closing quotes are captured separately
// and checked for equality in parseLine. A YAML-shaped uses: line inside a
// `run: |` block scalar would false-positive here; like pinact, we accept
// that.
var usesRE = regexp.MustCompile(`^(\s*(?:- +)?['"]?uses['"]?\s*: +)(['"]?)([^\s'",]+)(['"]?)(,?)\s*(?:#\s*(\S*).*)?$`)

var shaRE = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// A parsedLine is one uses: line split into the pieces that must be
// preserved verbatim (prefix, quoting, kyaml comma) and the reference.
type parsedLine struct {
	prefix  string // indentation, optional dash, "uses: "
	quote   string // "", `"`, or "'"
	path    string // full action path before @, e.g. owner/repo/sub/dir
	ref     Ref    // Ref.Ref is whatever follows @, possibly a SHA
	comma   string // "," in kyaml, "" in block yaml
	comment string // first token of a trailing # comment, "tag=" stripped
}

// parseLine reports whether line is a uses: reference to a GitHub action
// or reusable workflow. Local actions (./…), docker:// images, values
// without @ or without an owner/repo prefix, and mismatched quoting are
// all rejected. Whether the ref is a tag or an already-pinned SHA is the
// caller's concern.
func parseLine(line string) (parsedLine, bool) {
	m := usesRE.FindStringSubmatch(line)
	if m == nil {
		return parsedLine{}, false
	}
	prefix, q1, value, q2, comma, comment := m[1], m[2], m[3], m[4], m[5], m[6]
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
	return parsedLine{
		prefix:  prefix,
		quote:   q1,
		path:    path,
		ref:     Ref{Owner: segments[0], Repo: segments[1], Ref: ref},
		comma:   comma,
		comment: strings.TrimPrefix(comment, "tag="),
	}, true
}

// parseUsesLine reports whether line is a pinnable uses: reference, i.e.
// one whose ref is not already a full commit SHA.
func parseUsesLine(line string) (parsedLine, bool) {
	parsed, ok := parseLine(line)
	if !ok || shaRE.MatchString(parsed.ref.Ref) {
		return parsedLine{}, false
	}
	return parsed, true
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

// A PinnedRef is a uses: line that is already pinned to a commit SHA and
// carries a version comment claiming what that SHA is.
type PinnedRef struct {
	Ref Ref    // Ref.Ref is the claimed version from the comment
	SHA string // the pinned commit
}

// PinnedRefs scans one workflow document and returns every SHA-pinned
// uses: line with a version comment, for verification that the comment
// still resolves to that SHA. Pinned lines without a comment are
// unverifiable and skipped.
func PinnedRefs(data []byte) []PinnedRef {
	var refs []PinnedRef
	for _, line := range strings.Split(string(data), "\n") {
		parsed, ok := parseLine(line)
		if !ok || !shaRE.MatchString(parsed.ref.Ref) || parsed.comment == "" {
			continue
		}
		refs = append(refs, PinnedRef{
			Ref: Ref{Owner: parsed.ref.Owner, Repo: parsed.ref.Repo, Ref: parsed.comment},
			SHA: parsed.ref.Ref,
		})
	}
	return refs
}

// Rewrite returns data with every pinnable uses: line rewritten to the SHA
// from pins (keyed by Ref.Key), keeping the resolved full version — or the
// original ref when none was discovered — as a trailing comment. Lines
// whose ref is missing from pins are left untouched.
func Rewrite(data []byte, pins map[string]Resolution) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		parsed, ok := parseUsesLine(line)
		if !ok {
			continue
		}
		res, ok := pins[parsed.ref.Key()]
		if !ok {
			continue
		}
		comment := res.Version
		if comment == "" {
			comment = parsed.ref.Ref
		}
		lines[i] = fmt.Sprintf("%s%s%s@%s%s%s # %s",
			parsed.prefix, parsed.quote, parsed.path, res.SHA, parsed.quote, parsed.comma, comment)
	}
	return []byte(strings.Join(lines, "\n"))
}

// A Change records a pin that moved during an Update run.
type Change struct {
	Key        string
	OldSHA     string
	NewSHA     string
	OldVersion string
	NewVersion string
}

// A Pinner resolves and rewrites action references across a set of encoded
// workflow documents.
type Pinner struct {
	Resolver Resolver
	Cache    *Cache
	// Update ignores cache reads and re-resolves every reference,
	// overwriting its cache entry (--update-pins).
	Update bool
	// Changes accumulates pins that moved during Update runs.
	Changes []Change
}

// Resolve resolves one reference, cache-first unless Update is set, and
// records the resolution — under both the ref's own key and, when a full
// version was discovered, under that version's key, so that later
// verification of the generated comment needs no network.
func (p *Pinner) Resolve(ctx context.Context, r Ref) (Resolution, error) {
	old, hadOld := p.Cache.Get(r.Key())
	if !p.Update && hadOld {
		return old, nil
	}
	res, err := p.Resolver.Resolve(ctx, r.Owner, r.Repo, r.Ref)
	if err != nil {
		return Resolution{}, err
	}
	p.Cache.Put(r.Key(), res)
	if res.Version != "" && res.Version != r.Ref {
		p.Cache.Put(Ref{Owner: r.Owner, Repo: r.Repo, Ref: res.Version}.Key(), res)
	}
	if p.Update && hadOld && old.SHA != res.SHA {
		p.Changes = append(p.Changes, Change{
			Key:        r.Key(),
			OldSHA:     old.SHA,
			NewSHA:     res.SHA,
			OldVersion: old.Version,
			NewVersion: res.Version,
		})
	}
	return res, nil
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

	pins := make(map[string]Resolution, len(order))
	var errs []error
	for _, r := range order {
		res, err := p.Resolve(ctx, r)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolving %s: %w", r.Key(), err))
			continue
		}
		pins[r.Key()] = res
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
