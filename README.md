<h1 align="center">execuetion</h1>
<p align="center">
    <a href="https://github.com/sondr3/execuetion/actions"><img alt="GitHub Actions Status" src="https://github.com/sondr3/execuetion/workflows/pipeline/badge.svg" /></a>
</p>

<p align="center">
    <b>Author GitHub Actions workflows in CUE, generate the YAML — execution, with CUE in the middle</b>
</p>

A self-contained binary that loads `.github/workflows/*.cue` and generates the
corresponding `.yml` files, typed against
[`cue.dev/x/githubactions`](https://cue.dev/x/githubactions). The CUE module
and its dependencies are embedded in the binary — consuming repos need no
`cue.mod/` directory, no network access, and no CUE toolchain.

**Note:** this is vibe coded slop for personal use.

# Quickstart

```sh
git clone github.com/sondr3/execuetion
cd execuetion
go install .
```

## Usage

Write one `.cue` file per workflow in `.github/workflows/`. Every file must
evaluate to a `githubactions.#Workflow` and use `package workflows`. The
output filename is derived from the source filename (`ci.cue` → `ci.yml`);
the workflow `name:` field can be renamed freely.

```cue
package workflows

import "cue.dev/x/githubactions"

githubactions.#Workflow & {
	name: "CI"
	on: push: branches: ["main"]
	jobs: build: {
		"runs-on": "ubuntu-latest"
		steps: [
			{uses: "actions/checkout@v7"},
			{name: "Test", run: "go test ./..."},
		]
	}
}
```

Then run the generator from anywhere inside the repo:

```sh
$ execuetion
Wrote 1 workflow(s), 0 up to date
```

### Output format

Workflows are written as [KYAML](https://kyaml.dev) by default — a JSON-ish
YAML subset that GitHub parses like any other YAML file. Pass `--format yaml`
to opt into regular block-style YAML instead. Use the same format for
generation and `--check`, since the comparison is byte-based.

### Action pinning

Generated workflows are pinned by default, [pinact](https://github.com/suzuki-shunsuke/pinact)-style:
every `uses:` reference to a mutable tag or branch is rewritten to the commit
SHA it resolves to, with the original ref kept as a trailing comment:

```yaml
uses: "actions/checkout@08c6903cd8c0fde910a37f88322edcfb5dd907a8", # v7
```

Local actions (`./…`), `docker://` images, and refs that are already full
SHAs are left untouched. Reusable workflow references
(`owner/repo/.github/workflows/x.yml@v1`) are pinned too.

Resolutions are cached globally in `~/.cache/execuetion/pins.json`
(`$XDG_CACHE_HOME` and `$EXECUETION_CACHE_DIR` are honored), keyed by
`owner/repo@ref` — the same handful of actions shared across all your repos
costs one GitHub API request ever. Resolution goes through the GitHub API and
uses `GITHUB_TOKEN` (or `GH_TOKEN`) when set, falling back to the token a
locally installed [`gh`](https://cli.github.com) CLI is logged in with
(`gh auth token`); anonymous requests are rate-limited to 60/hour.

- `--no-pin` skips pinning entirely.
- `--update-pins` re-resolves every ref encountered and refreshes the cache —
  this is how you move to a newer release of an action; cached pins never
  expire on their own.

Note that `--check` compares the pinned output, so it needs either a warm
cache or network access (CI runners typically have `GITHUB_TOKEN` available).
`--check --update-pins` additionally flags workflows as stale when an
upstream tag has moved since the committed pin — useful as a scheduled audit.

### Shared helpers

The binary ships no helpers, only the schema — but the phantom module (rooted
at `.github/workflows/`, so the rest of your repo is untouched) is named
`execuetion.dev`, so repo-local helper packages work with a plain import. Put
`package lib` files in `.github/workflows/lib/` and import them as
`"execuetion.dev/lib"` from any workflow.

### Editor support

Generation needs no `cue.mod/` — but your editor does: the CUE language
server can only resolve imports and autocomplete when a module file exists on
disk. Run `execuetion init` to write `.github/workflows/cue.mod/module.cue`,
declaring the module and pinning `cue.dev/x/githubactions` at the version
vendored into the binary, and commit it. `cue lsp` and editor extensions use
it (fetching the schema from the Central Registry); generation ignores it
entirely — the embedded module shadows it through the overlay, so generating
stays hermetic and offline. Anything beyond `module.cue` in that directory
(`pkg/`, `gen/`, `usr/`) is rejected, since the loader would merge it with
the embedded module.

### Check mode

`execuetion --check` is a CI gate: it regenerates every workflow in memory
and fails (exit code 1) if the committed YAML is stale, missing, or orphaned
(a generated `.yml` whose `.cue` source was deleted). By default it is strict:
every workflow file must carry the generated-code header. Pass
`--allow-handwritten` to let hand-written workflows coexist during migration.

Comparison normalizes CRLF line endings and trailing whitespace, so Windows
`autocrlf` checkouts do not produce false positives.

### Exit codes

| Code | Meaning                        |
| ---- | ------------------------------ |
| 0    | OK                             |
| 1    | Stale or orphan (`--check`)    |
| 2    | Evaluation or load error       |

## Updating the embedded schema

The `cue.dev/x/githubactions` schema is vendored into the binary; see
[`internal/cuemod/VENDOR.md`](internal/cuemod/VENDOR.md) for the update
procedure. Regenerate all workflows in the same PR that bumps it.

# License

Apache.
