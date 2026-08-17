<h1 align="center">execuetion</h1>
<p align="center">
    <a href="https://github.com/sondr3/actions/actions"><img alt="GitHub Actions Status" src="https://github.com/sondr3/actions/workflows/pipeline/badge.svg" /></a>
</p>

<p align="center">
    <b>Author GitHub Actions workflows in CUE, generate the YAML — execution, with CUE in the middle</b>
</p>

A self-contained binary that loads `.github/workflows/*.cue` and generates the
corresponding `.yml` files, typed against
[`cue.dev/x/githubactions`](https://cue.dev/x/githubactions). The CUE module
and its dependencies are embedded in the binary — consuming repos need no
`cue.mod/` directory, no network access, and no CUE toolchain.

# Quickstart

```sh
git clone github.com/sondr3/actions
cd actions
go install .
```

## Usage

Write one `.cue` file per workflow in `.github/workflows/`. Every file must
evaluate to a `githubactions.#Workflow` and use `package workflows`. The
output filename is derived from the source filename (`ci.cue` → `ci.yml`);
the workflow `name:` field can be renamed freely.

```cue
package workflows

import (
	"cue.dev/x/githubactions"
	"eons.actions/lib"
)

githubactions.#Workflow & {
	name: "CI"
	on: push: branches: ["main"]
	jobs: build: lib.#Job & {
		steps: [
			lib.#Checkout,
			lib.#SetupGo,
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

### Shared helpers

`eons.actions/lib` is a helper package embedded in the binary (see
[`internal/cuemod/lib`](internal/cuemod/lib)). Repo-local helpers work too:
`.cue` files in a `lib/` directory at the repo root join the same package
through the overlay.

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
