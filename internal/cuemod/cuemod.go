// Package cuemod embeds the phantom CUE module that execuetion overlays
// onto the consuming repo's .github/workflows/ directory: cue.mod/ holds the
// module file plus vendored dependencies, see VENDOR.md.
package cuemod

import "embed"

// GithubActionsVersion is the version of cue.dev/x/githubactions vendored
// into cue.mod/pkg/. Keep in sync with VENDOR.md when updating; "execuetion
// --init" pins this version in the editor-facing module.cue it writes.
const GithubActionsVersion = "v0.5.0"

//go:embed all:cue.mod
var FS embed.FS
