# Vendored CUE dependencies

This directory is embedded into the binary and overlaid onto the consuming
repo's `.github/workflows/` directory at load time, where `cue.mod/` becomes
the phantom module.

Dependencies are vendored into `cue.mod/pkg/` (the legacy import location,
still supported for the main module). They must **not** also be listed in
`deps:` in `module.cue` — that triggers an "ambiguous import" error.

| Module                    | Version |
| ------------------------- | ------- |
| `cue.dev/x/githubactions` | v0.5.0  |

## Updating a dependency

1. In a scratch directory: `cue mod init scratch.example && cue mod get cue.dev/x/githubactions@latest`
2. Force extraction by evaluating a file that imports it: `cue eval`
3. Copy the `.cue` files from `$CUE_CACHE_DIR/mod/extract/cue.dev/x/githubactions@<version>/`
   (macOS default: `~/Library/Caches/cue/mod/extract/...`) into
   `cue.mod/pkg/cue.dev/x/githubactions/`, excluding its nested `cue.mod/`.
4. Update the version in the table above.
5. Regenerate all workflows in the same PR — schema updates may reformat output.
